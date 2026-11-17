package web

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/schedule"
	"github.com/toddawhittaker/foreman/internal/secret"
)

// The Foreman-wide settings tools (SPEC 21.7): the restart schedule (7.12),
// webhooks (8.10, 8.11), the factorio.com account (9.11) and the public mods
// page (9.16). Each calls the code its page calls. A webhook's address and
// the account's username and token go in and never come back (SPEC 14.8).

func (s *server) addForemanTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "foreman_settings", Annotations: readOnly,
		Description: "Show Foreman's own settings: the restart schedule, the webhooks (preset, host, language and events, never the address), " +
			"where the factorio.com account comes from (never its username or token) and whether the public mods page is on."}, s.mcpForemanSettings)
	mcp.AddTool(srv, &mcp.Tool{Name: "set_restart_schedule", Annotations: oneStep,
		Description: fmt.Sprintf("Set the daily restart schedule, as Foreman's Restart schedule page does. time is HH:MM, zone an IANA zone such as UTC or Europe/London, "+
			"limit_hours how long a restart may wait for players to leave, 0 to %d. A field left out keeps its saved value. "+
			"With enabled false the other fields are ignored and the schedule is off.", schedule.MaxLimitHours)}, s.mcpSetSchedule)
	mcp.AddTool(srv, &mcp.Tool{Name: "add_webhook", Annotations: oneStep,
		Description: "Add a webhook that Foreman sends its events to. url is the address; it is kept sealed and never shown again, so no tool returns it. " +
			"preset is " + presetList() + "; events lists " + eventList() + " and defaults to the ones the Notifications page starts with; lang is a language tag such as en, default en."}, s.mcpAddWebhook)
	mcp.AddTool(srv, &mcp.Tool{Name: "change_webhook", Annotations: oneStep,
		Description: "Change a webhook. Leave url out to keep its stored address; a new one replaces it and is never shown again. " +
			"A preset, events or lang left out keeps its saved value. webhook_id is from foreman_settings."}, s.mcpChangeWebhook)
	mcp.AddTool(srv, &mcp.Tool{Name: "delete_webhook", Annotations: twoStep,
		Description: "Delete a webhook, in two calls: the first returns a preview and a confirm_code and changes nothing; " +
			"call again with the same arguments and the code to delete it. The address cannot be recovered, so adding it again needs it."}, s.mcpDeleteWebhook)
	mcp.AddTool(srv, &mcp.Tool{Name: "set_factorio_account", Annotations: oneStep,
		Description: "Store the factorio.com username and token Foreman uses to download mods, replacing any stored account. " +
			"Both are write-only: nothing is returned, and foreman_settings shows only where the account comes from. " +
			"Refused when the account is set by environment variables or FOREMAN_SECRET_KEY is not set."}, s.mcpSetAccount)
	mcp.AddTool(srv, &mcp.Tool{Name: "remove_factorio_account", Annotations: oneStep,
		Description: "Remove the stored factorio.com account. An account set by environment variables cannot be removed here."}, s.mcpRemoveAccount)
	mcp.AddTool(srv, &mcp.Tool{Name: "set_public_mods", Annotations: oneStep,
		Description: "Turn the public mods page on or off, as Foreman's Public mods setting does."}, s.mcpSetPublicMods)
}

// presetList and eventList name the choices a webhook tool accepts.
func presetList() string {
	names := make([]string, len(notify.Presets))
	for i, p := range notify.Presets {
		names[i] = string(p)
	}
	return strings.Join(names, ", ")
}

func eventList() string {
	names := make([]string, len(notify.Events))
	for i, e := range notify.Events {
		names[i] = string(e)
	}
	return strings.Join(names, ", ")
}

type mcpScheduleOut struct {
	// Configured is false until a schedule was first saved; the fields then
	// hold the defaults a new schedule starts with.
	Configured bool   `json:"configured"`
	Enabled    bool   `json:"enabled"`
	Time       string `json:"time"`
	Zone       string `json:"zone"`
	LimitHours int    `json:"limit_hours"`
	// NextRestart is when the next scheduled restart falls, or "" when the
	// schedule is off.
	NextRestart string `json:"next_restart,omitempty"`
}

