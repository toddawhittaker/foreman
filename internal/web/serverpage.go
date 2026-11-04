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

func (p ServerPage) CanStop() bool {
	s := p.Status.State
	return !p.Busy() && (s == game.StateRunning || s == game.StateCrashed)
}

func (p ServerPage) CanRestart() bool { return !p.Busy() && p.Status.State == game.StateRunning }

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
	// AnywayPath confirms "Start anyway", when every problem allows it.
	AnywayPath string
	// Download offers to download the missing game image (SPEC 3.7).
	Download bool
	// Undo names the "Before restore" snapshot a failed restore took.
	Undo string
	// In says which step list holds the message: "operation", "download",
	// or "" for on its own.
	In string
}

// Failure returns the last action's error, or nil.
func (p ServerPage) Failure() *Failure {
	if !p.HaveResult || p.Result.Err == nil {
		return nil
	}
	err := p.Result.Err
	f := &Failure{Undo: p.Result.Undo}
	var pe *game.PrecheckError
	switch {
	case errors.As(err, &pe):
		f.Message = "Foreman did not start the game, because the save that would load does not match the world:"
		for _, pr := range pe.Check.Problems {
			f.Problems = append(f.Problems, pr.Text)
		}
		if pe.Check.CanStartAnyway() {
			f.AnywayPath = gamePath(p.World.ID, "start-anyway")
			if p.Result.Action == game.AuditRestart {
				f.AnywayPath += "?then=restart"
			}
		}
	case errors.Is(err, game.ErrImageMissing):
		f.Message, f.Download = sentence(err), true
	default:
		f.Message = failureText(err)
	}
	text := err.Error()
	switch {
	case p.Status.Operation != nil && p.Status.Operation.Done && p.Status.Operation.Error == text:
		f.In = "operation"
	case p.Status.Download != nil && p.Status.Download.Done && p.Status.Download.Error == text:
		f.In = "download"
	}
	return f
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

// Done says what the last action did, when that is not otherwise visible.
func (p ServerPage) Done() string {
	if !p.HaveResult || p.Result.Err != nil || p.Result.Action != game.ActionRestore {
		return ""
	}
	return "The world is restored. Foreman took the snapshot \"Before restore\" first; to undo the restore, restore that snapshot from the Snapshots page."
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
