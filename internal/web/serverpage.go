package web

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// ServerPage is the server's status and controls for the active world
// (SPEC 7.1, 7.2, 8.1, DESIGN 7.5, 7.6).
type ServerPage struct {
	World    world.Summary
	HasWorld bool
	Status   game.Status
	// Players is who is online, when PlayersKnown; PlayersError says the
	// game could not be asked.
	Players      []game.Player
	PlayersKnown bool
	PlayersError bool
	// Result is how the last action ended, when HaveResult.
	Result     actionResult
	HaveResult bool
	// Others are the actions asked for while it ran, shown apart.
	Others []actionResult
	// WorldNames holds the names of the worlds a switch, or the notice of
	// an unfinished one, speaks of, by id.
	WorldNames map[string]string
	// Image is the game image the world runs, and ImageMissing whether
	// Docker lacks it; both are looked up only while the game can start
	// (SPEC 3.7).
	Image        string
	ImageMissing bool
	// Now is when the page was built, for the countdown's time left.
	Now time.Time
	// Rollback undoes an update that worked (SPEC 10.4); set only then.
	Rollback *RollbackOffer
	// StartAnywaySnap is the newest "Before start anyway" snapshot, which a
	// crash loop offers to restore (SPEC 17.9); set only while crash-looping.
	StartAnywaySnap *world.Snapshot
	// JoinAddress is the host:port players join, from FOREMAN_PUBLIC_ADDRESS;
	// "" hides the line (SPEC 3.2).
	JoinAddress string
	// Live is set for a live update, whose facts also carry the heading's
	// state badge out of band (DESIGN 7.5).
	Live bool
	// FocusResult is true when this load follows the admin's own cancel stop
	// or snapshot, so the Done message takes focus and is read out.
	FocusResult bool
	// ModsNotice says the page loads its line about mods players cannot get
	// (#131), which needs the mod portal.
	ModsNotice bool
}

// navPage marks a nav item as the page itself rather than a page inside its
// section, so the layout can tell aria-current "page" from "true" (DESIGN 6).
// The layout writes the same suffix.
const navPage = "#page"

func (ServerPage) templateName() string { return "server" }
func (ServerPage) navItem() string      { return PathServer + navPage }

func (p ServerPage) liveURL() string {
	if !p.HasWorld {
		return ""
	}
	return gamePath(p.World.ID, "events")
}

// Paths the server page links and posts to.
func (p ServerPage) WorldPath() string     { return worldPath(p.World.ID) }
func (p ServerPage) SnapshotsPath() string { return snapshotsPath(p.World.ID) }
func (p ServerPage) StartPath() string     { return gamePath(p.World.ID, "start") }
func (p ServerPage) StopPath() string      { return gamePath(p.World.ID, "stop") }
func (p ServerPage) RestartPath() string   { return gamePath(p.World.ID, "restart") }
func (p ServerPage) ForceStopPath() string { return gamePath(p.World.ID, "force-stop") }
func (p ServerPage) PullPath() string      { return gamePath(p.World.ID, "pull-image") }
func (p ServerPage) ConsolePath() string   { return consolePath(p.World.ID, "") }
func (p ServerPage) SnapshotPath() string  { return gamePath(p.World.ID, "take-snapshot") }

// switchOp returns the switch running now, or nil.
func (p ServerPage) switchOp() *game.SwitchInfo {
	if o := p.Status.Operation; o != nil && !o.Done {
		return o.Switch
	}
	return nil
}

// ActiveName and ActivePath name and link the active world. During a
// switch that is the new world once it has been made active, though the
// page still follows the old one until the switch ends (SPEC 7.9).
func (p ServerPage) ActiveName() string {
	if sw := p.switchOp(); sw != nil && sw.Presented {
		return p.name(sw.To)
	}
	return p.World.Name
}

func (p ServerPage) ActivePath() string {
	if sw := p.switchOp(); sw != nil && sw.Presented {
		return worldPath(sw.To)
	}
	return p.WorldPath()
}

