package main

import (
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/web/csrf"
	"github.com/dmitrymomot/forge/web/render"
	"github.com/dmitrymomot/forge/web/smartlink"
)

//go:embed templates
var templatesFS embed.FS

func mustPage(page string) *template.Template {
	return template.Must(template.ParseFS(templatesFS, "templates/layout.html", "templates/"+page))
}

var (
	indexTmpl    = mustPage("index.html")
	linkTmpl     = mustPage("link.html")
	newLinkTmpl  = mustPage("new_link.html")
	newOfferTmpl = mustPage("new_offer.html")
)

// Dashboard serves the management UI on the app host: link/offer listings
// with click stats, and the create forms.
type Dashboard struct {
	manager *smartlink.Manager
	offers  *OfferStore
	cache   *smartlink.Cache
	tracker *Tracker
	log     *slog.Logger
}

func (d *Dashboard) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", d.index)
	mux.HandleFunc("GET /links/new", d.newLinkForm)
	mux.HandleFunc("POST /links", d.createLink)
	mux.HandleFunc("GET /links/{code}", d.linkDetail)
	mux.HandleFunc("GET /offers/new", d.newOfferForm)
	mux.HandleFunc("POST /offers", d.createOffer)
	return mux
}

type linkRow struct {
	OfferName string
	Link      smartlink.Link
	Stats     ClickCounts
}

type indexData struct {
	Links  []linkRow
	Offers []Offer
}

func (d *Dashboard) index(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	links, err := d.manager.List(ctx, smartlink.Filter{})
	if err != nil {
		d.serverError(w, "list links", err)
		return
	}
	counts, err := d.tracker.Summaries(ctx)
	if err != nil {
		d.serverError(w, "load click counts", err)
		return
	}
	offers, err := d.offers.List(ctx)
	if err != nil {
		d.serverError(w, "list offers", err)
		return
	}
	names := make(map[string]string, len(offers))
	for _, o := range offers {
		names[o.Ref] = o.Name
	}
	rows := make([]linkRow, 0, len(links))
	for _, l := range links {
		rows = append(rows, linkRow{Link: l, Stats: counts[l.Code], OfferName: names[l.Ref]})
	}
	d.render(w, http.StatusOK, indexTmpl, indexData{Links: rows, Offers: offers})
}

type linkData struct {
	OfferName string
	Link      smartlink.Link
	Stats     LinkStats
}

func (d *Dashboard) linkDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := d.manager.Get(ctx, r.PathValue("code"))
	if err != nil {
		if errors.Is(err, smartlink.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		d.serverError(w, "get link", err)
		return
	}
	stats, err := d.tracker.Stats(ctx, l.Code)
	if err != nil {
		d.serverError(w, "load stats", err)
		return
	}
	d.render(w, http.StatusOK, linkTmpl, linkData{Link: l, Stats: stats, OfferName: d.offerName(r, l)})
}

type newLinkData struct {
	Error    string
	Code     string
	Ref      string
	Target   string
	Campaign string
	CSRF     string
	Offers   []Offer
}

func (d *Dashboard) newLinkForm(w http.ResponseWriter, r *http.Request) {
	offers, err := d.offers.List(r.Context())
	if err != nil {
		d.serverError(w, "list offers", err)
		return
	}
	d.render(w, http.StatusOK, newLinkTmpl, newLinkData{Offers: offers, CSRF: csrf.Token(r)})
}

func (d *Dashboard) createLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	offers, err := d.offers.List(ctx)
	if err != nil {
		d.serverError(w, "list offers", err)
		return
	}
	form := newLinkData{
		Code:     strings.TrimSpace(r.FormValue("code")),
		Ref:      r.FormValue("ref"),
		Target:   strings.TrimSpace(r.FormValue("target")),
		Campaign: strings.TrimSpace(r.FormValue("campaign")),
		CSRF:     csrf.Token(r),
		Offers:   offers,
	}
	p := smartlink.CreateParams{Code: form.Code, Ref: form.Ref, Target: form.Target}
	if form.Campaign != "" {
		p.Metadata = map[string]string{"campaign": form.Campaign}
	}
	if _, err := d.manager.Create(ctx, p); err != nil {
		if errors.Is(err, smartlink.ErrInvalidLink) || errors.Is(err, smartlink.ErrCodeReserved) || errors.Is(err, smartlink.ErrDuplicate) {
			form.Error = err.Error()
			d.render(w, http.StatusUnprocessableEntity, newLinkTmpl, form)
			return
		}
		d.serverError(w, "create link", err)
		return
	}
	render.Redirect(w, r, http.StatusSeeOther, "/")
}

