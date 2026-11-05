package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
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
}

func (ServerPage) templateName() string { return "server" }
func (ServerPage) navItem() string      { return PathServer }

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

// StateWord is the badge's word (DESIGN 7.5).
func (p ServerPage) StateWord() string { return stateWord(p.Status.State) }

func stateWord(s game.State) string {
	switch s {
	case game.StateStopped:
		return "Stopped"
	case game.StateStarting:
		return "Starting"
	case game.StateRunning:
		return "Running"
	case game.StateStopping:
		return "Stopping"
	case game.StateCrashed:
		return "Crashed"
	case game.StateCrashLooping:
		return "Crash-looping"
	}
	return "Unknown"
}

// GameVersion is the version the running game reports, else the world's.
func (p ServerPage) GameVersion() string {
	if p.Status.GameVersion != "" {
		return p.Status.GameVersion
	}
	return p.World.GameVersion
}

// Running reports whether the game has a run to show uptime for.
func (p ServerPage) Running() bool { return !p.Status.StartedAt.IsZero() }

// Uptime says how long the game has run, in whole minutes, and the same as
// an ISO 8601 duration for the time element.
func (p ServerPage) Uptime() string    { return durationText(p.Status.Uptime) }
func (p ServerPage) UptimeISO() string { return durationISO(p.Status.Uptime) }

// CPU is the game's CPU use, as a percentage of one core.
func (p ServerPage) CPU() string { return fmt.Sprintf("%.1f%%", p.Status.CPUPercent) }

// Busy reports whether a start, stop, restart or restore is running.
func (p ServerPage) Busy() bool { return p.Status.Operation != nil && !p.Status.Operation.Done }

// The actions the page offers in the current state. An action that would
// be refused is left out, and ActionsNote says why (DESIGN 7.1).
func (p ServerPage) CanStart() bool {
	s := p.Status.State
	return !p.Busy() && (s == game.StateStopped || s == game.StateCrashLooping)
}

// StartBlocked says why the last attempt to run the world did not, and what
// the admin can do, or is "". It uses what Foreman already knows: the crash
// loop (SPEC 8.9) and the last start or restart the checks refused
// (SPEC 7.4). The page then offers a plain "Try to start again" for after
// the fix, in place of the main Start button; an admin must still be able
// to start by hand (SPEC 8.9).
func (p ServerPage) StartBlocked() string {
	if !p.CanStart() {
		return ""
	}
	if p.Status.State == game.StateCrashLooping {
		return p.World.Name + " crashed again and again the last time it ran. Fix what the game reported, for example the world's mods or its save, then try to start it again. Or switch to another world from the Worlds page."
	}
	if p.HaveResult && (p.Result.Action == game.AuditStart || p.Result.Action == game.AuditRestart) && game.IsRefusal(p.Result.Err) && !p.OffersAnyway() {
		return "Foreman refused the last start of " + p.World.Name + ", for the reason above. Fix the world first, then try to start it again."
	}
	return ""
}

// OffersAnyway reports whether the last result's box offers to go ahead
// anyway; the box then leads alone, with no advice to fix the world first.
func (p ServerPage) OffersAnyway() bool {
	f := p.Failure()
	return f != nil && f.Anyway != nil
}

// blockedAnnouncement is read out with the page's results while a start is
// blocked, so a screen reader user learns why Start server is gone.
const blockedAnnouncement = "Start server is replaced by Try to start again, for after the world is fixed."

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

// ActionsNote explains the actions left out, or is "".
func (p ServerPage) ActionsNote() string {
	switch {
	case p.Status.State == game.StateUnknown:
		return "Foreman cannot start or stop the game until it can ask Docker about it."
	case p.Busy() && p.Status.Operation.Action == game.AuditSwitch:
		return "Wait for the switch to finish, or use Force stop. Force stop ends the switch; the game is stopped without saving."
	case p.Busy():
		return "Wait for this to finish, or use Force stop to stop the game now without saving."
	case p.Status.State == game.StateStarting || p.Status.State == game.StateStopping:
		return "The game is not in game, so it cannot be warned or saved. Wait, or use Force stop."
	}
	return ""
}