// Switching says which worlds a running switch is between, or is zero.
func (p ServerPage) Switching() i18n.Msg {
	if sw := p.switchOp(); sw != nil {
		return i18n.M("server.facts.switching_value", "from", p.name(sw.From), "to", p.name(sw.To))
	}
	return i18n.Msg{}
}

// FactsKnown reports whether the run's figures (uptime, save, CPU, memory)
// are true of the active world now. They are not when Docker cannot be
// asked, nor once a switch has made the new world active (SPEC 7.1, 7.9).
func (p ServerPage) FactsKnown() bool {
	if p.Status.State == game.StateUnknown {
		return false
	}
	sw := p.switchOp()
	return sw == nil || !sw.Presented
}

// StateWord is the badge's word (DESIGN 7.5).
func (p ServerPage) StateWord() i18n.Msg { return stateWord(p.Status.State) }

// Reason says why the state is not known, or is zero.
func (p ServerPage) Reason() i18n.Msg { return unknownMsg(p.Status.Unknown) }

// GameVersion is the version the running game reports, else the world's.
func (p ServerPage) GameVersion() string {
	if p.Status.GameVersion != "" && p.FactsKnown() {
		return p.Status.GameVersion
	}
	return p.World.GameVersion
}

// Running reports whether the game has a run to show uptime for.
func (p ServerPage) Running() bool { return !p.Status.StartedAt.IsZero() }

// Uptime says how long the game has run, in whole minutes, and the same as
// an ISO 8601 duration for the time element.
func (p ServerPage) Uptime() i18n.Msg  { return durationText(p.Status.Uptime) }
func (p ServerPage) UptimeISO() string { return durationISO(p.Status.Uptime) }

// Busy reports whether a start, stop, restart or restore is running.
func (p ServerPage) Busy() bool { return p.Status.Operation != nil && !p.Status.Operation.Done }

// The actions the page offers in the current state. An action that would
// be refused is left out, and ActionsNote says why (DESIGN 7.1).
func (p ServerPage) CanStart() bool {
	s := p.Status.State
	return !p.Busy() && (s == game.StateStopped || s == game.StateCrashLooping)
}

// downloading reports whether the game image is being downloaded now.
func (p ServerPage) downloading() bool {
	return p.Status.Download != nil && !p.Status.Download.Done
}

// ImageNote says, before any Start, that the world cannot start until its
// game image is downloaded, or is "" (SPEC 3.7). Start is then replaced by
// the download. A failed start that said so already needs no second note.
func (p ServerPage) ImageNote() i18n.Msg {
	if !p.ImageMissing || !p.CanStart() || p.downloading() {
		return i18n.Msg{}
	}
	if f := p.Failure(); f != nil && f.Download {
		return i18n.Msg{}
	}
	return i18n.M("server.image.missing_note", "world", p.World.Name, "image", p.Image)
}

// OffersDownload reports whether the page offers the game image download:
// when the image is missing before a start, or after a start refused for it.
func (p ServerPage) OffersDownload() bool {
	if p.Busy() || p.downloading() {
		return false
	}
	if p.ImageMissing && p.CanStart() {
		return true
	}
	f := p.Failure()
	return f != nil && f.Download
}

// Downloaded confirms a download that worked, while the world waits to
// start, or is zero.
func (p ServerPage) Downloaded() i18n.Msg {
	if !p.CanStart() || p.ImageMissing || p.Image == "" {
		return i18n.Msg{}
	}
	results := p.Others
	if p.HaveResult {
		results = append([]actionResult{p.Result}, results...)
	}
	for _, res := range results {
		if res.Action == game.AuditImageDownload && res.Err == nil {
			return i18n.M("server.image.downloaded", "image", p.Image, "world", p.World.Name)
		}
	}
	return i18n.Msg{}
}

// switchRefused reports whether the last result is a switch that ended
// before it began, so the old world is still active and nothing changed.
func (p ServerPage) switchRefused() bool {
	if !p.HaveResult || p.Result.Action != game.AuditSwitch || p.Result.Err == nil {
		return false
	}
	f := p.Failure()
	return f != nil && f.In != "operation"
}

