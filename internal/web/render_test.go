package web

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/world"
)

func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	return r
}

// A 60-character name with markup in it: it must be escaped and must wrap.
var longName = "<b>" + strings.Repeat("n", 53) + "</b>"

type pageCase struct {
	name   string
	user   string
	status int
	page   Page
}

func pageCases() []pageCase {
	return append([]pageCase{
		{"design", "todd", 200, testDesignPage()},
		{"worlds empty", "todd", 200, WorldsPage{}},
		{"worlds after delete", "todd", 200, WorldsPage{Deleted: longName}},
		{"worlds long user", longName, 200, WorldsPage{}},
		{"worlds listed", "todd", 200, WorldsPage{Worlds: []world.Summary{
			{ID: "base", Name: longName, GameVersion: "2.0.72", SpaceAge: true, Active: true, DiskBytes: 2_100_000_000,
				LastPlayed: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)},
			{ID: "future", Name: "Future", Newer: true},
			{ID: "broken", Name: "broken", Unreadable: true},
			{ID: "odd", Name: "Odd", SizeUnknown: true},
		}}},
		{"snapshots empty", "todd", 200, SnapshotsPage{WorldID: "base", WorldName: longName}},
		{"all snapshots", "todd", 200, allSnapshotsCase()},
		{"all snapshots empty", "todd", 200, AllSnapshotsPage{}},
		{"snapshot delete", "todd", 200, SnapshotDeletePage{WorldID: "base", WorldName: longName, Snapshot: snapshotsCase().Snapshots[0], Frees: 1000, FromAll: true}},
		{"snapshot delete rollback", "todd", 409, SnapshotDeletePage{WorldID: "base", WorldName: "Base", Snapshot: snapshotsCase().Snapshots[1], Rollback: "2.0.72", Error: i18n.M("worlds.snapshots.delete.nothing", "reason", msgBusy())}},
		{"snapshots bulk delete", "todd", 200, bulkDeleteCase()},
		{"snapshots bulk delete partly done", "todd", 409, bulkDeletePartlyDone()},
		{"snapshots bulk delete none ticked", "todd", 400, SnapshotBulkDeletePage{Error: i18n.M("worlds.snapshots.bulk.nothing_ticked")}},
		{"snapshots listed", "todd", 200, snapshotsCase()},
		{"snapshots label error", "todd", 400, SnapshotsPage{WorldID: "base", WorldName: "Base",
			Label: longName, LabelError: i18n.M("worlds.snapshots.label.too_long", "count", 100), Error: msgBusy()}},
		{"restore", "todd", 200, RestorePage{WorldID: "base", WorldName: longName, Snapshot: snapshotsCase().Snapshots[0]}},
		{"restore error", "todd", 409, RestorePage{WorldID: "base", WorldName: "Base",
			Snapshot: snapshotsCase().Snapshots[0], Error: msgBusy()}},
		{"restore failed with undo", "todd", 500, RestorePage{WorldID: "base", WorldName: "Base",
			Snapshot: snapshotsCase().Snapshots[0], Error: i18n.M("worlds.restore.failed"), Undo: "20261103T142500Z"}},
		{"saves empty", "todd", 200, SavesPage{WorldName: longName, WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves",
			LoadHref: "/worlds/base/saves", UploadLimit: MaxSaveUpload}},
		{"saves listed", "todd", 200, SavesPage{WorldName: longName, WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves",
			LoadHref: "/worlds/base/load-save", UploadLimit: MaxSaveUpload, LoadSave: "a.zip", OlderHref: "/worlds/base/saves?all=1",
			Uploaded: "a.zip", Chosen: true, Rows: []SaveRow{
				{Name: "a.zip", Href: "/worlds/base/saves/a.zip", Size: 2_100_000_000, Loads: true,
					Saved: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)},
				{Name: longName, Href: "/worlds/base/saves/x.zip", Size: 12, LoadValue: longName},
			}}},
		{"saves errors", "todd", 422, SavesPage{WorldName: "Base", WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves",
			LoadHref: "/worlds/base/load-save", UploadLimit: MaxSaveUpload, LoadSave: "gone.zip", LoadMissing: true,
			UploadError: i18n.M("worlds.upload.not_zip"), LoadError: i18n.M("worlds.saves.choose")}},
		{"saves newer", "todd", 200, SavesPage{WorldName: "Base", Newer: true, WorldHref: "/worlds/base", SavesHref: "/worlds/base/saves"}},
		{"world", "todd", 200, WorldPage{World: world.Summary{ID: "base", Name: longName, Active: true}, Name: longName}},
		{"world rename error", "todd", 400, WorldPage{World: world.Summary{ID: "base", Name: "Base"}, Name: "Other",
			NameError: i18n.M("worlds.error.name_taken")}},
		{"world newer", "todd", 200, WorldPage{World: world.Summary{ID: "future", Name: "Future", Newer: true}}},
		{"delete", "todd", 200, DeletePage{World: world.Summary{ID: "base", Name: longName, DiskBytes: 2_100_000_000}, FinalSnapshot: true}},
		{"delete name error", "todd", 400, DeletePage{World: world.Summary{ID: "base", Name: "Base"}, ConfirmName: "base",
			NameError: i18n.M("worlds.delete.wrong_name", "name", "Base")}},
		{"delete refused", "todd", 409, DeletePage{World: world.Summary{ID: "base", Name: "Base", Active: true},
			Error: i18n.M("worlds.error.active")}},
		{"deleted empty", "todd", 200, DeletedPage{}},
		{"deleted listed", "todd", 200, DeletedPage{More: true, Worlds: []world.DeletedWorld{
			{ID: "base", Name: longName, Snapshots: snapshotsCase().Snapshots},
			{ID: "other", Name: "Other", Snapshots: snapshotsCase().Snapshots[1:]},
		}}},
		{"restore deleted", "todd", 200, RestorePage{WorldID: "base", WorldName: longName, Snapshot: snapshotsCase().Snapshots[0], Deleted: true}},
		{"import empty", "todd", 200, ImportPage{InboxDir: "/data/import", Folder: ImportForm{IDPrefix: "folder"}, Upload: ImportForm{IDPrefix: "upload"}}},
		{"import errors", "todd", 400, ImportPage{InboxDir: "/data/import",
			Inbox:  []world.InboxFolder{{Name: longName, Saves: 1, Bytes: 10}, {Name: "b", Saves: 2, HasMods: true}},
			Folder: ImportForm{IDPrefix: "folder", Source: "b", SourceError: msgNoFolder(), NameError: msgNameEmpty(), VersionError: msgBadGameVersion()},
			Upload: ImportForm{IDPrefix: "upload"}}},
		{"import upload errors", "todd", 400, ImportPage{InboxDir: "/data/import", Folder: ImportForm{IDPrefix: "folder"},
			Upload: ImportForm{IDPrefix: "upload", SourceError: i18n.M("worlds.upload.not_zip"), NameError: msgNameEmpty(), VersionError: msgBadGameVersion()}}},
		{"generate empty", "todd", 200, GeneratePage{Form: generateCase(nil)}},
		{"generate preview", "todd", 200, GeneratePage{CanPreview: true, Form: generateCase(nil), Preview: &PreviewView{Seed: "1234", Pictures: []PreviewPicture{
			{Planet: "nauvis", Name: planetMsg("nauvis"), Path: "/generate/preview/tok/nauvis"},
			{Planet: "vulcanus", Name: planetMsg("vulcanus"), Path: "/generate/preview/tok/vulcanus"}}}}},
		{"generate preview failed", "todd", 200, GeneratePage{CanPreview: true, Form: generateCase(nil),
			Preview: &PreviewView{Error: i18n.M("worlds.generate.preview.failed")}}},
		{"generate errors", "todd", 400, GeneratePage{Form: generateCase(func(f *GenerateForm) {
			f.Name, f.Mode, f.Reset = longName, modeExchange, true
			f.NameError, f.VersionError, f.SpaceAgeError = msgNameEmpty(), msgBadGameVersion(), msgSpaceAgeOld()
			f.PresetError = i18n.M("worlds.generate.error.preset")
			f.ExchangeError, f.FormError = i18n.M("worlds.generate.error.exchange_empty"), i18n.M("worlds.generate.error.settings")
		})}},
		{"generate settings errors and warnings", "todd", 400, GeneratePage{Form: generateCase(func(f *GenerateForm) {
			f.Preset, f.Modified, f.Tab = "death-world", true, tabEnemy
			f.MapGen.Peaceful.Checked, f.MapGen.Peaceful.Blocked = true, true
			f.MapGen.Seed.Error = i18n.M("mapgen.err.seed", "max", "4294967295")
			f.SettingsProblems = []Problem{{FieldID: mgSeedID, Message: f.MapGen.Seed.Error}}
			f.Reasons = []AchReason{{Key: "mapgen.achievement.not_in_peaceful_mode", Msg: achievementMsgs["mapgen.achievement.not_in_peaceful_mode"], Active: true}}
		})}},
		{"generate untested layout", "todd", 200, GeneratePage{Form: generateCase(func(f *GenerateForm) {
			f.Version, f.Line, f.MapKey, f.Presets = "2.1.15", "2.1", "2.1|1", mapgen.Presets("2.1")
			f.fill("2.1.15", nil)
		})}},
		{"clone with save", "todd", 200, ClonePage{World: world.Summary{ID: "base", Name: longName, GameVersion: "2.0.77", SpaceAge: true},
			Name: longName + " (copy)", Choice: cloneKeep, SaveName: "autosave_3.zip", SaveTime: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC), RunningNote: true}},
		{"clone fresh only errors", "todd", 400, ClonePage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "1.1.110"},
			Name: "", Choice: cloneFresh, FreshOnlyWhy: i18n.M("worlds.clone.no_save"), NameError: msgNameEmpty(),
			Error: msgBusy(), ErrorBusy: true}},
		{"chat", "todd", 200, ChatPage{}},
		{"notifications", "todd", 200, NotificationsPage{}},
		{"notifications no key", "todd", 200, NotificationsPage{NoKey: true}},
		{"notifications list", "todd", 200, NotificationsPage{Done: i18n.M("notify.done.added", "preset", i18n.M("notify.preset.discord"), "host", "discord.com"), Hooks: []WebhookView{
			{ID: 1, Preset: i18n.M("notify.preset.discord"), Host: "discord.com", LangTag: "en", LangName: "English", AllOn: true},
			{ID: 2, Preset: i18n.M("notify.preset.ntfy"), Host: "ntfy.lan", LangTag: "de", LangName: "Deutsch", Events: "Crash, low disk", Plain: true},
			{ID: 3, Preset: i18n.M("notify.preset.generic"), Host: "hooks.example.org", LangTag: "fr", LangName: "Français", AllOn: true, Unreadable: true},
		}}},
		{"notification add", "todd", 200, formPage(notify.Discord, allEventsOn(), "en")},
		{"notification add refused", "todd", 400, func() WebhookFormPage {
			p := formPage("", map[notify.Event]bool{}, "xx")
			p.PresetError, p.URLError, p.EventsError, p.LangError = i18n.M("notify.preset.invalid"), i18n.M("notify.url.scheme"), i18n.M("notify.events.none"), i18n.M("notify.lang.invalid")
			return p
		}()},
		{"notification edit plain", "todd", 200, func() WebhookFormPage {
			p := formPage(notify.Ntfy, allEventsOn(), "de")
			p.Edit, p.ID, p.Kept = true, 2, WebhookView{ID: 2, Preset: i18n.M("notify.preset.ntfy"), Host: "ntfy.lan", Plain: true}
			return p
		}()},
		{"notification edit unreadable", "todd", 400, func() WebhookFormPage {
			p := formPage(notify.Generic, allEventsOn(), "fr")
			p.Edit, p.ID, p.Kept = true, 3, WebhookView{ID: 3, Preset: i18n.M("notify.preset.generic"), Host: longName, Unreadable: true}
			p.URLError = i18n.M("notify.url.missing")
			return p
		}()},
		{"notification delete", "todd", 200, WebhookDeletePage{Hook: WebhookView{ID: 1, Preset: i18n.M("notify.preset.discord"), Host: "discord.com"}}},
		{"schedule", "todd", 200, SchedulePage{}},
		{"account none", "todd", 200, AccountPage{Source: factorioaccount.None, CanStore: true}},
		{"account stored", "todd", 200, AccountPage{Source: factorioaccount.Stored, CanStore: true, UpdatedAt: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC),
			UpdatedBy: longName, Return: "/worlds/base/mods", Done: i18n.M("account.done.saved")}},
		{"account env", "todd", 200, AccountPage{Source: factorioaccount.Env}},
		{"account unreadable", "todd", 200, AccountPage{Source: factorioaccount.Unreadable, CanStore: true}},
		{"account no key", "todd", 200, AccountPage{Source: factorioaccount.None}},
		{"account unreadable without a key", "todd", 200, AccountPage{Source: factorioaccount.Unreadable}},
		{"account errors", "todd", 400, AccountPage{Source: factorioaccount.None, CanStore: true,
			UsernameError: i18n.M("account.username.missing"), TokenError: i18n.M("account.token.missing")}},
		{"account refused without a key", "todd", 409, AccountPage{Source: factorioaccount.None, Return: "/worlds/base/mods",
			Error: i18n.M("account.error.nokey")}},
		{"signed out", "", 200, SignedOutPage{}},
		{"sign in empty", "", 200, SignInPage{}},
		{"sign in one error", "", 401, SignInPage{Username: "todd",
			PasswordError: i18n.M("accounts.signin.bad")}},
		{"sign in two errors", "", 400, SignInPage{UsernameError: i18n.M("accounts.signin.username.missing"), PasswordError: i18n.M("accounts.signin.password.missing")}},
		{"sign in rate limited", "", 429, SignInPage{Username: longName, RateLimited: true, RetryAfter: 14*time.Minute + time.Second}},
		{"setup empty", "", 200, SetupPage{}},
		{"setup with hint", "", 200, SetupPage{MinPasswordLength: 12}},
		{"setup all errors", "", 400, SetupPage{Username: longName, MinPasswordLength: 12, //nolint:gosec // error wording, not a credential
			UsernameError: i18n.M("accounts.username.missing"), PasswordError: i18n.M("accounts.setup.password.short", "count", 12),
			ConfirmError: i18n.M("accounts.setup.confirm.mismatch")}},
		{"error 403", "todd", 403, ErrorPage{Status: 403}},
		{"error 404", "", 404, ErrorPage{Status: 404}},
		{"error 500", "", 500, ErrorPage{Status: 500}},
		{"error other with detail", "", 400, ErrorPage{Status: 400, Detail: i18n.M("worlds.upload.bad_name")}},
		{"server no world", "todd", 200, ServerPage{}},
		{"server running", "todd", 200, serverCase(game.StateRunning, func(p *ServerPage) {
			p.Status.Uptime, p.Status.StartedAt = 3*time.Hour+12*time.Minute, time.Date(2026, 11, 3, 11, 0, 0, 0, time.UTC)
			p.Status.Save, p.Status.HaveUsage, p.Status.CPUPercent, p.Status.MemoryBytes = longName, true, 12.5, 1_500_000_000
			p.PlayersKnown = true
			p.Players = []game.Player{{Name: longName, Joined: time.Date(2026, 11, 3, 13, 0, 0, 0, time.UTC), Duration: 85 * time.Minute}, {Name: "bob"}}
		})},
		{"server starting", "todd", 200, serverCase(game.StateStarting, func(p *ServerPage) {
			p.Status.Operation = &game.Operation{Action: game.AuditStart, Steps: []game.Step{
				{Name: game.StepCheck, State: game.StepDone}, {Name: game.StepSnapshot, State: game.StepSkipped},
				{Name: game.StepLoad, State: game.StepProgress}, {Name: game.StepRCON, State: game.StepWaiting}}}
		})},
		{"server refused by the check", "todd", 200, serverCase(game.StateStopped, func(p *ServerPage) {
			err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{
				{Kind: world.ProblemModMissing, Text: longName, StartAnyway: true}}}}
			p.Status.Operation = &game.Operation{Action: game.AuditStart, Done: true, Error: err.Error(), Steps: []game.Step{
				{Name: game.StepCheck, State: game.StepFailed}, {Name: game.StepLoad, State: game.StepWaiting}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: err}, true
		})},
		{"server restart refused by the check", "todd", 200, serverCase(game.StateRunning, func(p *ServerPage) {
			err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{
				{Kind: world.ProblemModMissing, Text: longName, Mods: []string{longName}, StartAnyway: true}}}}
			p.Status.Operation = &game.Operation{Action: game.AuditRestart, Done: true, Error: err.Error(), Steps: []game.Step{
				{Name: game.StepCheck, State: game.StepFailed}, {Name: game.StepLoad, State: game.StepWaiting}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditRestart, Err: err}, true
		})},
		{"server switch refused by the check", "todd", 200, serverCase(game.StateRunning, func(p *ServerPage) {
			err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{
				{Kind: world.ProblemModDisabled, Text: longName, StartAnyway: true}}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditSwitch, Err: err, Target: "other"}, true
			p.WorldNames = map[string]string{"other": longName}
		})},
		{"server plain refusal", "todd", 200, serverCase(game.StateStopped, func(p *ServerPage) {
			err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{
				{Kind: world.ProblemSaveNewer, Text: longName}}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: err}, true
		})},
		{"server image missing", "todd", 200, serverCase(game.StateStopped, func(p *ServerPage) {
			p.Status.Download = &game.Operation{Action: game.AuditImageDownload, Done: true, Error: "no route to registry",
				Steps: []game.Step{{Name: game.StepDownload, State: game.StepFailed}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: game.ErrImageMissing}, true
		})},
		{"server crash-looping", "todd", 200, serverCase(game.StateCrashLooping, func(p *ServerPage) {
			p.Status.CrashReason = ". Factorio said: " + longName
		})},
		{"server crashed", "todd", 200, serverCase(game.StateCrashed, func(p *ServerPage) {
			p.Status.LastCrash = time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)
		})},
		{"server unknown", "todd", 200, serverCase(game.StateUnknown, func(p *ServerPage) {
			p.Status.Reason = "Foreman cannot reach Docker: " + longName
		})},
		{"server restored", "todd", 200, serverCase(game.StateRunning, func(p *ServerPage) {
			p.Result, p.HaveResult = actionResult{Action: game.ActionRestore, Restored: "a", Undo: "b"}, true
		})},
		{"stop", "todd", 200, StopPage{World: world.Summary{ID: "base", Name: longName}, CountdownForm: defaultCountdown()}},
		{"kick confirm", "todd", 200, PlayerActionPage{World: world.Summary{ID: "base", Name: longName}, Action: game.PlayerKick, Name: "Alice", Online: true}},
		{"kick player gone", "todd", 200, PlayerActionPage{World: world.Summary{ID: "base", Name: "Base"}, Action: game.PlayerKick, Name: "Alice", Gone: true}},
		{"ban reason error", "todd", 400, PlayerActionPage{World: world.Summary{ID: "base", Name: "Base"}, Action: game.PlayerBan, Name: "Alice",
			Return: "/worlds/base/settings", Reason: "x", ReasonError: i18n.M("players.error.reason", "max", 200)}},
		{"ban not running", "todd", 200, PlayerActionPage{World: world.Summary{ID: "base", Name: "Base"}, Action: game.PlayerBan, Name: "Alice",
			Error: i18n.M("players.error.not_running", "world", "Base")}},
		{"restart countdown error", "todd", 400, StopPage{World: world.Summary{ID: "base", Name: "Base"}, Restart: true,
			CountdownForm: CountdownForm{Countdown: "-1", CountdownError: i18n.M("server.countdown.invalid", "max", 600)}}},
		{"switch running", "todd", 200, SwitchPage{From: world.Summary{ID: "base", Name: longName}, To: world.Summary{ID: "other", Name: longName},
			Plan: game.SwitchPlan{Running: true}, CountdownForm: defaultCountdown()}},
		{"switch with a download and problems", "todd", 200, SwitchPage{From: world.Summary{ID: "base", Name: "Base"}, To: world.Summary{ID: "other", Name: "Other"},
			Plan: game.SwitchPlan{Running: true, Download: true, Image: "factoriotools/factorio:2.0.76",
				Check: world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemModMissing, Text: "Mod a is missing.", StartAnyway: true}}}},
			ImageSize: 345678901, Lead: i18n.M("server.switch.problems_lead", "world", "Other"), CheckProblems: []i18n.Msg{i18n.M("server.problem.other", "text", "Mod a is missing.")},
			CountdownForm: defaultCountdown()}},
		{"switch stopped", "todd", 200, SwitchPage{From: world.Summary{ID: "base", Name: "Base"}, To: world.Summary{ID: "other", Name: "Other"}}},
		{"switch countdown error", "todd", 400, SwitchPage{From: world.Summary{ID: "base", Name: "Base"}, To: world.Summary{ID: "other", Name: "Other"},
			Plan: game.SwitchPlan{Running: true}, CountdownForm: CountdownForm{Countdown: "x", CountdownError: i18n.M("server.countdown.invalid", "max", 600)}}},
		{"switch refused", "todd", 409, SwitchPage{From: world.Summary{ID: "base", Name: "Base"}, To: world.Summary{ID: "other", Name: "Other"},
			Refusal: i18n.M("server.error.switch_same")}},
		{"force stop", "todd", 200, ForceStopPage{World: world.Summary{ID: "base", Name: longName,
			LastPlayed: time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)}}},
		{"restore running", "todd", 200, RestorePage{WorldID: "base", WorldName: longName, Snapshot: snapshotsCase().Snapshots[0],
			Running: true, CountdownForm: defaultCountdown()}},
		{"restore countdown error", "todd", 400, RestorePage{WorldID: "base", WorldName: "Base", Snapshot: snapshotsCase().Snapshots[0],
			Running: true, CountdownForm: CountdownForm{Countdown: "x", CountdownError: i18n.M("server.countdown.invalid", "max", 600)}}},
		{"console no world", "todd", 200, ConsolePage{}},
		{"console empty", "todd", 200, ConsolePage{World: world.Summary{ID: "base", Name: longName}, HasWorld: true}},
		{"console log and history", "todd", 200, ConsolePage{World: world.Summary{ID: "base", Name: "Base"}, HasWorld: true,
			Lines: []string{"2026-11-03 10:00:00 [JOIN] alice joined the game", longName},
			History: []consoleEntry{{Command: "/players online", Reply: "Online players (1):\n  alice (online)\n"},
				{Command: longName, Reply: longName, Cut: true},
				{Command: "/server-save", Last: true}}}},
		{"console command refused", "todd", 400, ConsolePage{World: world.Summary{ID: "base", Name: "Base"}, HasWorld: true,
			Command: "/c game.print(1)", CommandError: i18n.M("server.console.error.long", "max", 1000)}},
		{"console not running", "todd", 409, ConsolePage{World: world.Summary{ID: "base", Name: "Base"}, HasWorld: true,
			Failure: i18n.M("server.console.not_running")}},
		{"world update offers", "todd", 200, WorldPage{World: world.Summary{ID: "base", Name: longName, GameVersion: "2.1.24", Active: true},
			Name: longName, ReleasesPath: "/worlds/base/releases",
			Rollback: &RollbackOffer{Snapshot: snapshotsCase().Snapshots[0], Version: "2.0.77", Path: "/worlds/base/rollback"}}},
		{"world update not active", "todd", 200, WorldPage{World: world.Summary{ID: "other", Name: "Other", GameVersion: "2.0.77"},
			Name: "Other", ReleasesPath: "/worlds/other/releases"}},
		{"versions", "todd", 200, VersionPicker{}},
		{"update stopped", "todd", 200, UpdatePage{World: world.Summary{ID: "base", Name: longName, GameVersion: "2.0.77", Active: true}}},
		{"update running", "todd", 200, UpdatePage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.77", Active: true},
			Target: "2.1.24", Running: true, CountdownForm: defaultCountdown()}},
		{"update errors", "todd", 400, UpdatePage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.77", Active: true},
			Target: "2.0.70", VersionError: i18n.M("server.error.version_not_newer"), Running: true,
			CountdownForm: CountdownForm{Countdown: "x", CountdownError: i18n.M("server.countdown.invalid", "max", 600)}}},
		{"update refused", "todd", 409, UpdatePage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.77", Active: true},
			Target: "2.1.24", Refusal: i18n.M("server.error.version_unsupported")}},
		{"rollback stopped", "todd", 200, RollbackPage{World: world.Summary{ID: "base", Name: longName, GameVersion: "2.1.24"},
			Snapshot: snapshotsCase().Snapshots[0], Version: "2.0.77"}},
		{"rollback running", "todd", 200, RollbackPage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.1.24"},
			Snapshot: snapshotsCase().Snapshots[0], Version: "2.0.77", Running: true, CountdownForm: defaultCountdown(),
			Error: i18n.M("server.error.image_missing")}},
		{"settings form", "todd", 200, settingsFormCase(false)},
		{"settings form errors", "todd", 400, settingsFormCase(true)},
		{"settings raw", "todd", 200, settingsRawCase(false)},
		{"settings raw errors", "todd", 400, settingsRawCase(true)},
		{"settings preview", "todd", 200, settingsPreviewCase()},
		{"settings nothing changed", "todd", 200, SettingsPreviewPage{WorldID: "base", WorldName: longName}},
		{"settings promoted with undo", "todd", 200, settingsPlayerCase(game.PlayerPromote, false)},
		{"settings demoted with undo", "todd", 200, settingsPlayerCase(game.PlayerDemote, false)},
		{"settings banned with unban", "todd", 200, settingsPlayerCase(game.PlayerBan, false)},
		{"settings unbanned with undo", "todd", 200, settingsPlayerCase(game.PlayerUnban, false)},
		{"settings whitelist added", "todd", 200, settingsPlayerCase(game.PlayerWhitelistAdd, false)},
		{"settings whitelist removed", "todd", 200, settingsPlayerCase(game.PlayerWhitelistRemove, false)},
		{"settings kicked", "todd", 200, settingsPlayerCase(game.PlayerKick, false)},
		{"settings unban refused", "todd", 200, settingsPlayerCase(game.PlayerUnban, true)},
	}, append(modPageCases(), modSettingsPageCases()...)...)
}

