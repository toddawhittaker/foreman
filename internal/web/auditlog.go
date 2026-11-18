package web

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
)

// auditTabOn shows the Audit log tab (SPEC 12.9).
const auditTabOn = true

// auditPageSize is how many entries a page of the log shows (SPEC 12.9).
const auditPageSize = 50

// auditActionWords is the action of each audit entry in words. An action
// not in this table is shown as it was recorded.
var auditActionWords = map[string]i18n.Msg{
	game.AuditStart:         i18n.M("audit.action.start"),
	game.AuditStop:          i18n.M("audit.action.stop"),
	game.AuditRestart:       i18n.M("audit.action.restart"),
	game.AuditForceStop:     i18n.M("audit.action.force-stop"),
	game.AuditStopCancel:    i18n.M("audit.action.stop-cancel"),
	game.AuditImageDownload: i18n.M("audit.action.image-download"),
	game.AuditConsole:       i18n.M("audit.action.console"),
	game.AuditSwitch:        i18n.M("audit.action.switch"),
	game.AuditUpdate:        i18n.M("audit.action.game-update"),
	game.AuditRollback:      i18n.M("audit.action.rollback"),
	game.AuditMods:          i18n.M("audit.action.mods"),
	game.AuditSnapshot:      i18n.M("audit.action.snapshot"),
	game.AuditRestore:       i18n.M("audit.action.restore"),

	game.AuditStartRefused:   i18n.M("audit.action.start-refused"),
	game.AuditRestartRefused: i18n.M("audit.action.restart-refused"),
	game.AuditSwitchRefused:  i18n.M("audit.action.switch-refused"),
	game.AuditUpdateRefused:  i18n.M("audit.action.game-update-refused"),
	game.AuditModsRefused:    i18n.M("audit.action.mods-refused"),

	game.AuditSettingsSave:   i18n.M("audit.action.settings-save"),
	game.AuditPlayer:         i18n.M("audit.action.player"),
	game.AuditAutosaveTidy:   i18n.M("audit.action.autosave-tidy"),
	game.AuditChat:           i18n.M("audit.action.chat"),
	AuditScheduleSet:         i18n.M("audit.action.restart-schedule-set"),
	AuditWebhookAdd:          i18n.M("audit.action.webhook-add"),
	AuditWebhookChange:       i18n.M("audit.action.webhook-change"),
	AuditWebhookDelete:       i18n.M("audit.action.webhook-delete"),
	AuditClone:               i18n.M("audit.action.clone"),
	AuditWhitelistSwitch:     i18n.M("audit.action.whitelist-switch"),
	AuditGenerate:            i18n.M("audit.action.generate"),
	AuditTokenCreate:         i18n.M("audit.action.token-create"),
	AuditTokenRenew:          i18n.M("audit.action.token-renew"),
	AuditTokenRevoke:         i18n.M("audit.action.token-revoke"),
	AuditMCPSet:              i18n.M("audit.action.mcp-set"),
	AuditAccountSet:          i18n.M("audit.action.factorio-account-set"),
	AuditAccountRemove:       i18n.M("audit.action.factorio-account-remove"),
	AuditPublicModsSet:       i18n.M("audit.action.public-mods-set"),
	AuditSnapshotDelete:      i18n.M("audit.action.snapshot-delete"),
	AuditWorldDelete:         i18n.M("audit.action.world-delete"),
	"world-archive":          i18n.M("audit.action.world-archive"),
	"world-unarchive":        i18n.M("audit.action.world-unarchive"),
	"user-create":            i18n.M("audit.action.user-create"),
	"user-delete":            i18n.M("audit.action.user-delete"),
	"password-change":        i18n.M("audit.action.password-change"),
	"snapshot-retention-set": i18n.M("audit.action.snapshot-retention-set"),
	"stop-countdown-set":     i18n.M("audit.action.stop-countdown-set"),
	"audit-retention-set":    i18n.M("audit.action.audit-retention-set"),
	"save-upload-limit-set":  i18n.M("audit.action.save-upload-limit-set"),
	"mod-upload-limit-set":   i18n.M("audit.action.mod-upload-limit-set"),
}

// AuditRow is one entry of the log as the page shows it.
type AuditRow struct {
	ID   int64
	Time time.Time
	User string
	// Token is the name of the API token the action came through, or "".
	Token string
	// Action is the action as recorded; Word is its words, zero when unknown.
	Action string
	Word   i18n.Msg
	// WorldID is "" when the entry is not about one world.
	WorldID    string
	WorldName  string
	WorldKnown bool
	Detail     string
}

// WorldPath is the page of the entry's world.
func (r AuditRow) WorldPath() string { return worldPath(r.WorldID) }