// mcpWebhook is a webhook as a result shows it: never its address.
type mcpWebhook struct {
	ID       int64    `json:"id"`
	Preset   string   `json:"preset"`
	Host     string   `json:"host"`
	Language string   `json:"language"`
	Events   []string `json:"events"`
	// Unreadable: the stored address cannot be opened, so nothing is sent
	// until change_webhook gives it again. Plain: the address is http://.
	Unreadable bool `json:"unreadable"`
	Plain      bool `json:"plain_http"`
}

func mcpWebhookOf(w notify.Webhook) mcpWebhook {
	out := mcpWebhook{ID: w.ID, Preset: string(w.Preset), Host: w.Host, Language: string(w.Lang), Events: []string{}, Unreadable: w.Unreadable, Plain: w.Plain}
	for _, e := range w.OnEvents() {
		out.Events = append(out.Events, string(e))
	}
	return out
}

type mcpForemanOut struct {
	// RestartSchedule is left out when this mode has no scheduler.
	RestartSchedule *mcpScheduleOut `json:"restart_schedule,omitempty"`
	Webhooks        struct {
		// CanStore is false without FOREMAN_SECRET_KEY, which webhook
		// addresses are sealed with.
		CanStore bool         `json:"can_store"`
		Hooks    []mcpWebhook `json:"hooks"`
	} `json:"webhooks"`
	FactorioAccount struct {
		// Source is environment, stored, unreadable (stored but cannot be
		// opened) or none.
		Source   string `json:"source"`
		CanStore bool   `json:"can_store"`
	} `json:"factorio_account"`
	PublicMods bool `json:"public_mods"`
}

// accountSource names where the account comes from.
func accountSource(src factorioaccount.Source) string {
	switch src {
	case factorioaccount.Env:
		return "environment"
	case factorioaccount.Stored:
		return "stored"
	case factorioaccount.Unreadable:
		return "unreadable"
	}
	return "none"
}

// scheduleOut is the saved schedule as a result shows it.
func (s *server) scheduleOut() mcpScheduleOut {
	r, found := s.scheduler.Settings()
	out := mcpScheduleOut{Configured: found, Enabled: r.Enabled, Time: clockText(r.Minute), Zone: r.Zone, LimitHours: r.LimitHours}
	if r.Enabled {
		out.NextRestart = mcpTime(s.scheduler.State().Next)
	}
	return out
}

var errNoScheduler = errors.New("the restart schedule is not available in this mode")

