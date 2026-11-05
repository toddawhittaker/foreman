package web

import (
	"errors"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
)

// How the last action on the Server page ended, and the next step it offers
// as a control: undo after a change, recovery after a crash loop
// (SPEC 17.9, VISION principle 1).

// Offer is a link to an action's confirmation page.
type Offer struct {
	Path, Label string
	// At is when the snapshot the offer restores was taken, or zero.
	At time.Time
}

// RecoverRestore offers to restore the newest "Before start anyway"
// snapshot after a crash loop, or is nil.
func (p ServerPage) RecoverRestore() *Offer {
	sn := p.StartAnywaySnap
	if !p.CrashLooping() || sn == nil {
		return nil
	}
	return &Offer{Path: restorePath(p.World.ID, sn.Name), Label: "Restore \"" + sn.Label + "\"", At: sn.TakenAt}
}

// switchBackOffer is the way back from the last switch: from a failed or
// unfinished one (SwitchBack), or else from one that worked, while Foreman
// still holds its result. A restart of Foreman forgets the latter
// (SPEC 7.9, 17.9).
func (p ServerPage) switchBackOffer() *SwitchOffer {
	if o := p.SwitchBack(); o != nil {
		return o
	}
	if p.Busy() || !p.HaveResult || p.Result.Action != game.AuditSwitch || p.Result.Err != nil || p.Result.From == "" {
		return nil
	}
	return &SwitchOffer{Path: switchPath(p.Result.From), Name: p.name(p.Result.From)}
}

// RecoverSwitch offers the switch back after a crash loop, or is nil.
func (p ServerPage) RecoverSwitch() *SwitchOffer {
	if !p.CrashLooping() {
		return nil
	}
	return p.switchBackOffer()
}

// LoneSwitchBack is SwitchBack when the crash box does not already offer it.
func (p ServerPage) LoneSwitchBack() *SwitchOffer {
	if p.CrashLooping() {
		return nil
	}
	return p.SwitchBack()
}

// cancelled reports whether the last action ended because its stop
// countdown was cancelled, which changed nothing (SPEC 7.6).
func (p ServerPage) cancelled() bool {
	return p.HaveResult && errors.Is(p.Result.Err, game.ErrStopCancelled)
}

// cancelledByOperation returns the result of a stop countdown's action from
// the finished operation, for the moment after the cancel when the run has
// ended in the controller but its result is not stored yet (SPEC 7.6).
func cancelledByOperation(st game.Status, now time.Time) (actionResult, bool) {
	o := st.Operation
	if o == nil || !o.Done || !strings.Contains(o.Error, game.ErrStopCancelled.Error()) {
		return actionResult{}, false
	}
	res := actionResult{Action: o.Action, Err: game.ErrStopCancelled, At: now}
	if o.Switch != nil {
		res.Target, res.From = o.Switch.To, o.Switch.From
	}
	return res, true
}

// actionNames name each action in a sentence.
var actionNames = map[string]string{
	game.AuditStart: "start", game.AuditStop: "stop", game.AuditRestart: "restart",
	game.AuditForceStop: "forced stop", game.AuditImageDownload: "image download", game.ActionRestore: "restore",
	game.AuditSwitch: "switch", game.AuditUpdate: "update", game.AuditRollback: "rollback",
	actionSnapshot: "snapshot",
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
	if !p.HaveResult {
		return ""
	}
	if p.cancelled() {
		if p.Result.Action == game.AuditSwitch {
			return "The switch to " + p.name(p.Result.Target) + " was cancelled. " + p.World.Name + " is still the active world, and nothing changed."
		}
		return "The " + actionNames[p.Result.Action] + " was cancelled. Nothing was saved or stopped, and nothing changed."
	}
	if p.Result.Err != nil {
		return ""
	}
	switch p.Result.Action {
	case game.ActionRestore:
		return "The world is restored. Foreman took the snapshot \"Before restore\" first."
	case game.AuditUpdate:
		return "The game version is updated. Foreman took the snapshot \"Before update\" first."
	case game.AuditRollback:
		return "The world is rolled back. Foreman took the snapshot \"Before restore\" first."
	case game.AuditSwitch:
		return "The worlds are switched, from " + p.name(p.Result.From) + " to " + p.World.Name + ". Foreman took the snapshot \"Before switch\" of both worlds first."
	case actionSnapshot:
		return "Foreman took the snapshot \"" + p.Result.Taken + "\" of " + p.World.Name + "."
	}
	return ""
}

// DoneAt is when the action in the Done message ended.
func (p ServerPage) DoneAt() time.Time { return p.Result.At }

// Undo offers to undo the change the Done message reports, at its own
// confirmation page (VISION principle 1, SPEC 17.9), or is nil.
func (p ServerPage) Undo() *Offer {
	if p.Done() == "" || p.cancelled() {
		return nil
	}
	switch p.Result.Action {
	case game.ActionRestore:
		if p.Result.Undo != "" {
			return &Offer{Path: restorePath(p.World.ID, p.Result.Undo), Label: "Undo this restore"}
		}
	case game.AuditRollback:
		if p.Result.Undo != "" {
			return &Offer{Path: restorePath(p.World.ID, p.Result.Undo), Label: "Undo this rollback"}
		}
	case game.AuditUpdate:
		if r := p.Rollback; r != nil {
			return &Offer{Path: r.Path, Label: "Roll back to " + r.Version}
		}
	case game.AuditSwitch:
		// After a crash loop the crash box offers it instead.
		if o := p.switchBackOffer(); o != nil && !p.CrashLooping() {
			return &Offer{Path: o.Path, Label: "Switch back to " + o.Name}
		}
	}
	return nil
}

// doneAnnouncement is what a live page reads out when an action that
// shows a "Done" message finishes.
var doneAnnouncement = map[string]string{
	game.ActionRestore: "The world is restored.",
	game.AuditSwitch:   "The worlds are switched.",
	game.AuditUpdate:   "The game version is updated.",
	game.AuditRollback: "The world is rolled back.",
	actionSnapshot:     "The snapshot is taken.",
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
	switch {
	case p.cancelled():
		said = append(said, "The "+actionNames[p.Result.Action]+" was cancelled.")
	case p.Done() != "":
		said = append(said, doneAnnouncement[p.Result.Action])
	}
	return strings.Join(said, " ")
}