// The version fragments render each of their states (SPEC 10.2, 10.7).
func TestVersionFragments(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		name string
		page Page
		frag string
		want []string
	}{
		{"list", VersionPicker{Versions: []string{"2.1.24", "2.0.77", "2.0.76"}, Stable: "2.0.77", Experimental: "2.1.24"},
			"version-list", []string{`<option value="2.1.24">2.1.24 (experimental)</option>`, `<option value="2.0.77">2.0.77 (stable)</option>`, `<option value="2.0.76">2.0.76</option>`}},
		{"list unreachable", VersionPicker{Unreachable: true}, "version-list", []string{`<option value="" disabled>Docker Hub cannot be reached; type a version below</option>`}},
		{"notice newer", ReleaseNotice{Current: "2.0.76", Stable: "2.0.77", Experimental: "2.1.24", UpdatePath: "/worlds/base/update",
			ChangelogURL: "https://wiki.factorio.com/Version_history/2.1.0"}, "release-notice",
			[]string{"stable <code>2.0.77</code>", "experimental <code>2.1.24</code>", `href="https://wiki.factorio.com/Version_history/2.1.0"`, `href="/worlds/base/update"`}},
		{"notice no image", ReleaseNotice{Current: "2.0.77", Experimental: "2.1.25"}, "release-notice",
			[]string{"No image yet"}},
		{"notice unreachable", ReleaseNotice{Current: "2.0.77", Unreachable: true}, "release-notice",
			[]string{"Docker Hub cannot be reached"}},
		{"notice newest", ReleaseNotice{Current: "2.1.24"}, "release-notice", []string{"newest Factorio release"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b strings.Builder
			if err := r.Fragment(&b, i18n.English, c.page, c.frag); err != nil {
				t.Fatal(err)
			}
			if c.want == nil && strings.TrimSpace(b.String()) != "" {
				t.Errorf("want empty, got %q", b.String())
			}
			for _, w := range c.want {
				if !strings.Contains(b.String(), w) {
					t.Errorf("missing %q in %q", w, b.String())
				}
			}
		})
	}
}