func (s *server) mcpForemanSettings(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpForemanOut, error) {
	var out mcpForemanOut
	if s.scheduler != nil {
		sched := s.scheduleOut()
		out.RestartSchedule = &sched
	}
	out.Webhooks.CanStore = s.notifier.CanStore()
	out.Webhooks.Hooks = []mcpWebhook{}
	hooks, err := s.notifier.List(ctx)
	if err != nil {
		return nil, out, s.mcpFail("list the webhooks", err)
	}
	for _, h := range hooks {
		out.Webhooks.Hooks = append(out.Webhooks.Hooks, mcpWebhookOf(h))
	}
	acct, err := s.accountState(ctx)
	if err != nil {
		return nil, out, s.mcpFail("read the factorio.com account", err)
	}
	out.FactorioAccount.Source, out.FactorioAccount.CanStore = accountSource(acct.Source), acct.CanStore
	out.PublicMods = s.publicModsOn(ctx)
	return mcpText("Foreman has %d webhooks; the factorio.com account is %s; the public mods page is %s.",
		len(hooks), out.FactorioAccount.Source, onOff(out.PublicMods)), out, nil
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// mcpProblemError words a form's problems in English, in the order the
// form shows them. A problem never holds what was sent.
func mcpProblemError(what string, ps []Problem) error {
	var msgs []string
	for _, p := range ps {
		msgs = append(msgs, inEnglish(p.Message))
	}
	return fmt.Errorf("%s: %s", what, strings.Join(msgs, " "))
}

type mcpScheduleIn struct {
	Enabled    bool   `json:"enabled" jsonschema:"whether Foreman restarts the world every day"`
	Time       string `json:"time,omitempty" jsonschema:"the time of day, HH:MM, 24 hours"`
	Zone       string `json:"zone,omitempty" jsonschema:"the time zone, UTC or an IANA name such as Europe/London"`
	LimitHours *int   `json:"limit_hours,omitempty" jsonschema:"hours a restart may wait for players to leave"`
}

func (s *server) mcpSetSchedule(ctx context.Context, req *mcp.CallToolRequest, in mcpScheduleIn) (*mcp.CallToolResult, mcpScheduleOut, error) {
	if s.scheduler == nil {
		return nil, mcpScheduleOut{}, errNoScheduler
	}
	// A field left out keeps its saved value, or the default of a schedule
	// never saved.
	cur := s.schedulePage()
	form := scheduleIn{Enabled: in.Enabled, Clock: cur.Clock, Zone: cur.Zone, Limit: cur.Limit}
	if in.Time != "" {
		form.Clock = in.Time
	}
	if in.Zone != "" {
		form.Zone = in.Zone
	}
	if in.LimitHours != nil {
		form.Limit = strconv.Itoa(*in.LimitHours)
	}
	p, err := s.saveSchedule(ctx, mcpActor(req), form)
	if err != nil {
		return nil, mcpScheduleOut{}, s.mcpFail("save the restart schedule", err)
	}
	if p.hasErrors() {
		return nil, mcpScheduleOut{}, mcpProblemError("the restart schedule was not saved", p.Problems())
	}
	out := s.scheduleOut()
	if !out.Enabled {
		return mcpText("The restart schedule is off."), out, nil
	}
	return mcpText("Foreman restarts the world every day at %s %s, waiting up to %d hours for players to leave.", out.Time, out.Zone, out.LimitHours), out, nil
}

// errNoWebhookKey is the refusal when no webhook can be stored.
var errNoWebhookKey = errors.New("webhooks cannot be stored because FOREMAN_SECRET_KEY is not set; the operator guide says how to set it")

type mcpAddWebhookIn struct {
	Preset string   `json:"preset" jsonschema:"discord, ntfy or generic"`
	URL    string   `json:"url,omitempty" jsonschema:"the address to send to, required; kept sealed and never returned"`
	Events []string `json:"events,omitempty" jsonschema:"the events to send; the Notifications page's defaults when left out"`
	Lang   string   `json:"lang,omitempty" jsonschema:"the language of the messages, a tag such as en; en when left out"`
}

type mcpChangeWebhookIn struct {
	WebhookID int64    `json:"webhook_id" jsonschema:"the webhook's id from foreman_settings"`
	Preset    string   `json:"preset,omitempty" jsonschema:"discord, ntfy or generic; the saved one when left out"`
	URL       string   `json:"url,omitempty" jsonschema:"a new address, kept sealed and never returned; the stored address is kept when left out"`
	Events    []string `json:"events,omitempty" jsonschema:"the events to send; the saved ones when left out"`
	Lang      string   `json:"lang,omitempty" jsonschema:"the language of the messages; the saved one when left out"`
}

type mcpWebhookOut struct {
	Webhook mcpWebhook `json:"webhook"`
}

// mcpEvents reads event names; any name that is not an event is refused
// without being repeated.
func mcpEvents(names []string) (map[notify.Event]bool, error) {
	on := map[notify.Event]bool{}
	for _, n := range names {
		e := notify.Event(n)
		if !e.Known() {
			return nil, fmt.Errorf("events holds a name that is not an event; the events are %s", eventList())
		}
		on[e] = true
	}
	return on, nil
}

func (s *server) mcpAddWebhook(ctx context.Context, req *mcp.CallToolRequest, in mcpAddWebhookIn) (*mcp.CallToolResult, mcpWebhookOut, error) {
	if !s.notifier.CanStore() {
		return nil, mcpWebhookOut{}, errNoWebhookKey
	}
	on := defaultEvents()
	if in.Events != nil {
		var err error
		if on, err = mcpEvents(in.Events); err != nil {
			return nil, mcpWebhookOut{}, err
		}
	}
	lang := i18n.Locale(in.Lang)
	if in.Lang == "" {
		lang = i18n.English
	}
	st := notify.Setup{Preset: notify.Preset(in.Preset), URL: secret.New(strings.TrimSpace(in.URL)), Events: on, Lang: lang}
	hook, p, err := s.addWebhook(ctx, mcpActor(req), st)
	if err != nil {
		return nil, mcpWebhookOut{}, s.mcpFail("add the webhook", err)
	}
	if p.hasErrors() {
		return nil, mcpWebhookOut{}, mcpProblemError("the webhook was not added", p.Problems())
	}
	return mcpText("Webhook %d was added: %s, %s.", hook.ID, hook.Preset, hook.Host), mcpWebhookOut{Webhook: mcpWebhookOf(hook)}, nil
}

// mcpWebhookNamed finds the webhook a call names.
func (s *server) mcpWebhookNamed(ctx context.Context, id int64) (notify.Webhook, error) {
	if !s.notifier.CanStore() {
		return notify.Webhook{}, errNoWebhookKey
	}
	hook, err := s.notifier.Get(ctx, id)
	if errors.Is(err, notify.ErrNoWebhook) {
		return notify.Webhook{}, fmt.Errorf("no webhook has the id %d; foreman_settings lists them", id)
	}
	if err != nil {
		return notify.Webhook{}, s.mcpFail("read the webhook", err)
	}
	return hook, nil
}

func (s *server) mcpChangeWebhook(ctx context.Context, req *mcp.CallToolRequest, in mcpChangeWebhookIn) (*mcp.CallToolResult, mcpWebhookOut, error) {
	hook, err := s.mcpWebhookNamed(ctx, in.WebhookID)
	if err != nil {
		return nil, mcpWebhookOut{}, err
	}
	on := map[notify.Event]bool{}
	for _, e := range hook.OnEvents() {
		on[e] = true
	}
	if in.Events != nil {
		if on, err = mcpEvents(in.Events); err != nil {
			return nil, mcpWebhookOut{}, err
		}
	}
	st := notify.Setup{Preset: hook.Preset, URL: secret.New(strings.TrimSpace(in.URL)), Events: on, Lang: hook.Lang}
	if in.Preset != "" {
		st.Preset = notify.Preset(in.Preset)
	}
	if in.Lang != "" {
		st.Lang = i18n.Locale(in.Lang)
	}
	saved, p, err := s.changeWebhook(ctx, mcpActor(req), hook, st)
	if errors.Is(err, notify.ErrNoWebhook) {
		return nil, mcpWebhookOut{}, fmt.Errorf("no webhook has the id %d; foreman_settings lists them", in.WebhookID)
	}
	if err != nil {
		return nil, mcpWebhookOut{}, s.mcpFail("save the webhook", err)
	}
	if p.hasErrors() {
		return nil, mcpWebhookOut{}, mcpProblemError("the webhook was not changed", p.Problems())
	}
	return mcpText("Webhook %d was saved: %s, %s.", saved.ID, saved.Preset, saved.Host), mcpWebhookOut{Webhook: mcpWebhookOf(saved)}, nil
}

type mcpDeleteWebhookIn struct {
	mcpConfirmIn
	WebhookID int64 `json:"webhook_id" jsonschema:"the webhook's id from foreman_settings"`
}

type mcpDeleteWebhookOut struct {
	mcpPreview
	Webhook mcpWebhook `json:"webhook"`
}

// webhookDeleteWill is the delete page's sentences in English.
func webhookDeleteWill() []string {
	return []string{inEnglish(i18n.M("notify.delete.stops")), inEnglish(i18n.M("notify.delete.readd"))}
}

func (s *server) mcpDeleteWebhook(ctx context.Context, req *mcp.CallToolRequest, in mcpDeleteWebhookIn) (*mcp.CallToolResult, mcpDeleteWebhookOut, error) {
	hook, err := s.mcpWebhookNamed(ctx, in.WebhookID)
	if err != nil {
		return nil, mcpDeleteWebhookOut{}, err
	}
	out := mcpDeleteWebhookOut{Webhook: mcpWebhookOf(hook)}
	// The preview shows the webhook as it was; a change to its preset, host,
	// language, events or address state before the confirm call makes the
	// preview stale. A new address on the same host does not.
	seen := fmt.Sprintf("%d %s %s %s %v %v %v", hook.ID, hook.Preset, hook.Host, hook.Lang, hook.OnEvents(), hook.Unreadable, hook.Plain)
	preview := func(stale bool) (*mcp.CallToolResult, mcpDeleteWebhookOut, error) {
		p, err := s.mcpAsk(req, in, seen)
		if err != nil {
			return nil, mcpDeleteWebhookOut{}, err
		}
		p.Will = webhookDeleteWill()
		out.mcpPreview = p
		text := fmt.Sprintf("Nothing has changed yet. Call delete_webhook again with the same arguments and confirm_code to delete webhook %d (%s, %s).", hook.ID, hook.Preset, hook.Host)
		if stale {
			text = fmt.Sprintf("Webhook %d changed since the preview, so nothing was deleted. Here is a fresh preview and code.", hook.ID)
		}
		res := mcpText("%s", text)
		res.IsError = stale
		return res, out, nil
	}
	if in.ConfirmCode == "" {
		return preview(false)
	}
	was, err := s.mcpSpend(req, in)
	if err != nil {
		return nil, mcpDeleteWebhookOut{}, err
	}
	if was.moved(seen) {
		return preview(true)
	}
	if err := s.deleteWebhook(ctx, mcpActor(req), hook); err != nil {
		if errors.Is(err, notify.ErrNoWebhook) {
			return nil, mcpDeleteWebhookOut{}, fmt.Errorf("no webhook has the id %d; foreman_settings lists them", in.WebhookID)
		}
		return nil, mcpDeleteWebhookOut{}, s.mcpFail("delete the webhook", err)
	}
	out.Done = true
	return mcpText("Webhook %d (%s, %s) was deleted.", hook.ID, hook.Preset, hook.Host), out, nil
}

type mcpAccountIn struct {
	Username string `json:"username" jsonschema:"the factorio.com username; write-only"`
	Token    string `json:"token" jsonschema:"the factorio.com token; write-only"`
}

// mcpAccountOut says only where the account stands, never what it is.
type mcpAccountOut struct {
	Source string `json:"source"`
}

// mcpAccountRefusal words a page's refusal in English: the page's own
// problems or its error, never the values sent.
func mcpAccountRefusal(what string, p AccountPage) error {
	if ps := p.Problems(); len(ps) > 0 {
		return mcpProblemError(what, ps)
	}
	return fmt.Errorf("%s: %s", what, inEnglish(p.Error))
}

func (s *server) mcpSetAccount(ctx context.Context, req *mcp.CallToolRequest, in mcpAccountIn) (*mcp.CallToolResult, mcpAccountOut, error) {
	p, err := s.setAccount(ctx, mcpActor(req), in.Username, in.Token)
	if err != nil {
		return nil, mcpAccountOut{}, s.mcpFail("store the factorio.com account", err)
	}
	if p.hasErrors() {
		return nil, mcpAccountOut{}, mcpAccountRefusal("the factorio.com account was not stored", p)
	}
	return mcpText("The factorio.com account was stored."), mcpAccountOut{Source: accountSource(factorioaccount.Stored)}, nil
}

func (s *server) mcpRemoveAccount(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpAccountOut, error) {
	removed, p, err := s.removeAccountBy(ctx, mcpActor(req))
	if err != nil {
		return nil, mcpAccountOut{}, s.mcpFail("remove the factorio.com account", err)
	}
	if p.hasErrors() {
		return nil, mcpAccountOut{}, mcpAccountRefusal("the factorio.com account was not removed", p)
	}
	if !removed {
		// Nothing stored: the account is from the environment, or there is none.
		return mcpText("No account is stored here, so nothing was removed."), mcpAccountOut{Source: accountSource(p.Source)}, nil
	}
	return mcpText("The stored factorio.com account was removed."), mcpAccountOut{Source: accountSource(factorioaccount.None)}, nil
}

type mcpPublicModsIn struct {
	On bool `json:"on" jsonschema:"true to turn the public mods page on, false to turn it off"`
}

type mcpPublicModsOut struct {
	On bool `json:"on"`
}

func (s *server) mcpSetPublicMods(ctx context.Context, req *mcp.CallToolRequest, in mcpPublicModsIn) (*mcp.CallToolResult, mcpPublicModsOut, error) {
	v := PublicModsOff
	if in.On {
		v = PublicModsOn
	}
	if err := s.setPublicModsBy(ctx, mcpActor(req), v); err != nil {
		return nil, mcpPublicModsOut{}, s.mcpFail("save the public mods setting", err)
	}
	return mcpText("The public mods page is %s.", onOff(in.On)), mcpPublicModsOut(in), nil
}
