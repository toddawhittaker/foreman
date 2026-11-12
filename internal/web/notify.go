package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

// PathNotifications is the page that sets where Foreman sends its events
// (SPEC 8.10 to 8.12).
const PathNotifications = "/settings/notifications"

// The audit actions of the notifications page (SPEC 12.9). Their entries
// name the webhook by id, preset and host, never its address (SPEC 14.8).
const (
	AuditWebhookAdd    = "webhook-add"
	AuditWebhookChange = "webhook-change"
	AuditWebhookDelete = "webhook-delete"
)

// The results the list can report after a change.
const (
	webhookDoneAdded   = "added"
	webhookDoneSaved   = "saved"
	webhookDoneDeleted = "deleted"
)

// ntfyExample is the sample topic address in the ntfy hint.
const ntfyExample = "https://ntfy.sh/your-topic"

// WebhookView is one webhook as the pages show it: the preset and host,
// never the address.
type WebhookView struct {
	ID       int64
	Preset   i18n.Msg
	Host     string
	LangTag  i18n.Locale
	LangName string
	AllOn    bool
	// Unreadable: the address cannot be opened, so nothing is sent until it
	// is typed again. Plain: the address is http://.
	Unreadable bool
	Plain      bool
	// Events names the events turned on, as one list in the page's language.
	Events string
}

// EditPath, DeletePath and the ids are the row's links.
func (v WebhookView) EditPath() string   { return webhookPath(v.ID) }
func (v WebhookView) DeletePath() string { return webhookPath(v.ID) + "/delete" }

func webhookPath(id int64) string { return PathNotifications + "/" + strconv.FormatInt(id, 10) }

// NotificationsPage lists the webhooks (DESIGN 8.16).
type NotificationsPage struct {
	NoKey bool
	Hooks []WebhookView
	Done  i18n.Msg
}

// Sending counts the webhooks whose address can be read, the ones that get
// messages.
func (p NotificationsPage) Sending() int {
	n := 0
	for _, h := range p.Hooks {
		if !h.Unreadable {
			n++
		}
	}
	return n
}

func (NotificationsPage) templateName() string { return "notifications" }
func (NotificationsPage) navItem() string      { return PathNotifications }

// AddPath is the add form.
func (NotificationsPage) AddPath() string { return PathNotifications + "/new" }

// PresetOption, EventOption and LangOption are the form's choices.
type PresetOption struct {
	Value   notify.Preset
	Name    i18n.Msg
	Hint    i18n.Msg
	Checked bool
}

type EventOption struct {
	Value   notify.Event
	Label   i18n.Msg
	Checked bool
}

type LangOption struct {
	Tag      i18n.Locale
	Name     string
	Selected bool
}

// WebhookFormPage adds or edits a webhook. The address is never put back
// in the page (DESIGN 8.16).
type WebhookFormPage struct {
	// Edit is set on the edit page, with the stored preset and host.
	Edit    bool
	ID      int64
	Kept    WebhookView
	Presets []PresetOption
	Events  []EventOption
	Langs   []LangOption

	PresetError i18n.Msg
	URLError    i18n.Msg
	EventsError i18n.Msg
	LangError   i18n.Msg
}

func (WebhookFormPage) templateName() string { return "notificationform" }
func (WebhookFormPage) navItem() string      { return PathNotifications }
func (p WebhookFormPage) hasErrors() bool    { return len(p.Problems()) > 0 }

// Path is where the form posts.
func (p WebhookFormPage) Path() string {
	if p.Edit {
		return webhookPath(p.ID)
	}
	return PathNotifications + "/new"
}

// BackPath is the list.
func (WebhookFormPage) BackPath() string { return PathNotifications }

// URLRequired is true when there is no readable address to keep.
func (p WebhookFormPage) URLRequired() bool { return !p.Edit || p.Kept.Unreadable }

// PlainNote shows the unencrypted note at first: the stored address is
// http://. A script shows and hides it as an address is typed.
func (p WebhookFormPage) PlainNote() bool { return p.Edit && p.Kept.Plain && !p.Kept.Unreadable }

// URLDescribedBy is the address field's aria-describedby.
func (p WebhookFormPage) URLDescribedBy() string {
	var ids []string
	if p.Edit {
		ids = append(ids, "webhook-url-hint")
	}
	if p.PlainNote() {
		ids = append(ids, "webhook-url-plain")
	}
	if !p.URLError.IsZero() {
		ids = append(ids, "webhook-url-error")
	}
	return strings.Join(ids, " ")
}

