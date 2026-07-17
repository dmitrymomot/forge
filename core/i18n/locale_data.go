package i18n

// FormatSpec holds one locale's rendering conventions. Passed to
// WithFormatOverride to patch a bundle-local copy; embedded in the static
// curated table otherwise.
type FormatSpec struct {
	// Relative holds the relative-time vocabulary.
	Relative RelativeSpec
	// DecimalSep separates integer and fractional digits ("." or ",").
	DecimalSep string
	// GroupSep separates 3-digit groups: "," "." or "\u00a0" (U+00A0 NBSP).
	GroupSep string
	// DateLayout, TimeLayout, DateTimeLayout are Go time layouts.
	DateLayout     string
	TimeLayout     string
	DateTimeLayout string
	// CurrencyBefore places the symbol before the amount ($1.50 vs 1,50 €).
	CurrencyBefore bool
	// CurrencySpace inserts a space between symbol and amount.
	CurrencySpace bool
	// PercentSpace inserts a space before the percent sign (fr: "50 %").
	PercentSpace bool
}

// Relative-time unit indices for RelativeSpec.Units.
const (
	relMinute = iota
	relHour
	relDay
	relWeek
	relMonth
	relYear

	numRelUnits = 6
)

// RelativeSpec is a locale's relative-time vocabulary. Units[u][cat] holds a
// "{{count}} <unit>" fragment per plural category (sparse; form fallback
// applies). Past/Future wrap the fragment via the {{d}} placeholder.
// FutureUnits overrides Units after Future when the language declines the
// unit differently in each direction (nil = reuse Units).
type RelativeSpec struct {
	FutureUnits *[numRelUnits][numCategories]string
	Units       [numRelUnits][numCategories]string
	Now         string
	Past        string
	Future      string
}

// localeInfo is one curated locale row.
type localeInfo struct {
	rule   PluralRule
	tag    string
	lang   string
	format FormatSpec
}

var relEN = RelativeSpec{
	Now: "just now", Past: "{{d}} ago", Future: "in {{d}}",
	Units: [numRelUnits][numCategories]string{
		relMinute: {One: "{{count}} minute", Other: "{{count}} minutes"},
		relHour:   {One: "{{count}} hour", Other: "{{count}} hours"},
		relDay:    {One: "{{count}} day", Other: "{{count}} days"},
		relWeek:   {One: "{{count}} week", Other: "{{count}} weeks"},
		relMonth:  {One: "{{count}} month", Other: "{{count}} months"},
		relYear:   {One: "{{count}} year", Other: "{{count}} years"},
	},
}