// generateCase is the Generate form as first offered, with edit applied.
func generateCase(edit func(*GenerateForm)) GenerateForm {
	f := GenerateForm{Version: "2.0.77", DefaultVersion: "2.0.77", SpaceAge: true, Download: true, CanDownload: true,
		Mode: modeSettings, Preset: "default", Presets: mapgen.Presets("2.0"), Line: "2.0", MapKey: "2.0|1", Tab: tabResources}
	f.fill("2.0.77", &MapGenText{Value: "42"})
	if edit != nil {
		edit(&f)
	}
	return f
}

// serverCase is the server page for an active world in state s.
func serverCase(s game.State, edit func(*ServerPage)) ServerPage {
	p := ServerPage{World: world.Summary{ID: "base", Name: longName, GameVersion: "2.1.24", Active: true}, HasWorld: true,
		Status: game.Status{World: "base", State: s}}
	edit(&p)
	return p
}

func render(t *testing.T, r *Renderer, c pageCase) (*httptest.ResponseRecorder, string) {
	t.Helper()
	return renderIn(t, r, c, i18n.English)
}

// renderIn renders c for a GET request in language loc.
func renderIn(t *testing.T, r *Renderer, c pageCase, loc i18n.Locale) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := r.Render(rec, requestIn(loc), c.status, c.user, c.page); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return rec, rec.Body.String()
}