// Problems lists the form's errors in the order the fields appear.
func (p WebhookFormPage) Problems() []Problem {
	return problems(
		Problem{"preset-" + string(notify.Discord), p.PresetError},
		Problem{"webhook-url", p.URLError},
		Problem{"webhook-event-" + string(notify.Start), p.EventsError},
		Problem{"webhook-lang", p.LangError},
	)
}

// WebhookDeletePage confirms a delete (DESIGN 7.4, 8.16).
type WebhookDeletePage struct {
	Hook WebhookView
}

func (WebhookDeletePage) templateName() string { return "notificationdelete" }
func (WebhookDeletePage) navItem() string      { return PathNotifications }

// BackPath is the list.
func (WebhookDeletePage) BackPath() string { return PathNotifications }

func (s *server) notifyRoutes() {
	s.mux.HandleFunc("GET "+PathNotifications, s.requireUser(s.handleNotifications))
	s.mux.HandleFunc("GET "+PathNotifications+"/new", s.requireUser(s.handleWebhookNew))
	s.mux.HandleFunc("POST "+PathNotifications+"/new", s.requireUser(s.handleWebhookAdd))
	s.mux.HandleFunc("GET "+PathNotifications+"/{id}", s.requireUser(s.handleWebhookEditForm))
	s.mux.HandleFunc("POST "+PathNotifications+"/{id}", s.requireUser(s.handleWebhookEdit))
	s.mux.HandleFunc("GET "+PathNotifications+"/{id}/delete", s.requireUser(s.handleWebhookDeleteForm))
	s.mux.HandleFunc("POST "+PathNotifications+"/{id}/delete", s.requireUser(s.handleWebhookDelete))
}

func presetName(p notify.Preset) i18n.Msg {
	switch p {
	case notify.Ntfy:
		return i18n.M("notify.preset.ntfy")
	case notify.Generic:
		return i18n.M("notify.preset.generic")
	}
	return i18n.M("notify.preset.discord")
}

func presetHint(p notify.Preset) i18n.Msg {
	switch p {
	case notify.Ntfy:
		return i18n.M("notify.preset.ntfy.hint", "example", i18n.Code(ntfyExample))
	case notify.Generic:
		return i18n.M("notify.preset.generic.hint")
	}
	return i18n.M("notify.preset.discord.hint")
}

func eventLabel(e notify.Event) i18n.Msg {
	switch e {
	case notify.Stop:
		return i18n.M("notify.event.stop")
	case notify.Crash:
		return i18n.M("notify.event.crash")
	case notify.CrashLoop:
		return i18n.M("notify.event.crash_loop")
	case notify.PlayerJoin:
		return i18n.M("notify.event.player_join")
	case notify.PlayerLeave:
		return i18n.M("notify.event.player_leave")
	case notify.ScheduledRestart:
		return i18n.M("notify.event.scheduled_restart")
	case notify.LowDisk:
		return i18n.M("notify.event.low_disk")
	}
	return i18n.M("notify.event.start")
}

func eventShort(e notify.Event) i18n.Msg {
	switch e {
	case notify.Stop:
		return i18n.M("notify.event.short.stop")
	case notify.Crash:
		return i18n.M("notify.event.short.crash")
	case notify.CrashLoop:
		return i18n.M("notify.event.short.crash_loop")
	case notify.PlayerJoin:
		return i18n.M("notify.event.short.player_join")
	case notify.PlayerLeave:
		return i18n.M("notify.event.short.player_leave")
	case notify.ScheduledRestart:
		return i18n.M("notify.event.short.scheduled_restart")
	case notify.LowDisk:
		return i18n.M("notify.event.short.low_disk")
	}
	return i18n.M("notify.event.short.start")
}

func languageName(l i18n.Locale) string {
	for _, lang := range i18n.Languages {
		if lang.Tag == l {
			return lang.Name
		}
	}
	return string(l)
}

// webhookView makes the row for w in loc's language.
func webhookView(loc i18n.Locale, w notify.Webhook) WebhookView {
	v := WebhookView{ID: w.ID, Preset: presetName(w.Preset), Host: w.Host, LangTag: w.Lang, LangName: languageName(w.Lang), AllOn: w.AllOn(),
		Unreadable: w.Unreadable, Plain: w.Plain}
	var names []string
	for _, e := range w.OnEvents() {
		names = append(names, loc.Format(eventShort(e)))
	}
	v.Events = upperFirst(strings.Join(names, loc.T("common.list.sep")))
	return v
}

// upperFirst starts a list of lower-case event names with a capital.
func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

