package web

import (
	"cmp"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
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
	return &Offer{Path: restorePath(p.World.ID, sn.Name), Label: i18n.M("server.recover.restore", "label", snapshotLabelArg(*sn), "time", momentUTC(sn.TakenAt)), At: sn.TakenAt}
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
	case game.AuditMods:
		return i18n.M("server.other.mods", "why", why)
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
	case game.AuditMods:
		return i18n.M("server.cancelled.mods"), i18n.M("server.cancelled.mods_short")
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
		return i18n.M("server.done.restore", "label", i18n.M("worlds.snapshot.label.before_restore"))
	case game.AuditUpdate:
		return i18n.M("server.done.update", "label", i18n.M("worlds.snapshot.label.before_update"))
	case game.AuditRollback:
		return i18n.M("server.done.rollback", "label", i18n.M("worlds.snapshot.label.before_restore"))
	case game.AuditSwitch:
		return i18n.M("server.done.switch", "from", p.name(p.Result.From), "to", p.World.Name,
			"label", i18n.M("worlds.snapshot.label.before_switch"))
	case actionSnapshot:
		return i18n.M("server.done.snapshot", "label", p.Result.Taken, "world", p.World.Name)
	case game.AuditMods:
		return modsDone(p.Result.Mods, p.World.Name)
	}
	return i18n.Msg{}
}

// modResult is what a mod change's result keeps from its plan: the
// changes, to say what changed, and the plan's page, to try it again
// (SPEC 9.14, 17.9). Retry is "" when the plan cannot be opened again, as
// when it used an uploaded zip, which is gone after the change.
type modResult struct {
	Changes []mods.Change
	Retry   string
	// Since is when the snapshot the change reused was taken, before an
	// earlier change of the same run (SPEC 9.4); zero when it took its own.
	Since time.Time
}

// modsDoneNamed is how many mods a Done message names before "and N more".
const modsDoneNamed = 3

// modsDone says what a finished mod change did to world, then the snapshot
// it took first, or the earlier one it kept (SPEC 9.4, 17.9). A change with
// no mod in it, such as a settings upload, keeps the plain sentence.
func modsDone(res modResult, world string) i18n.Msg {
	label := i18n.M("worlds.snapshot.label.before_mods")
	what := modsChanged(res.Changes, world)
	switch {
	case !res.Since.IsZero() && what.IsZero():
		what = i18n.M("server.mods_done.changed", "world", world)
	case what.IsZero():
		return i18n.M("mods.done", "world", world, "label", label)
	}
	if !res.Since.IsZero() {
		return join(what, i18n.M("server.mods_done.kept", "label", label, "time", momentUTC(res.Since)))
	}
	return join(what, i18n.M("server.mods_done.snapshot", "label", label))
}

// modUndoSnapshot is the snapshot that undoes a mod change, given the
// world's newest mod snapshot and its open run of mod changes from before
// it (SPEC 9.4, 17.9), and when that run began if the change continued it.
// It is "" when no snapshot undoes the change: a refusal touches nothing,
// and a failure that left the run's files as they were changed nothing.
func (s *server) modUndoSnapshot(id string, err error, newest string, run world.Snapshot, inRun bool) (string, time.Time) {
	now, open := s.worlds.ModRunSnapshot(id)
	switch {
	case err == nil && open && inRun && now.Name == run.Name:
		return now.Name, run.TakenAt
	case err == nil && open:
		return now.Name, time.Time{}
	}
	if after := s.newestModSnapshot(id); after != "" && after != newest {
		return after, time.Time{}
	}
	// The change reused the run's snapshot and then changed the files without
	// recording the run, so that snapshot still undoes it.
	if inRun && !open && (err == nil || !game.IsRefusal(err)) {
		return run.Name, run.TakenAt
	}
	return "", time.Time{}
}

// modsUndo offers the snapshot that undoes a mod change; after a run of
// changes it names when the run began, since it undoes them all (SPEC 9.4).
func modsUndo(id string, res actionResult) *Offer {
	if res.Undo == "" {
		return nil
	}
	label := i18n.M("mods.undo")
	if !res.Mods.Since.IsZero() {
		label = i18n.M("server.undo.mods_since", "time", momentUTC(res.Mods.Since))
	}
	return &Offer{Path: restorePath(id, res.Undo), Label: label}
}

// modActs are the kinds of change a Done message names, in the order a
// mixed change counts them.
var modActs = []mods.Action{mods.ActInstall, mods.ActUpgrade, mods.ActDowngrade, mods.ActEnable, mods.ActDisable, mods.ActRemove}

// modActDone is the sentence of a change of one kind.
func modActDone(act mods.Action, items i18n.Msg, world string) i18n.Msg {
	switch act {
	case mods.ActInstall:
		return i18n.M("server.mods_done.install", "mods", items, "world", world)
	case mods.ActUpgrade:
		return i18n.M("server.mods_done.upgrade", "mods", items, "world", world)
	case mods.ActDowngrade:
		return i18n.M("server.mods_done.downgrade", "mods", items, "world", world)
	case mods.ActEnable:
		return i18n.M("server.mods_done.enable", "mods", items, "world", world)
	case mods.ActDisable:
		return i18n.M("server.mods_done.disable", "mods", items, "world", world)
	}
	return i18n.M("server.mods_done.remove", "mods", items, "world", world)
}

