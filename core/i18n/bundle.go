package i18n

import (
	"fmt"
	"maps"
	"slices"
)

// MissReason says why the miss handler fired.
type MissReason uint8

const (
	// MissingKey: no catalog in the fallback chain defined the key. Fired at
	// render time; the render echoes the key.
	MissingKey MissReason = iota
	// MissingForm: a plural form is missing or dead relative to the locale's
	// wired rule. Fired at construction, by probing.
	MissingForm
)

// String returns "missing_key" or "missing_form".
func (r MissReason) String() string {
	if r == MissingForm {
		return "missing_form"
	}
	return "missing_key"
}

// Miss describes one lookup gap reported to the miss handler.
type Miss struct {
	// Locale is the locale the lookup started from.
	Locale Locale
	// Key is the message key; for MissingForm it carries the form suffix
	// ("cart.items.many").
	Key string
	// Reason says whether this is a runtime miss or a load-time form gap.
	Reason MissReason
}

// pluralMsg holds one plural message's compiled forms. A nil entry means the
// catalog did not define that form.
type pluralMsg struct {
	forms [numCategories]*compiledMsg
}

// localeData is one locale's compiled catalog and settings.
type localeData struct {
	messages map[string]*compiledMsg
	plurals  map[string]*pluralMsg
	rule     PluralRule
	tag      string
	lang     string
	// chain is the lookup order: this locale, its base language (if distinct
	// and present), then the default locale. Deduplicated, precomputed at New.
	chain  []int
	format FormatSpec
	// ruleWired records whether rule came from WithPlural. Probing skips
	// locales on DefaultRule: it is not a claim about grammar, so a catalog
	// cannot be incomplete with respect to it.
	ruleWired bool
}

// Bundle is the immutable engine built by New. It is safe for concurrent use;
// nothing mutates after construction.
type Bundle struct {
	byTag      map[string]int
	onMiss     func(Miss)
	cfg        Config
	locales    []localeData
	defaultIdx int
}

// New builds a Bundle from catalogs and options.
//
// The supported locale set is exactly what the catalogs declare: this package
// ships no locale list and validates tags against none. Locales with no wired
// plural rule use DefaultRule; locales with no wired format use Invariant.
// Neither is an error.
//
// Every catalog is parsed and compiled here, so rendering reads no files and
// parses nothing.
func New(opts ...Option) (*Bundle, error) {
	cf := &config{
		cfg:     DefaultConfig(),
		plurals: make(map[string]PluralRule),
		formats: make(map[string]FormatSpec),
	}
	for _, o := range opts {
		o(cf)
	}
	if len(cf.nilRules) > 0 {
		return nil, fmt.Errorf("%w: %q", ErrNilRule, cf.nilRules[0])
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}

	raw, err := collect(cf)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: no messages loaded", ErrInvalidCatalog)
	}
	defTag := normalizeTag(cf.cfg.DefaultLocale)
	if _, ok := raw[defTag]; !ok {
		return nil, fmt.Errorf("%w: default locale %q has no messages", ErrInvalidCatalog, defTag)
	}

	// Sort tags so locale indices are deterministic across runs.
	tags := slices.Sorted(maps.Keys(raw))
	b := &Bundle{
		byTag:   make(map[string]int, len(tags)),
		onMiss:  cf.onMiss,
		locales: make([]localeData, len(tags)),
		cfg:     cf.cfg,
	}
	for i, tag := range tags {
		b.byTag[tag] = i
	}
	b.defaultIdx = b.byTag[defTag]

	// The sorted pass above fixed every index, so this pass may walk raw in
	// map order: each iteration writes a different locale.
	for tag, rc := range raw {
		ld := &b.locales[b.byTag[tag]]
		ld.tag = tag
		ld.lang = Locale{tag: tag}.Lang()
		ld.rule, ld.ruleWired = cf.plurals[ld.lang]
		if !ld.ruleWired {
			ld.rule = DefaultRule
		}
		ld.format = resolveFormat(cf.formats, tag, ld.lang)
		compileInto(ld, rc)
	}
	// Chains reference every locale's index, so they are built only once the
	// loop above has populated all of them.
	for i := range b.locales {
		b.locales[i].chain = b.buildChain(i)
	}
	b.probePlurals()
	return b, nil
}

