package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamedir"
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
	// Adopting is set when World is the one a first adoption is building,
	// which is not the active world yet (SPEC 4.8).
	Adopting bool
	// VersionWarning is SPEC 4.9's warning for the world's save, in adopted
	// mode; "" when there is none.
	VersionWarning string
}

func (ServerPage) templateName() string { return "server" }
func (ServerPage) navItem() string      { return PathServer }

func (p ServerPage) liveURL() string {
	if !p.HasWorld {
		return ""
	}
	if p.Adopting {
		// The stream must know the page shows the adoption, so it can tell
		// the page to load again whenever the world has become the active one.
		return gamePath(p.World.ID, "events") + "?" + adoptingParam + "=1"
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
func (ServerPage) AdoptPath() string       { return PathAdopt }

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
// In adopted mode the container's owner chooses the version, so it is the
// container's, or unknown (SPEC 4.9).
func (p ServerPage) GameVersion() string {
	switch {
	case p.Status.Adopted && p.Status.RunningVersion != "unknown":
		return p.Status.RunningVersion
	case p.Status.Adopted:
		return ""
	case p.Status.GameVersion != "":
		return p.Status.GameVersion
	}
	return p.World.GameVersion
}

// controllable reports whether the game folder lets Foreman control the
// game. Outside adopted mode it always does. Otherwise only a fresh or
// ready folder does; an unadopted or broken one, or one not yet looked at,
// does not (SPEC 4.8).
func (p ServerPage) controllable() bool {
	if !p.Status.Adopted {
		return true
	}
	k := p.Status.Layout.Kind
	return k == gamedir.Fresh || k == gamedir.Ready
}

// LayoutBroken reports an adopted-mode game folder Foreman cannot use.
func (p ServerPage) LayoutBroken() bool {
	return p.Status.Adopted && p.Status.Layout.Kind == gamedir.Broken
}

// LayoutProblem is what is wrong with a broken game folder, as the end of a
// sentence. The reason already names the path, so the path is not added.
func (p ServerPage) LayoutProblem() string {
	return strings.TrimSuffix(p.Status.Layout.Reason, ".")
}

// CanAdopt reports whether the page offers to adopt the game folder.
func (p ServerPage) CanAdopt() bool {
	return p.Status.Adopted && p.Status.Layout.Kind == gamedir.Unadopted && !p.Busy()
}

// NeedsLayoutStep reports whether the game folder, not the game, is what
// the page must talk about.
func (p ServerPage) NeedsLayoutStep() bool { return p.LayoutBroken() || p.CanAdopt() }

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
	return p.controllable() && !p.Busy() && (s == game.StateStopped || s == game.StateCrashLooping)
}

func (p ServerPage) CanStop() bool {
	s := p.Status.State
	return p.controllable() && !p.Busy() && (s == game.StateRunning || s == game.StateCrashed)
}

func (p ServerPage) CanRestart() bool {
	return p.controllable() && !p.Busy() && p.Status.State == game.StateRunning
}

func (p ServerPage) CanForceStop() bool {
	if !p.controllable() {
		return false
	}
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
func (p ServerPage) ShowOperation() bool { return showOp(p.Status.Operation) }

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
	case game.AuditAdopt:
		return choose(failed, "The adoption failed", "Adopting the game folder")
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

// StepWord is the word beside a step (DESIGN 7.6).
func (ServerPage) StepWord(s game.StepState) string {
	switch s {
	case game.StepDone:
		return "Done"
	case game.StepProgress:
		return "In progress"
	case game.StepFailed:
		return "Failed"
	case game.StepSkipped:
		return "Skipped"
	}
	return "Waiting"
}

// StepClass is the step word's modifier class, or "".
func (ServerPage) StepClass(s game.StepState) string {
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
	// AnywayPath confirms "Start anyway" or "Switch anyway", when every
	// problem allows it; AnywayVerb and AnywayText word the offer.
	AnywayPath string
	AnywayVerb string
	AnywayText string
	// Download offers to download the missing game image (SPEC 3.7).
	Download bool
	// Undo names the "Before restore" snapshot a failed restore took.
	Undo string
	// In says which step list holds the message: "operation", "download",
	// or "" for on its own.
	In string
}

// precheckLead opens the message for a start the pre-start check refused.
const precheckLead = "Foreman did not start the game, because the save that would load does not match the world:"

// switchPrecheckLead opens the message for a switch the pre-start check of
// the new world refused. Nothing was stopped or changed.
const switchPrecheckLead = "Foreman did not switch, because the save that would load in the new world does not match that world:"

// Failure returns the last action's error, or nil.
func (p ServerPage) Failure() *Failure {
	if !p.HaveResult || p.Result.Err == nil {
		return nil
	}
	// A network refusal is stale once a later check found the network
	// clean (SPEC 4.4).
	if errors.Is(p.Result.Err, game.ErrNetwork) && p.Result.At.Before(p.Status.NetworkCleanAt) {
		return nil
	}
	err := p.Result.Err
	f := &Failure{Undo: p.Result.Undo}
	var pe *game.PrecheckError
	switch {
	case errors.As(err, &pe):
		f.Message = choose(p.Result.Action == game.AuditSwitch, switchPrecheckLead, precheckLead)
		for _, pr := range pe.Check.Problems {
			f.Problems = append(f.Problems, pr.Text)
		}
		if pe.Check.CanStartAnyway() {
			f.AnywayPath, f.AnywayVerb = gamePath(p.World.ID, "start-anyway"), "Start anyway"
			switch p.Result.Action {
			case game.AuditRestart:
				f.AnywayPath += "?then=restart"
			case game.AuditSwitch:
				f.AnywayPath, f.AnywayVerb = switchAnywayPath(p.Result.Target), "Switch anyway"
			}
			f.AnywayText = anywayText(pe.Check.Problems, p.Result.Action == game.AuditSwitch)
		}
	case errors.Is(err, game.ErrImageMissing):
		// In adopted mode the container's owner supplies the image.
		f.Message, f.Download = sentence(err), !p.Status.Adopted
	default:
		f.Message = failureText(err)
	}
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

// anywayText words the offer to go ahead past problems that allow it. When
// mods are missing or turned off, it says how to keep what they built.
func anywayText(problems []world.Problem, switching bool) string {
	mods := 0
	for _, pr := range problems {
		mods += len(pr.Mods)
	}
	if mods == 0 {
		if switching {
			return "You can fix the world first, or switch anyway. Switching anyway takes a snapshot of both worlds first, and the game then deletes what is listed above."
		}
		return "You can fix the world first, or start anyway. Starting anyway takes a snapshot first, and the game then deletes what is listed above."
	}
	built := "that mod built"
	if mods > 1 {
		built = "those mods built"
	}
	anyway := "start anyway: Foreman takes a snapshot first"
	if switching {
		anyway = "switch anyway: Foreman takes a snapshot of both worlds first"
	}
	return "Foreman cannot add or turn on a world's mods yet. To keep what " + built +
		", put the mod files in the import folder's mods/ and import the world again. Or " + anyway +
		", so you can undo it from the Snapshots page."
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
	game.AuditSwitch: "switch", game.AuditAdopt: "adoption",
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
	case game.AuditSwitch:
		return "The worlds are switched. Foreman took the snapshot \"Before switch\" of both worlds first; to undo, switch back, or restore that snapshot from the Snapshots page."
	case game.AuditAdopt:
		return "The game folder is adopted. Foreman took the snapshot \"Before adoption\" first. The original saves, mods and config folders are kept in a pre-foreman folder inside the game folder."
	}
	return ""
}

// doneAnnouncement is what a live page reads out when an action that
// shows a "Done" message finishes.
var doneAnnouncement = map[string]string{
	game.ActionRestore: "The world is restored.",
	game.AuditSwitch:   "The worlds are switched.",
	game.AuditAdopt:    "The game folder is adopted.",
}

// resultAnnouncement is what a live page reads out for the results the
// page shows: errors first, then a restore that finished (DESIGN 7.5).
func resultAnnouncement(p ServerPage) string {
	var said []string
	if f := p.Failure(); f != nil {
		said = append(said, "Error: "+f.Message)
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