func (s *server) handleNotifications(w http.ResponseWriter, r *http.Request, u store.User) {
	p := NotificationsPage{NoKey: !s.notifier.CanStore()}
	if !p.NoKey {
		hooks, err := s.notifier.List(r.Context())
		if err != nil {
			s.fail(w, r, "list the webhooks", err)
			return
		}
		loc := localeOf(r)
		for _, h := range hooks {
			p.Hooks = append(p.Hooks, webhookView(loc, h))
		}
		p.Done = webhookDone(r.URL.Query())
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// reHost is what a host in a done link may hold; anything else shows no
// message.
var reHost = regexp.MustCompile(`^[a-z0-9.:\[\]-]{1,253}$`)

// webhookDone is the Done message a change left in the list's address.
func webhookDone(q url.Values) i18n.Msg {
	preset, host := notify.Preset(q.Get("preset")), q.Get("host")
	if !preset.Known() || !reHost.MatchString(host) {
		return i18n.Msg{}
	}
	name := presetName(preset)
	switch q.Get("done") {
	case webhookDoneAdded:
		return i18n.M("notify.done.added", "preset", name, "host", host)
	case webhookDoneSaved:
		return i18n.M("notify.done.saved", "preset", name, "host", host)
	case webhookDoneDeleted:
		return i18n.M("notify.done.deleted", "preset", name, "host", host)
	}
	return i18n.Msg{}
}

func webhookDoneURL(done string, w notify.Webhook) string {
	q := url.Values{"done": {done}, "preset": {string(w.Preset)}, "host": {w.Host}}
	return PathNotifications + "?" + q.Encode()
}

// withKey sends a request for a form back to the list, which explains,
// when no webhook can be stored.
func (s *server) withKey(w http.ResponseWriter, r *http.Request) bool {
	if s.notifier.CanStore() {
		return true
	}
	http.Redirect(w, r, PathNotifications, http.StatusSeeOther)
	return false
}

// formPage is the form with the choices given.
func formPage(preset notify.Preset, on map[notify.Event]bool, lang i18n.Locale) WebhookFormPage {
	var p WebhookFormPage
	for _, v := range notify.Presets {
		p.Presets = append(p.Presets, PresetOption{Value: v, Name: presetName(v), Hint: presetHint(v), Checked: v == preset})
	}
	for _, e := range notify.Events {
		p.Events = append(p.Events, EventOption{Value: e, Label: eventLabel(e), Checked: on[e]})
	}
	for _, l := range i18n.Languages {
		p.Langs = append(p.Langs, LangOption{Tag: l.Tag, Name: l.Name, Selected: l.Tag == lang})
	}
	return p
}

func allEventsOn() map[notify.Event]bool {
	on := map[notify.Event]bool{}
	for _, e := range notify.Events {
		on[e] = true
	}
	return on
}

func (s *server) handleWebhookNew(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.withKey(w, r) {
		return
	}
	// The language defaults to the adding admin's (DESIGN 8.16).
	s.page(w, r, http.StatusOK, u.Username, formPage(notify.Discord, allEventsOn(), localeOf(r)))
}

// postedSetup reads the form. The address is wrapped at once so it can
// never be printed.
func postedSetup(r *http.Request) notify.Setup {
	on := map[notify.Event]bool{}
	for _, v := range r.PostForm["events"] {
		if e := notify.Event(v); e.Known() {
			on[e] = true
		}
	}
	return notify.Setup{
		Preset: notify.Preset(r.PostFormValue("preset")),
		URL:    secret.New(strings.TrimSpace(r.PostFormValue("url"))),
		Events: on,
		Lang:   i18n.Locale(r.PostFormValue("lang")),
	}
}

// checkSetup finds the problems a page can show before anything is stored.
// kept is the edit page's stored webhook, nil when adding.
func checkSetup(st notify.Setup, kept *notify.Webhook) WebhookFormPage {
	p := formPage(st.Preset, st.Events, st.Lang)
	if !st.Preset.Known() {
		p.PresetError = i18n.M("notify.preset.invalid")
	}
	raw := st.URL.Reveal()
	switch {
	case raw != "":
		if st.Preset.Known() {
			if _, err := notify.CheckURL(st.Preset, raw); err != nil {
				p.URLError = urlProblem(err)
			}
		}
	case kept == nil || kept.Unreadable:
		p.URLError = i18n.M("notify.url.missing")
	case st.Preset.Known() && st.Preset != kept.Preset:
		p.URLError = i18n.M("notify.url.preset")
	}
	if len(st.Events) == 0 {
		p.EventsError = i18n.M("notify.events.none")
	}
	if _, ok := i18n.Parse(string(st.Lang)); !ok {
		p.LangError = i18n.M("notify.lang.invalid")
	}
	// The field comes back empty, so an address that was fine must be typed again.
	if raw != "" && p.URLError.IsZero() && len(p.Problems()) > 0 {
		p.URLError = i18n.M("notify.url.retype")
	}
	return p
}

// urlProblem says what to fix in an address without echoing it.
func urlProblem(err error) i18n.Msg {
	switch {
	case errors.Is(err, notify.ErrURLMissing):
		return i18n.M("notify.url.missing")
	case errors.Is(err, notify.ErrURLTooLong):
		return i18n.M("notify.url.long", "count", notify.MaxURLLength)
	case errors.Is(err, notify.ErrURLScheme):
		return i18n.M("notify.url.scheme")
	case errors.Is(err, notify.ErrURLHasUser):
		return i18n.M("notify.url.user")
	case errors.Is(err, notify.ErrURLNotDiscord):
		return i18n.M("notify.url.discord")
	case errors.Is(err, notify.ErrURLNoTopic):
		return i18n.M("notify.url.ntfy")
	}
	return i18n.M("notify.url.invalid")
}

func webhookAudit(w notify.Webhook) string {
	return fmt.Sprintf("webhook %d: %s, %s", w.ID, w.Preset, w.Host)
}

func (s *server) handleWebhookAdd(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) || !s.withKey(w, r) {
		return
	}
	st := postedSetup(r)
	if p := checkSetup(st, nil); p.hasErrors() {
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	hook, err := s.notifier.Add(r.Context(), st, u.Username)
	if err != nil {
		s.fail(w, r, "add a webhook", err)
		return
	}
	s.audit(r.Context(), u, AuditWebhookAdd, "", webhookAudit(hook))
	http.Redirect(w, r, webhookDoneURL(webhookDoneAdded, hook), http.StatusSeeOther)
}

// webhook finds the webhook the path names, or answers 404.
func (s *server) webhook(w http.ResponseWriter, r *http.Request, u store.User) (notify.Webhook, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.notFound(w, r, u)
		return notify.Webhook{}, false
	}
	hook, err := s.notifier.Get(r.Context(), id)
	if errors.Is(err, notify.ErrNoWebhook) {
		s.notFound(w, r, u)
		return notify.Webhook{}, false
	}
	if err != nil {
		s.fail(w, r, "read a webhook", err)
		return notify.Webhook{}, false
	}
	return hook, true
}

func editPage(loc i18n.Locale, p WebhookFormPage, hook notify.Webhook) WebhookFormPage {
	p.Edit, p.ID, p.Kept = true, hook.ID, webhookView(loc, hook)
	return p
}

func (s *server) handleWebhookEditForm(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.withKey(w, r) {
		return
	}
	hook, ok := s.webhook(w, r, u)
	if !ok {
		return
	}
	on := map[notify.Event]bool{}
	for _, e := range hook.OnEvents() {
		on[e] = true
	}
	s.page(w, r, http.StatusOK, u.Username, editPage(localeOf(r), formPage(hook.Preset, on, hook.Lang), hook))
}

func (s *server) handleWebhookEdit(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) || !s.withKey(w, r) {
		return
	}
	hook, ok := s.webhook(w, r, u)
	if !ok {
		return
	}
	st := postedSetup(r)
	if p := checkSetup(st, &hook); p.hasErrors() {
		s.page(w, r, http.StatusBadRequest, u.Username, editPage(localeOf(r), p, hook))
		return
	}
	saved, err := s.notifier.Update(r.Context(), hook.ID, st, u.Username)
	if errors.Is(err, notify.ErrNoWebhook) {
		s.notFound(w, r, u)
		return
	}
	if err != nil {
		s.fail(w, r, "save a webhook", err)
		return
	}
	s.audit(r.Context(), u, AuditWebhookChange, "", webhookAudit(saved))
	http.Redirect(w, r, webhookDoneURL(webhookDoneSaved, saved), http.StatusSeeOther)
}

func (s *server) handleWebhookDeleteForm(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.withKey(w, r) {
		return
	}
	hook, ok := s.webhook(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, WebhookDeletePage{Hook: webhookView(localeOf(r), hook)})
}

func (s *server) handleWebhookDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) || !s.withKey(w, r) {
		return
	}
	hook, ok := s.webhook(w, r, u)
	if !ok {
		return
	}
	err := s.notifier.Delete(r.Context(), hook.ID)
	if errors.Is(err, notify.ErrNoWebhook) {
		s.notFound(w, r, u)
		return
	}
	if err != nil {
		s.fail(w, r, "delete a webhook", err)
		return
	}
	s.audit(r.Context(), u, AuditWebhookDelete, "", webhookAudit(hook))
	http.Redirect(w, r, webhookDoneURL(webhookDoneDeleted, hook), http.StatusSeeOther)
}