// collect loads every source into one tag→rawCatalog map.
func collect(cf *config) (map[string]*rawCatalog, error) {
	raw := make(map[string]*rawCatalog)
	for _, fsys := range cf.sources {
		cats, err := loadFS(fsys)
		if err != nil {
			return nil, err
		}
		for tag, rc := range cats {
			dst, ok := raw[tag]
			if !ok {
				raw[tag] = rc
				continue
			}
			if err := mergeCatalog(dst, rc); err != nil {
				return nil, err
			}
		}
	}
	for _, in := range cf.inline {
		tag := normalizeTag(in.tag)
		if tag == "" {
			return nil, fmt.Errorf("%w: %q is not a language tag", ErrInvalidCatalog, in.tag)
		}
		rc, ok := raw[tag]
		if !ok {
			rc = newRawCatalog()
			raw[tag] = rc
		}
		if err := rc.addNamespace(in.ns, in.data); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// mergeCatalog folds src into dst, rejecting any key the two sources both
// define. It routes through dst's own setters — setMessage and putPlural —
// rather than writing the maps directly, so that every collision shape
// catalog.go rejects within a single source (message vs. message, message vs.
// plural, and a message shadowing a plural's form "x.one" against plural "x")
// is rejected across sources too, from the identical code. That shared routing
// is what keeps the rawCatalog invariant ("a key appears in exactly one of
// messages or plurals") true of a merged catalog.
func mergeCatalog(dst, src *rawCatalog) error {
	for k, v := range src.messages {
		if err := dst.setMessage(k, v); err != nil {
			return err
		}
	}
	for k, forms := range src.plurals {
		if err := dst.putPlural(k, forms); err != nil {
			return err
		}
	}
	return nil
}

// resolveFormat picks a locale's spec: exact tag, then base language, then
// Invariant. It deliberately never falls back to the default locale's spec —
// German dates for a Vietnamese reader are worse than neutral ones.
func resolveFormat(formats map[string]FormatSpec, tag, lang string) FormatSpec {
	if s, ok := formats[tag]; ok {
		return completeSpec(s)
	}
	if s, ok := formats[lang]; ok {
		return completeSpec(s)
	}
	return Invariant
}

// compileInto compiles a locale's raw catalog. Every message in the bundle is
// parsed exactly once, here.
func compileInto(ld *localeData, rc *rawCatalog) {
	ld.messages = make(map[string]*compiledMsg, len(rc.messages))
	for k, tmpl := range rc.messages {
		ld.messages[k] = compileMessage(tmpl)
	}
	ld.plurals = make(map[string]*pluralMsg, len(rc.plurals))
	for k, forms := range rc.plurals {
		pm := new(pluralMsg)
		for cat, tmpl := range forms {
			pm.forms[cat] = compileMessage(tmpl)
		}
		ld.plurals[k] = pm
	}
}

// buildChain precomputes locale i's lookup order: itself, its base language,
// then the default. This is where "exact tag → base language → default" lives,
// once, for both T and TN — so a lookup is array indexing, not string work.
func (b *Bundle) buildChain(i int) []int {
	chain := make([]int, 0, 3)
	add := func(j int) {
		if !slices.Contains(chain, j) {
			chain = append(chain, j)
		}
	}
	add(i)
	if lang := b.locales[i].lang; lang != b.locales[i].tag {
		if j, ok := b.byTag[lang]; ok {
			add(j)
		}
	}
	add(b.defaultIdx)
	return chain
}

// probePlurals reports incomplete and dead plural translations through the
// miss handler at construction. It never fails New.
//
// Only locales with an explicitly wired rule are probed: DefaultRule is not a
// claim about any language's grammar, so a catalog cannot be incomplete with
// respect to it. zero is never dead (the zero convention lets a translator
// define it regardless of the rule) and neither is other (the terminal form of
// every fallback chain).
//
// The order gaps are reported in is unspecified.
func (b *Bundle) probePlurals() {
	if b.onMiss == nil {
		return
	}
	for i := range b.locales {
		ld := &b.locales[i]
		if !ld.ruleWired {
			continue
		}
		supported := SupportedForms(ld.rule)
		var produced [numCategories]bool
		for _, c := range supported {
			produced[c] = true
		}
		for key, pm := range ld.plurals {
			// Incomplete: the rule produces a form the catalog never defines.
			for _, c := range supported {
				if pm.forms[c] == nil {
					b.report(ld.tag, key+"."+c.String())
				}
			}
			// Dead: the catalog defines a form the rule never produces.
			for c := range PluralCategory(numCategories) {
				if pm.forms[c] != nil && !produced[c] && c != Zero && c != Other {
					b.report(ld.tag, key+"."+c.String())
				}
			}
		}
	}
}

func (b *Bundle) report(tag, key string) {
	b.onMiss(Miss{Locale: Locale{tag: tag}, Key: key, Reason: MissingForm})
}

// resolveIdx maps a normalized Locale to a supported-locale index by the one
// fallback rendering and negotiation must agree on: exact tag, then base
// language. ok is false when the locale is zero or matches neither — locIdx
// then substitutes the default, Parse then reports ErrUnknownLocale. Holding
// this walk in a single place is what keeps those two callers from drifting.
func (b *Bundle) resolveIdx(l Locale) (int, bool) {
	if l.tag == "" {
		return 0, false
	}
	if i, ok := b.byTag[l.tag]; ok {
		return i, true
	}
	if lang := l.Lang(); lang != l.tag {
		if i, ok := b.byTag[lang]; ok {
			return i, true
		}
	}
	return 0, false
}

// locIdx resolves a Locale to a supported locale index: exact tag, then base
// language, then the default. The zero Locale reads as unresolved.
func (b *Bundle) locIdx(loc Locale) int {
	if i, ok := b.resolveIdx(loc); ok {
		return i
	}
	return b.defaultIdx
}

func (b *Bundle) miss(idx int, key string, r MissReason) {
	if b.onMiss != nil {
		b.onMiss(Miss{Locale: Locale{tag: b.locales[idx].tag}, Key: key, Reason: r})
	}
}

// lookupMsg walks idx's chain for a plain message.
func (b *Bundle) lookupMsg(idx int, key string) *compiledMsg {
	for _, i := range b.locales[idx].chain {
		if m, ok := b.locales[i].messages[key]; ok {
			return m
		}
	}
	return nil
}

// lookupPluralOther walks idx's chain for a plural message and returns its
// Other form — the count-independent fallback T uses when a key exists only as
// a plural but is rendered without a count. This mirrors how tnAt falls through
// a missing plural to a plain message: a key ValidateKeys accepts (present as
// either a message or a plural) then renders under both T and TN rather than
// echoing under one of them.
func (b *Bundle) lookupPluralOther(idx int, key string) *compiledMsg {
	for _, i := range b.locales[idx].chain {
		if pm, ok := b.locales[i].plurals[key]; ok {
			if m := pm.forms[Other]; m != nil {
				return m
			}
		}
	}
	return nil
}

// lookupPlural walks idx's chain for a plural message, selecting the form with
// the rule of the locale the message was found in — English text must pick its
// form with English's rule even when a Ukrainian lookup fell through to it.
func (b *Bundle) lookupPlural(idx int, key string, n int) *compiledMsg {
	for _, i := range b.locales[idx].chain {
		ld := &b.locales[i]
		pm, ok := ld.plurals[key]
		if !ok {
			continue
		}
		if m := selectForm(pm, ld.rule(n), n); m != nil {
			return m
		}
	}
	return nil
}

// selectForm applies the zero convention, then the rule's category, then the
// form-fallback chain.
func selectForm(pm *pluralMsg, cat PluralCategory, n int) *compiledMsg {
	if n == 0 && pm.forms[Zero] != nil {
		return pm.forms[Zero]
	}
	if int(cat) < numCategories && pm.forms[cat] != nil {
		return pm.forms[cat]
	}
	for _, fb := range formFallback(cat) {
		if m := pm.forms[fb]; m != nil {
			return m
		}
	}
	return nil
}

// tAt renders key at an already-resolved locale index. Shared by T and
// Localizer.T, which must not redo the locIdx hash T already paid for.
func (b *Bundle) tAt(idx int, key string, args ...any) string {
	if m := b.lookupMsg(idx, key); m != nil {
		return m.render(args, 0, false)
	}
	if m := b.lookupPluralOther(idx, key); m != nil {
		return m.render(args, 0, false)
	}
	b.miss(idx, key, MissingKey)
	return key
}

// appendTAt is tAt appending into dst.
func (b *Bundle) appendTAt(dst []byte, idx int, key string, args ...any) []byte {
	if m := b.lookupMsg(idx, key); m != nil {
		return m.appendTo(dst, args, 0, false)
	}
	if m := b.lookupPluralOther(idx, key); m != nil {
		return m.appendTo(dst, args, 0, false)
	}
	b.miss(idx, key, MissingKey)
	return append(dst, key...)
}

// tnAt renders a pluralized message at an already-resolved locale index.
// Shared by TN and Localizer.TN.
func (b *Bundle) tnAt(idx int, key string, n int, args ...any) string {
	if m := b.lookupPlural(idx, key, n); m != nil {
		return m.render(args, n, true)
	}
	if m := b.lookupMsg(idx, key); m != nil {
		return m.render(args, n, true)
	}
	b.miss(idx, key, MissingKey)
	return key
}

// appendTNAt is tnAt appending into dst.
func (b *Bundle) appendTNAt(dst []byte, idx int, key string, n int, args ...any) []byte {
	if m := b.lookupPlural(idx, key, n); m != nil {
		return m.appendTo(dst, args, n, true)
	}
	if m := b.lookupMsg(idx, key); m != nil {
		return m.appendTo(dst, args, n, true)
	}
	b.miss(idx, key, MissingKey)
	return append(dst, key...)
}

// T renders a message. args are variadic key/value pairs. A key present only
// as a plural renders its "other" form — a count-less lookup degrading to real
// text rather than the raw key, so ValidateKeys accepting a plural key never
// leads to an echo here. A key no catalog in the chain defines at all echoes
// back and notifies the miss handler — T never fails.
func (b *Bundle) T(loc Locale, key string, args ...any) string {
	return b.tAt(b.locIdx(loc), key, args...)
}

// AppendT is T appending into dst.
func (b *Bundle) AppendT(dst []byte, loc Locale, key string, args ...any) []byte {
	return b.appendTAt(dst, b.locIdx(loc), key, args...)
}

// TN renders a pluralized message, injecting n as {{count}}. A plain (non
// plural) message under the same key still renders, so a catalog may promote a
// key to plural forms without breaking callers.
func (b *Bundle) TN(loc Locale, key string, n int, args ...any) string {
	return b.tnAt(b.locIdx(loc), key, n, args...)
}

// AppendTN is TN appending into dst.
func (b *Bundle) AppendTN(dst []byte, loc Locale, key string, n int, args ...any) []byte {
	return b.appendTNAt(dst, b.locIdx(loc), key, n, args...)
}

// Parse resolves a tag against the supported set, returning the locale that
// will actually be used: exact tag, else base language. It reports
// ErrUnknownLocale rather than silently defaulting, for callers that care.
func (b *Bundle) Parse(tag string) (Locale, error) {
	l := newLocale(tag)
	if l.IsZero() {
		return Locale{}, fmt.Errorf("%w: %q", ErrUnknownLocale, tag)
	}
	if i, ok := b.resolveIdx(l); ok {
		return Locale{tag: b.locales[i].tag}, nil
	}
	return Locale{}, fmt.Errorf("%w: %q", ErrUnknownLocale, tag)
}

// ParseOrDefault is Parse with the default locale instead of an error. Empty,
// unknown, and malformed input are all first-class paths.
func (b *Bundle) ParseOrDefault(tag string) Locale {
	if l, err := b.Parse(tag); err == nil {
		return l
	}
	return b.Default()
}

// Default returns the bundle's default locale.
func (b *Bundle) Default() Locale { return Locale{tag: b.locales[b.defaultIdx].tag} }

// Locales returns the supported set, sorted by tag. Drives negotiation and
// language-switcher UIs.
func (b *Bundle) Locales() []Locale {
	out := make([]Locale, len(b.locales))
	for i := range b.locales {
		out[i] = Locale{tag: b.locales[i].tag}
	}
	return out
}