// StartLabel is the Start button's words. After a refused switch the
// button names the world it starts, which is not the one the admin asked
// to switch to.
func (p ServerPage) StartLabel() i18n.Msg {
	if p.switchRefused() {
		return i18n.M("server.action.start_world", "world", p.World.Name)
	}
	return i18n.M("server.action.start")
}

// StartBlocked says why the last attempt to run the world did not, and what
// the admin can do, or is zero. It uses what Foreman already knows: the crash
// loop (SPEC 8.9) and the last start or restart the checks refused
// (SPEC 7.4). The page then offers a plain "Try to start again" for after
// the fix, in place of the main Start button; an admin must still be able
// to start by hand (SPEC 8.9).
func (p ServerPage) StartBlocked() i18n.Msg {
	if !p.CanStart() {
		return i18n.Msg{}
	}
	if p.Status.State == game.StateCrashLooping {
		return i18n.M("server.blocked.crash_loop", "world", p.World.Name)
	}
	if p.HaveResult && (p.Result.Action == game.AuditStart || p.Result.Action == game.AuditRestart) && game.IsRefusal(p.Result.Err) && !p.OffersAnyway() {
		return i18n.M("server.blocked.refused", "world", p.World.Name)
	}
	return i18n.Msg{}
}

// OffersAnyway reports whether the last result's box offers to go ahead
// anyway; the box then leads alone, with no advice to fix the world first.
func (p ServerPage) OffersAnyway() bool {
	f := p.Failure()
	return f != nil && f.Anyway != nil
}

// blockedAnnouncement is read out with the page's results while a start is
// blocked, so a screen reader user learns why Start server is gone.
var blockedAnnouncement = i18n.M("server.blocked.announce")

// CrashLooping reports whether the crash box leads with the ways to
// recover, in place of a separate note (SPEC 8.9, 17.9).
func (p ServerPage) CrashLooping() bool { return p.Status.State == game.StateCrashLooping }

// CanSnapshot reports whether Take snapshot is offered: when no operation
// runs and the game is settled, so the snapshot is not refused (SPEC 6.10).
func (p ServerPage) CanSnapshot() bool {
	switch p.Status.State {
	case game.StateStopped, game.StateRunning, game.StateCrashLooping:
		return !p.Busy()
	}
	return false
}

// CancelStopPath is where Cancel stop posts while players are being warned,
// or "". It names the world being stopped: in a switch, the one switched
// from (SPEC 7.6).
func (p ServerPage) CancelStopPath() string {
	o := p.Status.Operation
	if o == nil || o.Done {
		return ""
	}
	if _, ok := o.CountdownEnds(); !ok {
		return ""
	}
	id := p.World.ID
	if o.Switch != nil {
		id = o.Switch.From
	}
	return gamePath(id, "cancel-stop")
}

// PlayerTable is the player list the page shows.
type PlayerTable struct {
	Caption i18n.Msg
	Players []game.Player
}

// PlayerTable returns who is online now, or else, while a stop warns
// players, who was online when it began (SPEC 7.6, 8.1); nil when there is
// nobody to list.
func (p ServerPage) PlayerTable() *PlayerTable {
	if p.PlayersKnown {
		if len(p.Players) == 0 {
			return nil
		}
		return &PlayerTable{Caption: i18n.M("server.players.caption", "count", len(p.Players)), Players: p.Players}
	}
	o := p.Status.Operation
	if o == nil || o.Done || len(o.Players) == 0 || p.Status.State == game.StateUnknown {
		return nil
	}
	return &PlayerTable{Caption: i18n.M("server.players.caption_stop", "count", len(o.Players)), Players: o.Players}
}

// Crashed reports whether the page shows the crash message.
func (p ServerPage) Crashed() bool {
	return p.Status.State == game.StateCrashed || p.Status.State == game.StateCrashLooping
}