var (
	reInput       = regexp.MustCompile(`<input\b[^>]*>`)
	reID          = regexp.MustCompile(`\bid="([^"]*)"`)
	rePlaceholder = regexp.MustCompile(`\bplaceholder="([^"]*)"`)
	reDescribed   = regexp.MustCompile(`aria-describedby="([^"]*)"`)
	reHrefAnchor  = regexp.MustCompile(`href="#([^"]*)"`)
	reTitle       = regexp.MustCompile(`<title>([^<]+)</title>`)
)

// Every page renders and meets the document rules every screen shares
// (SPEC 17.1, DESIGN 2 and 7.2), in every language (SPEC 22.1).
func TestEveryPageRenders(t *testing.T) {
	r := newTestRenderer(t)
	for _, l := range i18n.Languages {
		for _, c := range pageCases() {
			t.Run(string(l.Tag)+"/"+c.name, func(t *testing.T) { checkPage(t, r, c, l.Tag) })
		}
	}
}

func checkPage(t *testing.T, r *Renderer, c pageCase, loc i18n.Locale) {
	t.Helper()
	rec, body := renderIn(t, r, c, loc)

	if rec.Code != c.status {
		t.Errorf("status %d, want %d", rec.Code, c.status)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q", got)
	}
	if !strings.HasPrefix(body, "<!doctype html>") {
		t.Errorf("body does not start with a doctype: %.40q", body)
	}
	if !strings.Contains(body, `<html lang="`+loc.String()+`">`) {
		t.Error("no lang on html")
	}
	if m := reTitle.FindAllStringSubmatch(body, -1); len(m) != 1 || strings.TrimSpace(m[0][1]) == "" {
		t.Errorf("want one non-empty title, got %v", m)
	}
	if n := strings.Count(body, "<h1"); n != 1 {
		t.Errorf("%d h1 elements, want 1", n)
	}
	if strings.Contains(body, "<b>") {
		t.Error("user text was not escaped")
	}

	ids := map[string]int{}
	for _, m := range reID.FindAllStringSubmatch(body, -1) {
		ids[m[1]]++
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("id %q used %d times", id, n)
		}
	}
	for _, in := range reInput.FindAllString(body, -1) {
		// Radios and checkboxes sit inside their label (DESIGN 7.2); a
		// hidden field is never shown, so has none.
		if strings.Contains(in, `type="radio"`) || strings.Contains(in, `type="checkbox"`) || strings.Contains(in, `type="hidden"`) {
			continue
		}
		m := reID.FindStringSubmatch(in)
		if m == nil {
			t.Errorf("input without id: %s", in)
			continue
		}
		if !regexp.MustCompile(`<label for="` + regexp.QuoteMeta(m[1]) + `"[ >]`).MatchString(body) {
			t.Errorf("input %q has no label", m[1])
		}
		// A placeholder may give an example, never the label (DESIGN 7.2):
		// the label above is required, and the example must not repeat it.
		if p := rePlaceholder.FindStringSubmatch(in); p != nil && strings.Contains(body, `">`+p[1]+`</label>`) {
			t.Errorf("input %q's placeholder repeats its label", m[1])
		}
	}
	for _, m := range reDescribed.FindAllStringSubmatch(body, -1) {
		for _, id := range strings.Fields(m[1]) {
			if ids[id] == 0 {
				t.Errorf("aria-describedby names missing id %q", id)
			}
		}
	}
	for _, m := range reHrefAnchor.FindAllStringSubmatch(body, -1) {
		if ids[m[1]] == 0 {
			t.Errorf("link to missing id #%s", m[1])
		}
	}
	if strings.Contains(body, "autofocus") && strings.Count(body, "autofocus") != 1 {
		t.Error("more than one autofocus")
	}

	// Sign-out is a POST form shown only when someone is signed in (DESIGN 6).
	signOut := `<form method="post" action="` + PathSignOut + `"`
	if got := strings.Contains(body, signOut); got != (c.user != "") {
		t.Errorf("sign-out form shown=%v with user %q", got, c.user)
	}
}