// AuditOption is one choice of a filter menu.
type AuditOption struct {
	Value string
	// Word is the label in words; Label is used when Word is zero.
	Word     i18n.Msg
	Label    string
	Selected bool
}

// AuditLogPage is the audit log, one page of it (SPEC 12.9).
type AuditLogPage struct {
	Rows    []AuditRow
	Users   []AuditOption
	Actions []AuditOption
	Worlds  []AuditOption
	// Filtered is true when any filter is set.
	Filtered bool
	// Older is the address of the next page, "" on the last one.
	Older string
}

func (AuditLogPage) templateName() string { return "auditlog" }
func (AuditLogPage) navItem() string      { return PathSchedule }

// Path is where the filter form sends its query.
func (AuditLogPage) Path() string { return auditLogPath() }

func (s *server) auditRoutes() {
	s.mux.HandleFunc("GET "+auditLogPath(), s.requireUser(s.handleAuditLog))
}

// auditCursorText writes a cursor as "<unix seconds>.<id>".
func auditCursorText(e store.AuditEntry) string {
	return strconv.FormatInt(e.Time.Unix(), 10) + "." + strconv.FormatInt(e.ID, 10)
}

// parseAuditCursor reads auditCursorText. Anything else is no cursor, the
// first page.
func parseAuditCursor(v string) store.AuditCursor {
	at, id, ok := strings.Cut(v, ".")
	if !ok {
		return store.AuditCursor{}
	}
	sec, err1 := strconv.ParseInt(at, 10, 64)
	n, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil || n < 1 {
		return store.AuditCursor{}
	}
	return store.AuditCursor{Time: time.Unix(sec, 0).UTC(), ID: n}
}

// auditOptions makes the menu for a filter from the values the log holds.
// A selected value the log no longer holds is kept, so the menu shows the
// filter that is in force.
func auditOptions(values []string, selected string, label func(string) AuditOption) []AuditOption {
	if selected != "" && !slices.Contains(values, selected) {
		values = append(slices.Clone(values), selected)
	}
	out := make([]AuditOption, 0, len(values))
	for _, v := range values {
		o := label(v)
		o.Value, o.Selected = v, v == selected
		out = append(out, o)
	}
	return out
}

func (s *server) handleAuditLog(w http.ResponseWriter, r *http.Request, u store.User) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.AuditFilter{
		Username: q.Get("user"), Action: q.Get("action"), WorldID: q.Get("world"),
		Before: parseAuditCursor(q.Get("before")), Limit: auditPageSize + 1,
	}
	entries, err := s.store.ListAudit(ctx, f)
	if err != nil {
		s.fail(w, r, "list the audit log", err)
		return
	}
	values, err := s.store.AuditFilterValues(ctx)
	if err != nil {
		s.fail(w, r, "list the audit log's filter values", err)
		return
	}
	worlds, err := s.worlds.List(ctx)
	if err != nil {
		s.fail(w, r, "list the worlds for the audit log", err)
		return
	}
	names := make(map[string]string, len(worlds))
	for _, sum := range worlds {
		names[sum.ID] = sum.Name
	}

	p := AuditLogPage{Filtered: f.Username != "" || f.Action != "" || f.WorldID != ""}
	if len(entries) > auditPageSize {
		entries = entries[:auditPageSize]
		next := url.Values{}
		for _, k := range []string{"user", "action", "world"} {
			if v := q.Get(k); v != "" {
				next.Set(k, v)
			}
		}
		next.Set("before", auditCursorText(entries[len(entries)-1]))
		p.Older = auditLogPath() + "?" + next.Encode()
	}
	for _, e := range entries {
		row := AuditRow{
			ID: e.ID, Time: e.Time, User: e.Username, Token: e.TokenName, Action: e.Action,
			WorldID: e.WorldID, Detail: e.Detail,
		}
		row.Word = auditActionWords[e.Action]
		row.WorldName, row.WorldKnown = names[e.WorldID]
		p.Rows = append(p.Rows, row)
	}
	p.Users = auditOptions(values.Users, f.Username, func(v string) AuditOption { return AuditOption{Label: v} })
	p.Actions = auditOptions(values.Actions, f.Action, func(v string) AuditOption {
		return AuditOption{Word: auditActionWords[v], Label: v}
	})
	p.Worlds = auditOptions(values.Worlds, f.WorldID, func(v string) AuditOption {
		if name, ok := names[v]; ok {
			return AuditOption{Label: name}
		}
		return AuditOption{Word: i18n.M("audit.world.deleted", "world", v)}
	})
	s.page(w, r, http.StatusOK, u.Username, p)
}