// ShowOperation reports whether the step list is shown: while the
// operation runs, and after it failed.
func (p ServerPage) ShowOperation() bool { return showOp(p.Status.Operation) && !p.opSuperseded() }

// ShowDownload is ShowOperation for the image download.
func (p ServerPage) ShowDownload() bool { return showOp(p.Status.Download) }

func showOp(o *game.Operation) bool { return o != nil && (!o.Done || o.Error != "") }

// OperationHeading names the operation in the step list.
func (p ServerPage) OperationHeading() string {
	o := p.Status.Operation
	failed := o.Done && o.Error != ""
	switch o.Action {
	case game.AuditStart:
		return choose(failed, "The start failed", "Starting the server")
	case game.AuditStop:
		return choose(failed, "The stop failed", "Stopping the server")
	case game.AuditRestart:
		return choose(failed, "The restart failed", "Restarting the server")
	case game.ActionRestore:
		return choose(failed, "The restore failed", "Restoring a snapshot")
	case game.AuditUpdate:
		return choose(failed, "The update failed", "Updating the game version")
	case game.AuditRollback:
		return choose(failed, "The rollback failed", "Rolling back the game version")
	case game.AuditSwitch:
		if sw := o.Switch; sw != nil {
			return choose(failed, "The switch from "+p.name(sw.From)+" to "+p.name(sw.To)+" failed",
				"Switching from "+p.name(sw.From)+" to "+p.name(sw.To))
		}
		return choose(failed, "The switch failed", "Switching worlds")
	}
	return choose(failed, "This failed", "Working")
}