// Each template file has a page type, so the test above covers every one.
func TestEveryTemplateHasAPage(t *testing.T) {
	r := newTestRenderer(t)
	files, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		if name == "layout" || isPartial(name) {
			continue
		}
		if _, ok := r.pages[i18n.English][name]; !ok {
			t.Errorf("template %s has no page type", f)
		}
	}
	covered := map[string]bool{}
	for _, c := range pageCases() {
		covered[c.page.templateName()] = true
	}
	for name := range r.pages[i18n.English] {
		if !covered[name] {
			t.Errorf("page %s has no render case", name)
		}
	}
}

func TestErrorSummary(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		page    Page
		heading string
		links   []string
	}{
		{SignInPage{PasswordError: i18n.M("accounts.username.missing")}, "Fix this problem to continue", []string{"password"}},
		{SignInPage{UsernameError: i18n.M("accounts.username.missing"), PasswordError: i18n.M("accounts.setup.confirm.mismatch")}, "Fix 2 problems to continue", []string{"username", "password"}},
		{SetupPage{UsernameError: i18n.M("accounts.username.missing"), PasswordError: i18n.M("accounts.setup.confirm.mismatch"), ConfirmError: i18n.M("accounts.setup.confirm.mismatch")}, "Fix 3 problems to continue", []string{"username", "password", "confirm"}},
	}
	for _, c := range cases {
		_, body := render(t, r, pageCase{page: c.page, status: 400})
		if !strings.Contains(body, c.heading) {
			t.Errorf("%T: no heading %q", c.page, c.heading)
		}
		var got []string
		for _, m := range reHrefAnchor.FindAllStringSubmatch(body, -1) {
			if m[1] != "main" {
				got = append(got, m[1])
			}
		}
		if !slices.Equal(got, c.links) {
			t.Errorf("%T: summary links %v, want %v", c.page, got, c.links)
		}
		if !strings.Contains(body, `id="error-summary" tabindex="-1" autofocus`) {
			t.Errorf("%T: summary does not take focus", c.page)
		}
	}

	_, body := render(t, r, pageCase{page: SignInPage{}, status: 200})
	if strings.Contains(body, "error-summary") {
		t.Error("summary shown with no errors")
	}
}

