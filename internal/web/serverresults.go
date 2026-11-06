package web

import (
	"errors"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

// How the last action on the Server page ended, and the next step it offers
// as a control: undo after a change, recovery after a crash loop
// (SPEC 17.9, VISION principle 1).

// Offer is a link to an action's confirmation page.
type Offer struct {
	Path  string
	Label i18n.Msg
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
	return &Offer{Path: restorePath(p.World.ID, sn.Name), Label: i18n.M("server.recover.restore", "label", sn.Label, "time", momentUTC(sn.TakenAt)), At: sn.TakenAt}
}

// CrashMessage is the crash box's sentence (SPEC 8.7).
func (p ServerPage) CrashMessage() i18n.Msg {
	return crashMsg(p.Status.State == game.StateCrashLooping, !p.CrashLeads(), p.Status.LastCrash, p.Status.Crash)
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

// otherError says an action asked for during another did not finish, and
// why.
func otherError(action string, why i18n.Msg) i18n.Msg {
	switch action {
	case game.AuditStart:
		return i18n.M("server.other.start", "why", why)
	case game.AuditStop:
		return i18n.M("server.other.stop", "why", why)
	case game.AuditRestart:
		return i18n.M("server.other.restart", "why", why)
	case game.AuditForceStop:
		return i18n.M("server.other.force_stop", "why", why)
	case game.AuditImageDownload:
		return i18n.M("server.other.download", "why", why)
	case game.ActionRestore:
		return i18n.M("server.other.restore", "why", why)
	case game.AuditSwitch:
		return i18n.M("server.other.switch", "why", why)
	case game.AuditUpdate:
		return i18n.M("server.other.update", "why", why)
	case game.AuditRollback:
		return i18n.M("server.other.rollback", "why", why)
	case actionSnapshot:
		return i18n.M("server.other.snapshot", "why", why)
	}
	return i18n.M("server.other.action", "why", why)
}

// cancelledMsg says an action's stop countdown was cancelled, which changed
// nothing, and short for a screen reader (SPEC 7.6).
func cancelledMsg(action string) (msg, short i18n.Msg) {
	switch action {
	case game.AuditStop:
		return i18n.M("server.cancelled.stop"), i18n.M("server.cancelled.stop_short")
	case game.AuditRestart:
		return i18n.M("server.cancelled.restart"), i18n.M("server.cancelled.restart_short")
	case game.ActionRestore:
		return i18n.M("server.cancelled.restore"), i18n.M("server.cancelled.restore_short")
	case game.AuditUpdate:
		return i18n.M("server.cancelled.update"), i18n.M("server.cancelled.update_short")
	case game.AuditRollback:
		return i18n.M("server.cancelled.rollback"), i18n.M("server.cancelled.rollback_short")
	case game.AuditSwitch:
		return i18n.Msg{}, i18n.M("server.cancelled.switch_short")
	}
	return i18n.M("server.cancelled.action"), i18n.M("server.cancelled.action_short")
}

// OtherErrors words the errors of the actions asked for while another ran.
func (p ServerPage) OtherErrors() []i18n.Msg {
	var out []i18n.Msg
	for _, res := range p.Others {
		if res.Err == nil {
			continue
		}
		out = append(out, otherError(res.Action, failureText(res.Err)))
	}
	return out
}

// Done says what the last action did, when that is not otherwise visible,
// or is zero.
func (p ServerPage) Done() i18n.Msg {
	if !p.HaveResult {
		return i18n.Msg{}
	}
	if p.cancelled() {
		if p.Result.Action == game.AuditSwitch {
			return i18n.M("server.cancelled.switch", "to", p.name(p.Result.Target), "world", p.World.Name)
		}
		msg, _ := cancelledMsg(p.Result.Action)
		return msg
	}
	if p.Result.Err != nil {
		return i18n.Msg{}
	}
	switch p.Result.Action {
	case game.ActionRestore:
		return i18n.M("server.done.restore")
	case game.AuditUpdate:
		return i18n.M("server.done.update")
	case game.AuditRollback:
		return i18n.M("server.done.rollback")
	case game.AuditSwitch:
		return i18n.M("server.done.switch", "from", p.name(p.Result.From), "to", p.World.Name)
	case actionSnapshot:
		return i18n.M("server.done.snapshot", "label", p.Result.Taken, "world", p.World.Name)
	}
	return i18n.Msg{}
}

// DoneAt is when the action in the Done message ended.
func (p ServerPage) DoneAt() time.Time { return p.Result.At }

// Undo offers to undo the change the Done message reports, at its own
// confirmation page (VISION principle 1, SPEC 17.9), or is nil.
func (p ServerPage) Undo() *Offer {
	if p.Done().IsZero() || p.cancelled() {
		return nil
	}
	switch p.Result.Action {
	case game.ActionRestore:
		if p.Result.Undo != "" {
			return &Offer{Path: restorePath(p.World.ID, p.Result.Undo), Label: i18n.M("server.undo.restore")}
		}
	case game.AuditRollback:
		if p.Result.Undo != "" {
			return &Offer{Path: restorePath(p.World.ID, p.Result.Undo), Label: i18n.M("server.undo.rollback")}
		}
	case game.AuditUpdate:
		if r := p.Rollback; r != nil {
			return &Offer{Path: r.Path, Label: i18n.M("server.undo.update", "version", r.Version)}
		}
	case game.AuditSwitch:
		// After a crash loop the crash box offers it instead.
		if o := p.switchBackOffer(); o != nil && !p.CrashLooping() {
			return &Offer{Path: o.Path, Label: i18n.M("server.switch_back", "world", o.Name)}
		}
	}
	return nil
}

// doneAnnouncement is what a live page reads out when an action that
// shows a "Done" message finishes.
var doneAnnouncement = map[string]i18n.Msg{
	game.ActionRestore: i18n.M("server.done.restore_short"),
	game.AuditSwitch:   i18n.M("server.done.switch_short"),
	game.AuditUpdate:   i18n.M("server.done.update_short"),
	game.AuditRollback: i18n.M("server.done.rollback_short"),
	actionSnapshot:     i18n.M("server.done.snapshot_short"),
}

// labelled puts a message's leading word, such as "Error:", before it.
func labelled(label, text i18n.Msg) i18n.Msg {
	return i18n.M("server.labelled", "label", label, "text", text)
}

// resultAnnouncement is what a live page reads out for the results the
// page shows: errors first, then a restore that finished (DESIGN 7.5). It
// is zero when there is nothing to say.
func resultAnnouncement(p ServerPage) i18n.Msg {
	var said []i18n.Msg
	if f := p.Failure(); f != nil {
		msg := f.Message
		if f.Next != nil {
			msg = join(msg, f.Next.Label)
		}
		said = append(said, labelled(f.Word(), msg))
		if f.Anyway != nil {
			said = append(said, f.Anyway.Offer)
		}
	}
	for _, e := range p.OtherErrors() {
		said = append(said, labelled(i18n.M("common.error.label"), e))
	}
	switch {
	case p.cancelled():
		_, short := cancelledMsg(p.Result.Action)
		said = append(said, short)
	case !p.Done().IsZero():
		said = append(said, doneAnnouncement[p.Result.Action])
	}
	if len(said) == 0 {
		return i18n.Msg{}
	}
	return join(said[0], said[1:]...)
}