// offerRowForm is one editable geo-rule row of the offer form.
type offerRowForm struct {
	Countries string
	URL       string
	Index     int
}

type newOfferData struct {
	Error      string
	Name       string
	DefaultURL string
	CSRF       string
	Rows       []offerRowForm
}

const offerFormRows = 3

func emptyOfferForm() newOfferData {
	rows := make([]offerRowForm, offerFormRows)
	for i := range rows {
		rows[i].Index = i
	}
	return newOfferData{Rows: rows}
}

func (d *Dashboard) newOfferForm(w http.ResponseWriter, r *http.Request) {
	form := emptyOfferForm()
	form.CSRF = csrf.Token(r)
	d.render(w, http.StatusOK, newOfferTmpl, form)
}

func (d *Dashboard) createOffer(w http.ResponseWriter, r *http.Request) {
	form := emptyOfferForm()
	form.CSRF = csrf.Token(r)
	form.Name = strings.TrimSpace(r.FormValue("name"))
	form.DefaultURL = strings.TrimSpace(r.FormValue("default_url"))
	for i := range form.Rows {
		form.Rows[i].Countries = strings.TrimSpace(r.FormValue("rule_countries_" + strconv.Itoa(i)))
		form.Rows[i].URL = strings.TrimSpace(r.FormValue("rule_url_" + strconv.Itoa(i)))
	}

	o, err := offerFromForm(form)
	if err != nil {
		form.Error = err.Error()
		d.render(w, http.StatusUnprocessableEntity, newOfferTmpl, form)
		return
	}
	if err := d.offers.Save(r.Context(), o); err != nil {
		d.serverError(w, "save offer", err)
		return
	}
	// Offer-save path invalidates the compile cache so the next click
	// resolves the fresh spec (a no-op for a brand-new ref).
	d.cache.Invalidate(o.Ref)
	render.Redirect(w, r, http.StatusSeeOther, "/")
}

// offerFromForm validates the form into an Offer, compiling the resulting
// Spec so a bad country code or URL template is rejected before saving.
func offerFromForm(form newOfferData) (Offer, error) {
	if form.Name == "" {
		return Offer{}, errors.New("name is required")
	}
	if err := checkHTTPURL(form.DefaultURL); err != nil {
		return Offer{}, err
	}
	var rules []GeoRule
	for _, row := range form.Rows {
		if row.Countries == "" && row.URL == "" {
			continue
		}
		if row.Countries == "" || row.URL == "" {
			return Offer{}, errors.New("a rule needs both countries and a URL")
		}
		if err := checkHTTPURL(row.URL); err != nil {
			return Offer{}, err
		}
		rules = append(rules, GeoRule{Countries: splitCountries(row.Countries), URL: row.URL})
	}
	o := Offer{
		CreatedAt:  time.Now(),
		Ref:        id.NewPrefixed("offer"),
		Name:       form.Name,
		DefaultURL: form.DefaultURL,
		Rules:      rules,
	}
	if _, err := smartlink.Compile(o.Spec()); err != nil {
		return Offer{}, err
	}
	return o, nil
}

// checkHTTPURL mirrors the Manager's scheme allowlist for offer targets: Ref
// specs bypass Create's Target validation, so the offer form enforces it.
func checkHTTPURL(u string) error {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("destination URLs must start with http:// or https://")
	}
	return nil
}

func splitCountries(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

func (d *Dashboard) offerName(r *http.Request, l smartlink.Link) string {
	if l.Ref == "" {
		return ""
	}
	o, err := d.offers.Get(r.Context(), l.Ref)
	if err != nil {
		return l.Ref
	}
	return o.Name
}

func (d *Dashboard) render(w http.ResponseWriter, status int, t *template.Template, data any) {
	if err := render.HTML(w, status, t, "layout", data); err != nil {
		d.log.Error("dashboard: render", "error", err)
	}
}

func (d *Dashboard) serverError(w http.ResponseWriter, op string, err error) {
	d.log.Error("dashboard: "+op, "error", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