// A form with errors says so first in the title, and the summary is named
// by its own content, not by aria-labelledby (WCAG 3.3.1).
func TestErrorTitleAndSummaryName(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		page  Page
		title string
	}{
		{SignInPage{}, "Sign in – Foreman"},
		{SignInPage{PasswordError: i18n.M("accounts.username.missing")}, "Error: Sign in – Foreman"},
		{SignInPage{RateLimited: true}, "Error: Sign in – Foreman"},
		{SignInPage{Busy: true}, "Error: Sign in – Foreman"},
		{SetupPage{}, "Create the first admin account – Foreman"},
		{SetupPage{ConfirmError: i18n.M("accounts.username.missing")}, "Error: Create the first admin account – Foreman"},
		{ErrorPage{Status: 404}, "Page not found – Foreman"},
	}
	for _, c := range cases {
		_, body := render(t, r, pageCase{page: c.page, status: 400})
		m := reTitle.FindStringSubmatch(body)
		if m == nil || m[1] != c.title {
			t.Errorf("%T %+v: title %v, want %q", c.page, c.page, m, c.title)
		}
		if strings.Contains(body, "aria-labelledby") {
			t.Errorf("%T: aria-labelledby on the page", c.page)
		}
	}
}

// Pages must not be framed by another site (clickjacking on the sign-in form).
func TestPagesRefuseFraming(t *testing.T) {
	r := newTestRenderer(t)
	for _, c := range pageCases() {
		rec, _ := render(t, r, c)
		if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
			t.Errorf("%s: Content-Security-Policy %q", c.name, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options %q", c.name, got)
		}
	}
}