// CrashLeads reports whether the crash is the newest thing the page reports,
// so its message comes first; otherwise it is marked as earlier.
func (p ServerPage) CrashLeads() bool {
	// A crash whose time is not known yet is the current state, so it leads.
	c := p.Status.LastCrash
	return p.Crashed() && (!p.HaveResult || c.IsZero() || p.Result.At.Before(c))
}

// opSuperseded reports whether the failed operation the status still holds
// is older than the last result, as when a later switch was refused before
// it began: its step list and note are then dropped.
func (p ServerPage) opSuperseded() bool {
	o := p.Status.Operation
	if o == nil || !o.Done || o.Error == "" || !p.HaveResult {
		return false
	}
	f := p.Failure()
	return f == nil || f.In != "operation"
}

func (p ServerPage) CanStop() bool {
	s := p.Status.State
	return !p.Busy() && (s == game.StateRunning || s == game.StateCrashed)
}

func (p ServerPage) CanRestart() bool {
	return !p.Busy() && p.Status.State == game.StateRunning
}

func (p ServerPage) CanForceStop() bool {
	switch p.Status.State {
	case game.StateStopped, game.StateCrashLooping, game.StateUnknown:
		return false
	}
	return true
}

// ActionsNote explains the actions left out, or is zero.
func (p ServerPage) ActionsNote() i18n.Msg {
	switch {
	case p.Status.State == game.StateUnknown:
		return i18n.M("server.actions_note.unknown")
	case p.Busy() && p.Status.Operation.Action == game.AuditSwitch:
		return i18n.M("server.actions_note.switch")
	case p.Busy():
		return i18n.M("server.actions_note.busy")
	case p.Status.State == game.StateStarting || p.Status.State == game.StateStopping:
		return i18n.M("server.actions_note.unsettled")
	}
	return i18n.Msg{}
}

// ShowOperation reports whether the step list is shown: while the
// operation runs, and after it failed.
func (p ServerPage) ShowOperation() bool {
	return showOp(p.Status.Operation) && !p.opSuperseded() && !p.cancelled()
}

// ShowDownload is ShowOperation for the image download.
func (p ServerPage) ShowDownload() bool { return showOp(p.Status.Download) }

func showOp(o *game.Operation) bool { return o != nil && (!o.Done || o.Error != "") }

// PortalUnchecked says the last start, restart or switch could not ask the
// mod portal, so it went ahead without knowing whether players can download
// the world's mods (SPEC 7.4).
func (p ServerPage) PortalUnchecked() bool {
	o := p.Status.Operation
	return o != nil && o.PortalUnchecked && !p.opSuperseded() && !p.cancelled()
}

// OperationHeading names the operation in the step list.
func (p ServerPage) OperationHeading() i18n.Msg {
	o := p.Status.Operation
	failed := o.Done && o.Error != ""
	switch o.Action {
	case game.AuditStart:
		if f := p.Failure(); failed && f != nil && f.Refused && f.In == "operation" {
			return i18n.M("server.op.start_refused")
		}
		return choose(failed, i18n.M("server.op.start_failed"), i18n.M("server.op.start"))
	case game.AuditStop:
		return choose(failed, i18n.M("server.op.stop_failed"), i18n.M("server.op.stop"))
	case game.AuditRestart:
		return choose(failed, i18n.M("server.op.restart_failed"), i18n.M("server.op.restart"))
	case game.ActionRestore:
		return choose(failed, i18n.M("server.op.restore_failed"), i18n.M("server.op.restore"))
	case game.AuditUpdate:
		return choose(failed, i18n.M("server.op.update_failed"), i18n.M("server.op.update"))
	case game.AuditRollback:
		return choose(failed, i18n.M("server.op.rollback_failed"), i18n.M("server.op.rollback"))
	case game.AuditMods:
		return choose(failed, i18n.M("server.op.mods_failed"), i18n.M("server.op.mods"))
	case game.AuditSwitch:
		if sw := o.Switch; sw != nil {
			from, to := p.name(sw.From), p.name(sw.To)
			return choose(failed, i18n.M("server.op.switch_named_failed", "from", from, "to", to),
				i18n.M("server.op.switch_named", "from", from, "to", to))
		}
		return choose(failed, i18n.M("server.op.switch_failed"), i18n.M("server.op.switch"))
	}
	return choose(failed, i18n.M("server.op.other_failed"), i18n.M("server.op.other"))
}