func choose(cond bool, yes, no string) string {
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

// Since says how long a player has been online.
func (ServerPage) Since(d time.Duration) string { return durationText(d) }

// refused reports whether this list's failed step is a refusal by the
// pre-start check, not a fault.
func (l stepList) refused() bool {
	return l.Failure != nil && l.Failure.Refused && l.Failure.In == l.In
}

// StepWord is the word beside a step (DESIGN 7.6). A step the pre-start
// check refused reads "Refused", since nothing went wrong.
func (l stepList) StepWord(s game.StepState) string {
	switch s {
	case game.StepDone:
		return "Done"
	case game.StepProgress:
		return "In progress"
	case game.StepFailed:
		if l.refused() {
			return "Refused"
		}
		return "Failed"
	case game.StepSkipped:
		return "Skipped"
	}
	return "Waiting"
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
	Message string
	// Problems are the pre-start check's findings (SPEC 7.4).
	Problems []string
	// Refused is true when the pre-start check refused the action.
	Refused bool
	// Anyway offers to go ahead past the problems, when every problem
	// allows it. The box is then the confirmation (SPEC 7.4, 17.5).
	Anyway *Anyway
	// Download offers to download the missing game image (SPEC 3.7).
	Download bool
	// Undo names the "Before restore" snapshot a failed restore took.
	Undo string
	// In says which step list holds the message: "operation", "download",
	// or "" for on its own.
	In string
	// Earlier is when the action ended, set when a crash after it leads
	// the page.
	Earlier time.Time
}

// Word is the message's leading word. A refusal that offers a choice is a
// warning, not an error: going ahead is the admin's call (DESIGN 7.3).
func (f *Failure) Word() string {
	if f.Anyway != nil {
		return "Warning:"
	}
	return "Error:"
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
const changedLead = "The world or the server changed since the page showed it, so Foreman did not go ahead. Read the message again before you choose."

// precheckLead opens the message for a start the pre-start check refused.
const precheckLead = "Foreman did not start the game, because the save that would load does not match the world:"

// switchPrecheckLead opens the message for a switch the pre-start check of
// the new world refused. Nothing was stopped or changed.
const switchPrecheckLead = "Foreman did not switch, because the save that would load in the new world does not match that world:"

// updatePrecheckLead opens the message for an update the pre-start check
// at the new version refused. Nothing was stopped or changed.
func updatePrecheckLead(check world.PreCheck, name, version string) string {
	for _, pr := range check.Problems {
		if pr.Kind != world.ProblemModGameVersion {
			return "Foreman did not update the game version, because the save does not match " + name + " on Factorio " + version + ":"
		}
	}
	return "Foreman did not update the game version, because " + name + " cannot run in Factorio " + version + ":"
}

// precheckLeadFor opens the message for a refused start or switch. When the
// only problems are mods made for another Factorio, the save is not what
// fails, so the lead says the world cannot run instead. name is the world
// that cannot run; "" says "the world".
func precheckLeadFor(check world.PreCheck, switching bool, name string) string {
	if len(check.Problems) == 0 {
		return choose(switching, switchPrecheckLead, precheckLead)
	}
	for _, pr := range check.Problems {
		if pr.Kind != world.ProblemModGameVersion {
			return choose(switching, switchPrecheckLead, precheckLead)
		}
	}
	if name == "" {
		name = "the world"
	}
	return choose(switching, "Foreman did not switch", "Foreman did not start the game") +
		", because " + name + " cannot run in this game:"
}

// Failure returns the last action's error, or nil.
func (p ServerPage) Failure() *Failure {
	if !p.HaveResult || p.Result.Err == nil {
		return nil
	}
	err := p.Result.Err
	f := &Failure{Undo: p.Result.Undo}
	if p.Crashed() && p.CrashLeads() {
		f.Earlier = p.Result.At
	}
	var pe *game.PrecheckError
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
			f.Message = changedLead + " " + f.Message
		}
		for _, pr := range pe.Check.Problems {
			f.Problems = append(f.Problems, pr.Text)
		}
		if pe.Check.CanStartAnyway() {
			f.Anyway = p.anyway(pe.Check)
		}
	case errors.Is(err, game.ErrImageMissing):
		f.Message, f.Download = sentence(err), true
	default:
		f.Message = failureText(err)
	}
	f.Refused = game.IsRefusal(err)
	text := err.Error()
	// A forced stop that interrupted a switch says in the operation's own
	// error which world it stopped.
	if o := p.Status.Operation; errors.Is(err, game.ErrCancelled) && o != nil && o.Done && o.Error != "" {
		f.Message, text = sentence(errors.New(o.Error)), o.Error
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
	// Path is where the button posts; Verb is the button's words.
	Path, Verb string
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
	// Advice says how to keep what missing mods built, or is "".
	Advice string
}

// anyway builds the offer for the last refused action.
func (p ServerPage) anyway(check world.PreCheck) *Anyway {
	a := &Anyway{
		Path: p.StartPath(), Verb: "Start anyway", World: p.World.Name,
		Countdown: defaultCountdownSeconds, Advice: anywayAdvice(check.Problems),
		Running: game.IsRunning(p.Status.State),
	}
	switch p.Result.Action {
	case game.AuditRestart:
		a.Path, a.Verb, a.Restart = p.RestartPath(), "Restart anyway", true
	case game.AuditSwitch:
		a.Path, a.Verb, a.Switch = switchPath(p.Result.Target), "Switch anyway", true
		a.World, a.From, a.FromID = p.name(p.Result.Target), p.World.Name, p.World.ID
	case game.AuditUpdate:
		// The refusal came from the start after the version was set, so
		// going ahead is a plain start anyway (SPEC 10.3, 7.4).
		if p.World.GameVersion == p.Result.Version {
			break
		}
		a.Path, a.Verb, a.Update, a.Version = updatePath(p.World.ID), "Update anyway", true, p.Result.Version
	}
	a.Seen = game.AnywayFingerprint(check, a.Running)
	return a
}

// anywayAdvice says how to keep what missing mods built; the lead already
// says what the game drops. Mods that are only turned off need no file.
func anywayAdvice(problems []world.Problem) string {
	missing := 0
	for _, pr := range problems {
		if pr.Kind == world.ProblemModMissing {
			missing += len(pr.Mods)
		}
	}
	if missing == 0 {
		return ""
	}
	built := "that mod built"
	if missing > 1 {
		built = "those mods built"
	}
	return "Foreman cannot add mods to a world yet. To keep what " + built +
		", put the mod files in the import folder's mods/ and import the world again."
}

// failureText words an action's error; one the admin cannot fix is logged
// when the action ends and is not shown.
func failureText(err error) string {
	status, msg := gameError(err)
	if status == http.StatusInternalServerError || msg == "" {
		return "Foreman could not finish this. The details are in Foreman's log."
	}
	return msg
}

// actionNames name each action in a sentence.
var actionNames = map[string]string{
	game.AuditStart: "start", game.AuditStop: "stop", game.AuditRestart: "restart",
	game.AuditForceStop: "forced stop", game.AuditImageDownload: "image download", game.ActionRestore: "restore",
	game.AuditSwitch: "switch", game.AuditUpdate: "update", game.AuditRollback: "rollback",
}

// OtherErrors words the errors of the actions asked for while another ran.
func (p ServerPage) OtherErrors() []string {
	var out []string
	for _, res := range p.Others {
		if res.Err == nil {
			continue
		}
		name := actionNames[res.Action]
		if name == "" {
			name = "action"
		}
		out = append(out, "The "+name+" asked for during another action did not finish: "+failureText(res.Err))
	}
	return out
}

// Done says what the last action did, when that is not otherwise visible.
func (p ServerPage) Done() string {
	if !p.HaveResult || p.Result.Err != nil {
		return ""
	}
	switch p.Result.Action {
	case game.ActionRestore:
		return "The world is restored. Foreman took the snapshot \"Before restore\" first; to undo the restore, restore that snapshot from the Snapshots page."
	case game.AuditUpdate:
		return "The game version is updated. Foreman took the snapshot \"Before update\" first; to undo, use Roll back on the world's page."
	case game.AuditRollback:
		return "The world is rolled back. Foreman took the snapshot \"Before restore\" first; to undo the rollback, restore that snapshot from the Snapshots page."
	case game.AuditSwitch:
		return "The worlds are switched. Foreman took the snapshot \"Before switch\" of both worlds first; to undo, switch back, or restore that snapshot from the Snapshots page."
	}
	return ""
}

// doneAnnouncement is what a live page reads out when an action that
// shows a "Done" message finishes.
var doneAnnouncement = map[string]string{
	game.ActionRestore: "The world is restored.",
	game.AuditSwitch:   "The worlds are switched.",
	game.AuditUpdate:   "The game version is updated.",
	game.AuditRollback: "The world is rolled back.",
}

// resultAnnouncement is what a live page reads out for the results the
// page shows: errors first, then a restore that finished (DESIGN 7.5).
func resultAnnouncement(p ServerPage) string {
	var said []string
	if f := p.Failure(); f != nil {
		said = append(said, f.Word()+" "+f.Message)
		if f.Anyway != nil {
			said = append(said, "You can "+strings.ToLower(f.Anyway.Verb)+"; the button is below.")
		}
	}
	for _, e := range p.OtherErrors() {
		said = append(said, "Error: "+e)
	}
	if p.Done() != "" {
		said = append(said, doneAnnouncement[p.Result.Action])
	}
	return strings.Join(said, " ")
}

// Announcement is the sentence a live page reads out when the state
// changes (DESIGN 7.5).
func announcement(s game.State) string {
	if s == game.StateUnknown {
		return "Foreman cannot tell the server's state"
	}
	return "Server is now " + strings.ToLower(stateWord(s))
}

// durationText says a duration in whole minutes, hours and days.
func durationText(d time.Duration) string {
	m := int(d / time.Minute)
	switch {
	case m < 1:
		return "less than 1 min"
	case m < 60:
		return fmt.Sprintf("%d min", m)
	case m < 24*60:
		return fmt.Sprintf("%d h %d min", m/60, m%60)
	}
	return fmt.Sprintf("%d d %d h", m/(24*60), m/60%24)
}

// durationISO is d as an ISO 8601 duration in whole minutes, for datetime.
func durationISO(d time.Duration) string {
	m := int(d / time.Minute)
	return fmt.Sprintf("PT%dH%dM", m/60, m%60)
}