var localeTable = []localeInfo{
	{tag: "en", lang: "en", rule: ruleOneOther, format: FormatSpec{
		DecimalSep: ".", GroupSep: ",", CurrencyBefore: true,
		DateLayout: "01/02/2006", TimeLayout: "3:04 PM", DateTimeLayout: "01/02/2006 3:04 PM",
		Relative: relEN,
	}},
	{tag: "en-GB", lang: "en", rule: ruleOneOther, format: FormatSpec{
		DecimalSep: ".", GroupSep: ",", CurrencyBefore: true,
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		Relative: relEN,
	}},
	{tag: "de", lang: "de", rule: ruleOneOther, format: FormatSpec{
		DecimalSep: ",", GroupSep: ".", CurrencySpace: true, PercentSpace: true,
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		Relative: RelativeSpec{
			Now: "gerade eben", Past: "vor {{d}}", Future: "in {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} Minute", Other: "{{count}} Minuten"},
				relHour:   {One: "{{count}} Stunde", Other: "{{count}} Stunden"},
				relDay:    {One: "{{count}} Tag", Other: "{{count}} Tagen"},
				relWeek:   {One: "{{count}} Woche", Other: "{{count}} Wochen"},
				relMonth:  {One: "{{count}} Monat", Other: "{{count}} Monaten"},
				relYear:   {One: "{{count}} Jahr", Other: "{{count}} Jahren"},
			},
		},
	}},
	{tag: "fr", lang: "fr", rule: ruleFrench, format: FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0", CurrencySpace: true, PercentSpace: true,
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		Relative: RelativeSpec{
			Now: "à l'instant", Past: "il y a {{d}}", Future: "dans {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minute", Other: "{{count}} minutes"},
				relHour:   {One: "{{count}} heure", Other: "{{count}} heures"},
				relDay:    {One: "{{count}} jour", Other: "{{count}} jours"},
				relWeek:   {One: "{{count}} semaine", Other: "{{count}} semaines"},
				relMonth:  {One: "{{count}} mois", Other: "{{count}} mois"},
				relYear:   {One: "{{count}} an", Other: "{{count}} ans"},
			},
		},
	}},
	{tag: "es", lang: "es", rule: ruleSpanish, format: FormatSpec{
		DecimalSep: ",", GroupSep: ".", CurrencySpace: true, PercentSpace: true,
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		Relative: RelativeSpec{
			Now: "ahora mismo", Past: "hace {{d}}", Future: "dentro de {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minuto", Other: "{{count}} minutos"},
				relHour:   {One: "{{count}} hora", Other: "{{count}} horas"},
				relDay:    {One: "{{count}} día", Other: "{{count}} días"},
				relWeek:   {One: "{{count}} semana", Other: "{{count}} semanas"},
				relMonth:  {One: "{{count}} mes", Other: "{{count}} meses"},
				relYear:   {One: "{{count}} año", Other: "{{count}} años"},
			},
		},
	}},
	{tag: "it", lang: "it", rule: ruleItalian, format: FormatSpec{
		DecimalSep: ",", GroupSep: ".", CurrencySpace: true,
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		Relative: RelativeSpec{
			Now: "proprio ora", Past: "{{d}} fa", Future: "tra {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minuto", Other: "{{count}} minuti"},
				relHour:   {One: "{{count}} ora", Other: "{{count}} ore"},
				relDay:    {One: "{{count}} giorno", Other: "{{count}} giorni"},
				relWeek:   {One: "{{count}} settimana", Other: "{{count}} settimane"},
				relMonth:  {One: "{{count}} mese", Other: "{{count}} mesi"},
				relYear:   {One: "{{count}} anno", Other: "{{count}} anni"},
			},
		},
	}},
	{tag: "pt-BR", lang: "pt", rule: rulePortuguese, format: FormatSpec{
		DecimalSep: ",", GroupSep: ".", CurrencyBefore: true, CurrencySpace: true,
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		Relative: RelativeSpec{
			Now: "agora mesmo", Past: "há {{d}}", Future: "em {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minuto", Other: "{{count}} minutos"},
				relHour:   {One: "{{count}} hora", Other: "{{count}} horas"},
				relDay:    {One: "{{count}} dia", Other: "{{count}} dias"},
				relWeek:   {One: "{{count}} semana", Other: "{{count}} semanas"},
				relMonth:  {One: "{{count}} mês", Other: "{{count}} meses"},
				relYear:   {One: "{{count}} ano", Other: "{{count}} anos"},
			},
		},
	}},
	{tag: "pl", lang: "pl", rule: rulePolish, format: FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0", CurrencySpace: true,
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		Relative: RelativeSpec{
			Now: "przed chwilą", Past: "{{d}} temu", Future: "za {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minutę", Few: "{{count}} minuty", Many: "{{count}} minut", Other: "{{count}} minut"},
				relHour:   {One: "{{count}} godzinę", Few: "{{count}} godziny", Many: "{{count}} godzin", Other: "{{count}} godzin"},
				relDay:    {One: "{{count}} dzień", Few: "{{count}} dni", Many: "{{count}} dni", Other: "{{count}} dni"},
				relWeek:   {One: "{{count}} tydzień", Few: "{{count}} tygodnie", Many: "{{count}} tygodni", Other: "{{count}} tygodni"},
				relMonth:  {One: "{{count}} miesiąc", Few: "{{count}} miesiące", Many: "{{count}} miesięcy", Other: "{{count}} miesięcy"},
				relYear:   {One: "{{count}} rok", Few: "{{count}} lata", Many: "{{count}} lat", Other: "{{count}} lat"},
			},
		},
	}},
	{tag: "cs", lang: "cs", rule: ruleCzech, format: FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0", CurrencySpace: true, PercentSpace: true,
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		Relative: RelativeSpec{
			Now: "právě teď", Past: "před {{d}}", Future: "za {{d}}",
			// Past takes the instrumental case…
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minutou", Few: "{{count}} minutami", Other: "{{count}} minutami"},
				relHour:   {One: "{{count}} hodinou", Few: "{{count}} hodinami", Other: "{{count}} hodinami"},
				relDay:    {One: "{{count}} dnem", Few: "{{count}} dny", Other: "{{count}} dny"},
				relWeek:   {One: "{{count}} týdnem", Few: "{{count}} týdny", Other: "{{count}} týdny"},
				relMonth:  {One: "{{count}} měsícem", Few: "{{count}} měsíci", Other: "{{count}} měsíci"},
				relYear:   {One: "{{count}} rokem", Few: "{{count}} lety", Other: "{{count}} lety"},
			},
			// …while the future takes the accusative, so cs overrides FutureUnits.
			FutureUnits: &[numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minutu", Few: "{{count}} minuty", Other: "{{count}} minut"},
				relHour:   {One: "{{count}} hodinu", Few: "{{count}} hodiny", Other: "{{count}} hodin"},
				relDay:    {One: "{{count}} den", Few: "{{count}} dny", Other: "{{count}} dní"},
				relWeek:   {One: "{{count}} týden", Few: "{{count}} týdny", Other: "{{count}} týdnů"},
				relMonth:  {One: "{{count}} měsíc", Few: "{{count}} měsíce", Other: "{{count}} měsíců"},
				relYear:   {One: "{{count}} rok", Few: "{{count}} roky", Other: "{{count}} let"},
			},
		},
	}},
	{tag: "uk", lang: "uk", rule: ruleEastSlavic, format: FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0", CurrencySpace: true,
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		Relative: RelativeSpec{
			Now: "щойно", Past: "{{d}} тому", Future: "через {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} хвилину", Few: "{{count}} хвилини", Many: "{{count}} хвилин", Other: "{{count}} хвилин"},
				relHour:   {One: "{{count}} годину", Few: "{{count}} години", Many: "{{count}} годин", Other: "{{count}} годин"},
				relDay:    {One: "{{count}} день", Few: "{{count}} дні", Many: "{{count}} днів", Other: "{{count}} днів"},
				relWeek:   {One: "{{count}} тиждень", Few: "{{count}} тижні", Many: "{{count}} тижнів", Other: "{{count}} тижнів"},
				relMonth:  {One: "{{count}} місяць", Few: "{{count}} місяці", Many: "{{count}} місяців", Other: "{{count}} місяців"},
				relYear:   {One: "{{count}} рік", Few: "{{count}} роки", Many: "{{count}} років", Other: "{{count}} років"},
			},
		},
	}},
	{tag: "ru", lang: "ru", rule: ruleEastSlavic, format: FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0", CurrencySpace: true,
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		Relative: RelativeSpec{
			Now: "только что", Past: "{{d}} назад", Future: "через {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} минуту", Few: "{{count}} минуты", Many: "{{count}} минут", Other: "{{count}} минут"},
				relHour:   {One: "{{count}} час", Few: "{{count}} часа", Many: "{{count}} часов", Other: "{{count}} часов"},
				relDay:    {One: "{{count}} день", Few: "{{count}} дня", Many: "{{count}} дней", Other: "{{count}} дней"},
				relWeek:   {One: "{{count}} неделю", Few: "{{count}} недели", Many: "{{count}} недель", Other: "{{count}} недель"},
				relMonth:  {One: "{{count}} месяц", Few: "{{count}} месяца", Many: "{{count}} месяцев", Other: "{{count}} месяцев"},
				relYear:   {One: "{{count}} год", Few: "{{count}} года", Many: "{{count}} лет", Other: "{{count}} лет"},
			},
		},
	}},
	{tag: "nl", lang: "nl", rule: ruleOneOther, format: FormatSpec{
		DecimalSep: ",", GroupSep: ".", CurrencyBefore: true, CurrencySpace: true,
		DateLayout: "02-01-2006", TimeLayout: "15:04", DateTimeLayout: "02-01-2006 15:04",
		Relative: RelativeSpec{
			Now: "zojuist", Past: "{{d}} geleden", Future: "over {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "{{count}} minuut", Other: "{{count}} minuten"},
				relHour:   {One: "{{count}} uur", Other: "{{count}} uur"},
				relDay:    {One: "{{count}} dag", Other: "{{count}} dagen"},
				relWeek:   {One: "{{count}} week", Other: "{{count}} weken"},
				relMonth:  {One: "{{count}} maand", Other: "{{count}} maanden"},
				relYear:   {One: "{{count}} jaar", Other: "{{count}} jaar"},
			},
		},
	}},
	{tag: "tr", lang: "tr", rule: ruleOneOther, format: FormatSpec{
		DecimalSep: ",", GroupSep: ".", CurrencyBefore: true,
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		Relative: RelativeSpec{
			Now: "az önce", Past: "{{d}} önce", Future: "{{d}} sonra",
			Units: [numRelUnits][numCategories]string{
				relMinute: {Other: "{{count}} dakika"},
				relHour:   {Other: "{{count}} saat"},
				relDay:    {Other: "{{count}} gün"},
				relWeek:   {Other: "{{count}} hafta"},
				relMonth:  {Other: "{{count}} ay"},
				relYear:   {Other: "{{count}} yıl"},
			},
		},
	}},
	{tag: "ar", lang: "ar", rule: ruleArabic, format: FormatSpec{
		DecimalSep: ".", GroupSep: ",", CurrencySpace: true,
		DateLayout: "02/01/2006", TimeLayout: "3:04 PM", DateTimeLayout: "02/01/2006 3:04 PM",
		Relative: RelativeSpec{
			Now: "الآن", Past: "قبل {{d}}", Future: "خلال {{d}}",
			Units: [numRelUnits][numCategories]string{
				relMinute: {One: "دقيقة واحدة", Two: "دقيقتين", Few: "{{count}} دقائق", Other: "{{count}} دقيقة"},
				relHour:   {One: "ساعة واحدة", Two: "ساعتين", Few: "{{count}} ساعات", Other: "{{count}} ساعة"},
				relDay:    {One: "يوم واحد", Two: "يومين", Few: "{{count}} أيام", Other: "{{count}} يوم"},
				relWeek:   {One: "أسبوع واحد", Two: "أسبوعين", Few: "{{count}} أسابيع", Other: "{{count}} أسبوع"},
				relMonth:  {One: "شهر واحد", Two: "شهرين", Few: "{{count}} أشهر", Other: "{{count}} شهر"},
				relYear:   {One: "سنة واحدة", Two: "سنتين", Few: "{{count}} سنوات", Other: "{{count}} سنة"},
			},
		},
	}},
	{tag: "ja", lang: "ja", rule: ruleOther, format: FormatSpec{
		DecimalSep: ".", GroupSep: ",", CurrencyBefore: true,
		DateLayout: "2006/01/02", TimeLayout: "15:04", DateTimeLayout: "2006/01/02 15:04",
		Relative: RelativeSpec{
			Now: "たった今", Past: "{{d}}前", Future: "{{d}}後",
			Units: [numRelUnits][numCategories]string{
				relMinute: {Other: "{{count}}分"},
				relHour:   {Other: "{{count}}時間"},
				relDay:    {Other: "{{count}}日"},
				relWeek:   {Other: "{{count}}週間"},
				relMonth:  {Other: "{{count}}か月"},
				relYear:   {Other: "{{count}}年"},
			},
		},
	}},
	{tag: "zh-CN", lang: "zh", rule: ruleOther, format: FormatSpec{
		DecimalSep: ".", GroupSep: ",", CurrencyBefore: true,
		DateLayout: "2006-01-02", TimeLayout: "15:04", DateTimeLayout: "2006-01-02 15:04",
		Relative: RelativeSpec{
			Now: "刚刚", Past: "{{d}}前", Future: "{{d}}后",
			Units: [numRelUnits][numCategories]string{
				relMinute: {Other: "{{count}}分钟"},
				relHour:   {Other: "{{count}}小时"},
				relDay:    {Other: "{{count}}天"},
				relWeek:   {Other: "{{count}}周"},
				relMonth:  {Other: "{{count}}个月"},
				relYear:   {Other: "{{count}}年"},
			},
		},
	}},
}