func choose[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

// stepList is one step list and the failure that may sit in it.
type stepList struct {
	Page    ServerPage
	Op      *game.Operation
	Failure *Failure
	In      string // the Failure.In value that puts the failure here
}

// OperationList and DownloadList are the two step lists the page shows.
func (p ServerPage) OperationList() stepList {
	return stepList{Page: p, Op: p.Status.Operation, Failure: p.Failure(), In: "operation"}
}

func (p ServerPage) DownloadList() stepList {
	return stepList{Page: p, Op: p.Status.Download, Failure: p.Failure(), In: "download"}
}

// Steps are the steps the list shows. After a refusal by the pre-start
// check the steps that never ran are left out, since nothing was tried.
func (l stepList) Steps() []game.Step {
	if !l.refused() {
		return l.Op.Steps
	}
	var out []game.Step
	for _, s := range l.Op.Steps {
		if s.State != game.StepSkipped {
			out = append(out, s)
		}
	}
	return out
}

// Left says how long the countdown on a step has to run, or is zero.
func (l stepList) Left(s game.Step) i18n.Msg {
	if s.State != game.StepProgress || s.Until.IsZero() {
		return i18n.Msg{}
	}
	n := int(math.Ceil(s.Until.Sub(l.Page.Now).Seconds()))
	if n <= 0 {
		return i18n.M("server.step.ending_now")
	}
	return i18n.M("server.step.seconds_left", "count", n)
}

// StepName is a step's words.
func (stepList) StepName(s game.Step) i18n.Msg { return stepName(s.Name) }

// Since says how long a player has been online.
func (ServerPage) Since(d time.Duration) i18n.Msg { return durationText(d) }

// refused reports whether this list's failed step is a refusal by the
// pre-start check, not a fault.
func (l stepList) refused() bool {
	return l.Failure != nil && l.Failure.Refused && l.Failure.In == l.In
}

// StepWord is the word beside a step (DESIGN 7.6). A step the pre-start
// check refused reads "Refused", since nothing went wrong.
func (l stepList) StepWord(s game.StepState) i18n.Msg {
	switch s {
	case game.StepDone:
		return i18n.M("server.step.done")
	case game.StepProgress:
		return i18n.M("server.step.progress")
	case game.StepFailed:
		if l.refused() {
			return i18n.M("server.step.refused")
		}
		return i18n.M("server.step.failed")
	case game.StepSkipped:
		return i18n.M("server.step.skipped")
	}
	return i18n.M("server.step.waiting")
}

// StepClass is the step word's modifier class, or "". A refusal that
// offers a choice is a warning, matching its message (DESIGN 7.3).
func (l stepList) StepClass(s game.StepState) string {
	if s == game.StepFailed && l.refused() && l.Failure.Anyway != nil {
		return "step-state--warning"
	}
	switch s {
	case game.StepDone, game.StepProgress, game.StepFailed:
		return "step-state--" + string(s)
	}
	return ""
}

// Failure is the last action's error, worded for the admin, with what the
// page can offer next.
type Failure struct {
	// Message may hold the game's own words, so it is shown with msgHTML.
	Message i18n.Msg
	// Problems are the pre-start check's findings (SPEC 7.4).
	Problems []i18n.Msg
	// Refused is true when the pre-start check refused the action.
	Refused bool
	// Anyway offers to go ahead past the problems, when every problem
	// allows it. The box is then the confirmation (SPEC 7.4, 17.5).
	Anyway *Anyway
	// Download offers to download the missing game image (SPEC 3.7).
	Download bool
	// Undo names the "Before restore" snapshot a failed restore took, and
	// UndoPath is where restoring it is confirmed.
	Undo, UndoPath string
	// UndoLabel names the snapshot Undo restores when it is not a "Before
	// restore" one.
	UndoLabel i18n.Msg
	// Fix is the main fix a refusal offers first, such as matching the
	// world's mods to its save (SPEC 9.8, 17.9), or nil.
	Fix *Offer
	// Next is a link that follows the message, or nil.
	Next *Offer
	// In says which step list holds the message: "operation", "download",
	// or "" for on its own.
	In string
	// Earlier is when the action ended, set when a crash after it leads
	// the page.
	Earlier time.Time
}

// Word is the message's leading word. A refusal that offers a choice is a
// warning, not an error: going ahead is the admin's call (DESIGN 7.3).
func (f *Failure) Word() i18n.Msg {
	if f.Anyway != nil {
		return i18n.M("common.label.warning")
	}
	return i18n.M("common.error.label")
}

// Class is the message's kind class, matching Word.
func (f *Failure) Class() string {
	if f.Anyway != nil {
		return "message--warning"
	}
	return "message--error"
}

// changedLead says why a go-ahead anyway did not run: what the box showed
// was no longer true, so the admin must read it again (SPEC 7.4, 17.5).
var changedLead = i18n.M("server.precheck.changed")

// Failure returns the last action's error, or nil.
func (p ServerPage) Failure() *Failure {
	if !p.HaveResult || p.Result.Err == nil || p.cancelled() {
		return nil
	}
	err := p.Result.Err
	f := &Failure{Undo: p.Result.Undo}
	if f.Undo != "" {
		f.UndoPath = restorePath(p.World.ID, f.Undo)
	}
	if p.Result.Action == game.AuditMods {
		f.UndoLabel = i18n.M("worlds.snapshot.label.before_mods")
	}
	if p.Crashed() && p.CrashLeads() {
		f.Earlier = p.Result.At
	}
	var pe *game.PrecheckError
	var de *game.ImageDownloadError
	switch {
	case errors.As(err, &pe):
		switching := p.Result.Action == game.AuditSwitch
		name := p.World.Name
		if switching {
			name = p.WorldNames[p.Result.Target]
		}
		f.Message = precheckLeadFor(pe.Check, switching, name)
		if p.Result.Action == game.AuditUpdate {
			f.Message = updatePrecheckLead(pe.Check, p.World.Name, p.Result.Version)
		}
		if pe.Changed {
			f.Message = join(changedLead, f.Message)
		}
		f.Problems = problemMsgs(pe.Check.Problems)
		if pe.Check.CanStartAnyway() {
			f.Anyway = p.anyway(pe.Check)
		}
		id := p.World.ID
		if switching && p.Result.Target != "" {
			id = p.Result.Target
		}
		f.Fix = precheckFix(pe.Check, id)
	case errors.Is(err, world.ErrModsChanged):
		f.Message, f.Next = failureText(err), &Offer{Path: modsPath(p.World.ID), Label: i18n.M("mods.changed.next")}
	case errors.As(err, &de):
		if !de.NotFound {
			f.Message = downloadFailure(de, p.Result.Action == game.AuditSwitch)
			break
		}
		// The version to fix belongs to the world the image is for.
		id := p.World.ID
		if p.Result.Action == game.AuditSwitch && p.Result.Target != "" {
			id = p.Result.Target
		}
		f.Message, f.Next = noImageLead(de), &Offer{Path: worldPath(id), Label: i18n.M("server.download.no_image_advice")}
	case errors.Is(err, game.ErrImageMissing):
		// The download is for this page's world only: a switch whose new
		// world lacks its image must never offer the old world's.
		f.Message = i18n.M("server.error.image_missing")
		f.Download = p.Result.Action != game.AuditSwitch || p.Result.Target == p.World.ID
	default:
		f.Message = failureText(err)
	}
	f.Refused = game.IsRefusal(err)
	text := err.Error()
	// A forced stop that interrupted a switch says in the operation's own
	// error which world it stopped.
	if o := p.Status.Operation; errors.Is(err, game.ErrCancelled) && o != nil && o.Done && o.Error != "" {
		if o.Err != nil {
			f.Message = failureText(o.Err)
		}
		text = o.Error
	}
	switch {
	case p.Status.Operation != nil && p.Status.Operation.Done && p.Status.Operation.Error == text:
		f.In = "operation"
	case p.Status.Download != nil && p.Status.Download.Done && p.Status.Download.Error == text:
		f.In = "download"
	}
	return f
}

// Anyway is the offer to start, restart or switch past the pre-start
// check's problems. It names every step and the snapshots taken first, so
// its button acts at once (SPEC 7.4, 17.5).
type Anyway struct {
	// Path is where the button posts; Verb is the button's words, Open
	// the words of the choice that shows it, and Offer says the choice is
	// there, for a screen reader.
	Path              string
	Verb, Open, Offer i18n.Msg
	// Switch is set for a switch; Restart for a restart of a running game;
	// Update for a game version update to Version.
	Switch, Restart, Update bool
	Version                 string
	// World is the world that loads the save. From is the world a switch
	// leaves, and FromID its id, which the form sends.
	World, From, FromID string
	// Running is true when the game runs now, so it is stopped first.
	Running bool
	// Countdown is the warning players get before the stop, in seconds.
	Countdown int
	// Seen fingerprints what the box shows, so a stale box is refused.
	Seen string
}

// anyway builds the offer for the last refused action.
func (p ServerPage) anyway(check world.PreCheck) *Anyway {
	a := &Anyway{
		Path: p.StartPath(), Verb: i18n.M("server.anyway.start"), Open: i18n.M("server.anyway.start_open"), Offer: i18n.M("server.anyway.start_offer"), World: p.World.Name,
		Countdown: defaultCountdownSeconds,
		Running:   game.IsRunning(p.Status.State),
	}
	switch p.Result.Action {
	case game.AuditRestart:
		a.Path, a.Verb, a.Open, a.Offer, a.Restart = p.RestartPath(), i18n.M("server.anyway.restart"), i18n.M("server.anyway.restart_open"), i18n.M("server.anyway.restart_offer"), true
	case game.AuditSwitch:
		a.Path, a.Verb, a.Open, a.Offer, a.Switch = switchPath(p.Result.Target), i18n.M("server.anyway.switch"), i18n.M("server.anyway.switch_open"), i18n.M("server.anyway.switch_offer"), true
		a.World, a.From, a.FromID = p.name(p.Result.Target), p.World.Name, p.World.ID
	case game.AuditUpdate:
		// The refusal came from the start after the version was set, so
		// going ahead is a plain start anyway (SPEC 10.3, 7.4).
		if p.World.GameVersion != p.Result.Version {
			a.toUpdate(p.World.ID, p.Result.Version)
		}
	}
	a.Seen = game.AnywayFingerprint(check, a.Running)
	return a
}

// updateAnyway is the offer to update a world that is not active past the
// pre-start check, shown on the world's own page (SPEC 10.3, 7.4).
func updateAnyway(check world.PreCheck, sum world.Summary, version string) *Anyway {
	a := &Anyway{World: sum.Name, Countdown: defaultCountdownSeconds}
	a.toUpdate(sum.ID, version)
	a.Seen = game.AnywayFingerprint(check, a.Running)
	return a
}

// toUpdate makes the offer an update of the world id to version.
func (a *Anyway) toUpdate(id, version string) {
	a.Path, a.Verb, a.Open, a.Offer, a.Update, a.Version = updatePath(id), i18n.M("server.anyway.update"), i18n.M("server.anyway.update_open"), i18n.M("server.anyway.update_offer"), true, version
}

// durationISO is d as an ISO 8601 duration in whole minutes, for datetime.
func durationISO(d time.Duration) string {
	m := int(d / time.Minute)
	return fmt.Sprintf("PT%dH%dM", m/60, m%60)
}