func TestSignInRateLimit(t *testing.T) {
	cases := []struct {
		after time.Duration
		want  string
	}{
		{0, "Too many failed sign-ins. Wait a few minutes, then try again."},
		{time.Second, "Too many failed sign-ins. Wait 1 minute, then try again."},
		{time.Minute, "Too many failed sign-ins. Wait 1 minute, then try again."},
		{14*time.Minute + time.Second, "Too many failed sign-ins. Wait 15 minutes, then try again."},
	}
	for _, c := range cases {
		if got := i18n.English.Format((SignInPage{RateLimited: true, RetryAfter: c.after}).RateLimitMessage()); got != c.want {
			t.Errorf("RateLimitMessage(%v) = %q, want %q", c.after, got, c.want)
		}
	}

	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 429, page: SignInPage{RateLimited: true, PasswordError: i18n.M("accounts.username.missing")}})
	if !strings.Contains(body, "Too many failed sign-ins. Wait a few minutes, then try again.") {
		t.Error("no rate-limit message")
	}
	if strings.Contains(body, "Fix this problem") {
		t.Error("rate limit should replace the field error summary")
	}
}

// A busy server is a form-level message: no field is marked invalid, and
// the message takes focus like the rate-limit one.
func TestSignInBusyIsFormLevel(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 503, page: SignInPage{Busy: true}})
	if !strings.Contains(body, "Foreman is busy. Wait a moment and try again.") {
		t.Error("no busy message")
	}
	if strings.Contains(body, "aria-invalid") {
		t.Error("a busy page marks a field invalid")
	}
	if !strings.Contains(body, `id="error-summary" tabindex="-1" autofocus`) {
		t.Error("busy message does not take focus")
	}
}

func TestSignInNeverEchoesPassword(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 401, page: SignInPage{Username: "todd", PasswordError: i18n.M("accounts.username.missing")}})
	for _, in := range reInput.FindAllString(body, -1) {
		if strings.Contains(in, `type="password"`) && strings.Contains(in, "value=") {
			t.Errorf("password input carries a value: %s", in)
		}
	}
	if !strings.Contains(body, `value="todd"`) {
		t.Error("username not refilled")
	}
}