// modActCount counts the mods of one kind in a mixed change.
func modActCount(act mods.Action, n int) i18n.Msg {
	switch act {
	case mods.ActInstall:
		return i18n.M("server.mods_done.count.install", "count", n)
	case mods.ActUpgrade:
		return i18n.M("server.mods_done.count.upgrade", "count", n)
	case mods.ActDowngrade:
		return i18n.M("server.mods_done.count.downgrade", "count", n)
	case mods.ActEnable:
		return i18n.M("server.mods_done.count.enable", "count", n)
	case mods.ActDisable:
		return i18n.M("server.mods_done.count.disable", "count", n)
	}
	return i18n.M("server.mods_done.count.remove", "count", n)
}

// modsChanged names the mods a change of one kind touched ("Installed a,
// b and c in Base."), or counts each kind of a mixed one ("Changed 12 mods
// in Base: 9 updated, 3 installed."). It is zero when no mod changed.
func modsChanged(changes []mods.Change, world string) i18n.Msg {
	by := map[mods.Action][]mods.Change{}
	n := 0
	for _, c := range changes {
		if slices.Contains(modActs, c.Action) {
			by[c.Action] = append(by[c.Action], c)
			n++
		}
	}
	if n == 0 {
		return i18n.Msg{}
	}
	var kinds []i18n.Msg
	for _, act := range modActs {
		if cs := by[act]; len(by) == 1 && len(cs) > 0 {
			return modActDone(act, modItems(act, cs), world)
		} else if len(cs) > 0 {
			kinds = append(kinds, modActCount(act, len(cs)))
		}
	}
	return i18n.M("server.mods_done.many", "count", n, "world", world, "kinds", listMsg(kinds))
}

// modItems lists up to modsDoneNamed mods, with the version a changed
// version went to, and how many more there are.
func modItems(act mods.Action, cs []mods.Change) i18n.Msg {
	var items []i18n.Msg
	for _, c := range cs[:min(len(cs), modsDoneNamed)] {
		if act == mods.ActUpgrade || act == mods.ActDowngrade {
			items = append(items, i18n.M("server.mods_done.to", "mod", i18n.Ident(c.Name), "version", c.To))
		} else {
			items = append(items, i18n.M("server.list.names", "names", i18n.Ident(c.Name)))
		}
	}
	switch {
	case len(cs) > modsDoneNamed:
		return i18n.M("server.list.more", "names", listMsg(items), "count", len(cs)-modsDoneNamed)
	case len(items) == 1:
		return items[0]
	}
	return andList([]i18n.Msg{listMsg(items[:len(items)-1]), items[len(items)-1]})
}

// modsFailure words a mod change that failed (SPEC 9.12, 17.9). A failed
// download names the mod and version, says nothing was changed and
// whether world kept running, and offers the next step as a control: the
// account page for a refused login, else trying the same plan again. Back
// is where the account page returns when there is no plan to return to.
// Any other failure keeps its usual words and no fix.
func modsFailure(err error, res modResult, world string, state game.State, back string) (i18n.Msg, *Offer) {
	account := &Offer{Path: accountLink(cmp.Or(res.Retry, back)), Label: i18n.M("server.mods_failed.account")}
	if errors.Is(err, game.ErrNoModFetcher) {
		return failureText(err), account
	}
	de, ok := errors.AsType[*game.ModDownloadError](err)
	if !ok {
		return failureText(err), nil
	}
	mod, version := i18n.Ident(de.Mod), de.Version
	var what i18n.Msg
	switch {
	case errors.Is(err, modportal.ErrChecksum):
		what = i18n.M("server.mods_failed.checksum", "mod", mod, "version", version)
	case errors.Is(err, modportal.ErrUnauthorized):
		what = i18n.M("server.mods_failed.unauthorized", "mod", mod, "version", version)
	case errors.Is(err, modportal.ErrNotOnPortal):
		what = i18n.M("server.mods_failed.gone", "mod", mod, "version", version)
	default:
		what = i18n.M("server.mods_failed.download", "mod", mod, "version", version)
	}
	// The download comes before the stop, so the game is as it was.
	switch state {
	case game.StateRunning:
		what = join(what, i18n.M("server.mods_failed.kept_running", "world", world))
	case game.StateStopped:
		what = join(what, i18n.M("server.mods_failed.stayed_stopped", "world", world))
	default:
		what = join(what, i18n.M("server.mods_failed.unchanged"))
	}
	switch {
	case errors.Is(err, modportal.ErrUnauthorized):
		return what, account
	case res.Retry != "":
		return what, &Offer{Path: res.Retry, Label: i18n.M("server.mods_failed.try_again")}
	}
	return what, nil
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
	case game.AuditMods:
		return modsUndo(p.World.ID, p.Result)
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
	game.AuditMods:     i18n.M("server.done.mods_short"),
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
	case !p.Done().IsZero() && p.Result.Action == game.AuditMods:
		// Which mods changed is the news; the snapshot is said on the page.
		what := modsChanged(p.Result.Mods.Changes, p.World.Name)
		if what.IsZero() {
			what = doneAnnouncement[game.AuditMods]
		}
		said = append(said, what)
	case !p.Done().IsZero():
		said = append(said, doneAnnouncement[p.Result.Action])
	}
	if len(said) == 0 {
		return i18n.Msg{}
	}
	return join(said[0], said[1:]...)
}