func TestErrorPageCopy(t *testing.T) {
	for _, s := range []int{403, 404, 500, 418} {
		p := ErrorPage{Status: s}
		if p.Heading().IsZero() || p.Explanation().IsZero() {
			t.Errorf("status %d has no copy", s)
		}
	}
	if got := i18n.English.Format((ErrorPage{Status: 403, Detail: i18n.M("worlds.upload.bad_name")}).Explanation()); got != i18n.English.T("worlds.upload.bad_name") {
		t.Errorf("Detail not used: %q", got)
	}
}

// The vendored htmx 2.0.11 and its SSE extension (npm htmx-ext-sse 2.2.4,
// dist/sse.min.js, tarball integrity sha512-LJmxVhyk...EI8O2wXXn4ZZtBdpEMqmEQ==)
// are exactly the pinned releases (STACK 3, SPEC 3.7).
func TestVendoredChecksums(t *testing.T) {
	sums, err := staticFS.ReadFile("static/vendor/SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		want, name, ok := strings.Cut(sc.Text(), "  ")
		if !ok {
			t.Fatalf("bad SHA256SUMS line %q", sc.Text())
		}
		body, err := staticFS.ReadFile("static/vendor/" + name)
		if err != nil {
			t.Fatalf("%s listed but missing: %v", name, err)
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s: sha256 %s, want %s", name, got, want)
		}
		listed[name] = true
	}
	for name, marker := range map[string]string{
		"htmx.min.js":         `version:"2.0.11"`,
		"htmx-ext-sse.min.js": `defineExtension("sse"`,
	} {
		if !listed[name] {
			t.Errorf("%s is not in SHA256SUMS", name)
			continue
		}
		body, err := staticFS.ReadFile("static/vendor/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(body, []byte(marker)) {
			t.Errorf("%s does not contain %s", name, marker)
		}
	}
	entries, err := fs.ReadDir(staticFS, "static/vendor")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "SHA256SUMS" && !listed[e.Name()] {
			t.Errorf("vendored file %s has no checksum", e.Name())
		}
	}
}

func TestStaticHandler(t *testing.T) {
	r := newTestRenderer(t)
	h := r.Static()
	css, err := r.staticURL("foreman.css")
	if err != nil {
		t.Fatal(err)
	}
	js, err := r.staticURL("vendor/htmx.min.js")
	if err != nil {
		t.Fatal(err)
	}

	get := func(target string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get(css)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("css: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("hashed URL Cache-Control %q", cc)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("no nosniff")
	}
	etag := rec.Header().Get("ETag")
	if rec := get(css, "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d", rec.Code)
	}

	rec = get(js)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Errorf("js: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// An old or missing version must not be cached forever.
	for _, u := range []string{PathStatic + "foreman.css", PathStatic + "foreman.css?v=old"} {
		if cc := get(u).Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control %q", u, cc)
		}
	}

	for _, u := range []string{PathStatic, PathStatic + "vendor/", PathStatic + "vendor/SHA256SUMS", PathStatic + "nope.css", PathStatic + "../render.go"} {
		if rec := get(u); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", u, rec.Code)
		}
	}
}

// Every asset URL a page links to is served by the static handler.
func TestPageAssetsResolve(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, page: WorldsPage{}, user: "todd"})
	re := regexp.MustCompile(`(?:href|src|srcset)="(` + regexp.QuoteMeta(PathStatic) + `[^" ]+)`)
	found := re.FindAllStringSubmatch(body, -1)
	if len(found) != 10 {
		t.Fatalf("want three icons, the stylesheet, htmx, its SSE extension, time.js, menu.js and the logo at two sizes, found %v", found)
	}
	for _, m := range found {
		u := strings.ReplaceAll(m[1], "&amp;", "&")
		rec := httptest.NewRecorder()
		r.Static().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != 200 {
			t.Errorf("%s: %d", u, rec.Code)
		}
	}
}

// The stylesheet uses colour only through tokens (DESIGN 3).
func TestCSSHasNoRawColoursOutsideTokens(t *testing.T) {
	css, err := staticFS.ReadFile("static/foreman.css")
	if err != nil {
		t.Fatal(err)
	}
	hex := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	for i, line := range strings.Split(string(css), "\n") {
		if hex.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "--color-") {
			t.Errorf("line %d: raw colour outside a token: %s", i+1, line)
		}
	}
}

// The brand icons are linked from the page head and served without a session.
func TestFaviconLinksAndFiles(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, page: WorldsPage{}, user: "todd"})
	head := body[:strings.Index(body, "</head>")]
	for _, want := range []string{
		`rel="icon" href="/static/icons/icon-16.png?v=`,
		`sizes="16x16" type="image/png"`,
		`rel="icon" href="/static/icons/icon-32.png?v=`,
		`sizes="32x32" type="image/png"`,
		`rel="apple-touch-icon" href="/static/icons/apple-touch-icon.png?v=`,
	} {
		if !strings.Contains(head, want) {
			t.Errorf("head lacks %s", want)
		}
	}

	hn := newHarness(t, nil)
	for _, c := range []struct{ path, contentType string }{
		{PathFavicon, "image/x-icon"},
		{PathStatic + "icons/favicon.ico", "image/x-icon"},
		{PathStatic + "icons/icon-16.png", "image/png"},
		{PathStatic + "icons/icon-32.png", "image/png"},
		{PathStatic + "icons/apple-touch-icon.png", "image/png"},
	} {
		rec := hn.get(c.path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != c.contentType {
			t.Errorf("%s: %d %q, want 200 %q", c.path, rec.Code, rec.Header().Get("Content-Type"), c.contentType)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty body", c.path)
		}
	}
}
