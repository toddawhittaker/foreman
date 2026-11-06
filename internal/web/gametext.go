package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Server control's words for the admin, built from the game package's
// errors and facts, in any language (SPEC 22.3). The game package keeps
// English text for the log; these say the same.

// join puts whole sentences one after another.
func join(first i18n.Msg, rest ...i18n.Msg) i18n.Msg {
	out := first
	for _, m := range rest {
		out = i18n.M("server.join", "first", out, "second", m)
	}
	return out
}

// gameError turns an error from server control into a status and words for
// the admin. Anything it does not know goes to worldError; a 500 means the
// caller logs the error and shows no detail.
func gameError(err error) (int, i18n.Msg) {
	if pe, ok := errors.AsType[*game.PrecheckError](err); ok {
		return http.StatusConflict, join(precheckLeadFor(pe.Check, false, ""), problemMsgs(pe.Check.Problems)...)
	}
	if errors.Is(err, game.ErrCountdown) {
		return http.StatusBadRequest, i18n.M("server.error.countdown")
	}
	if msg, ok := refusalMsg(err); ok {
		return http.StatusConflict, msg
	}
	// A Docker that answers nothing has a plain sentence of its own; the raw
	// transport error stays in the log.
	if ue, ok := errors.AsType[*docker.UnreachableError](err); ok {
		return http.StatusServiceUnavailable, i18n.M("server.error.docker_unreachable", "target", ue.Target)
	}
	if de, ok := errors.AsType[*game.ImageDownloadError](err); ok {
		return http.StatusBadGateway, downloadFailure(de, false)
	}
	// Any other failed connection is wrapped in *url.Error.
	if _, ok := errors.AsType[*url.Error](err); ok {
		return http.StatusServiceUnavailable, i18n.M("server.error.docker_down")
	}
	return worldError(err)
}

// refusalMsg words the refusals of server control that the admin can act
// on, or reports false.
func refusalMsg(err error) (i18n.Msg, bool) {
	if e, ok := errors.AsType[*game.StartError](err); ok {
		return startErrorMsg(e), true
	}
	if e, ok := errors.AsType[*game.ForcedStopError](err); ok {
		if e.Stopped {
			return i18n.M("server.error.cancelled_stopped", "world", e.World), true
		}
		return i18n.M("server.error.cancelled_none", "world", e.World), true
	}
	if e, ok := errors.AsType[*game.NetworkError](err); ok {
		return networkMsg(e), true
	}
	if e, ok := errors.AsType[game.NotActiveError](err); ok {
		return i18n.M("server.error.not_active_named", "world", e.Name), true
	}
	for _, r := range refusals {
		if errors.Is(err, r.err) {
			return r.msg, true
		}
	}
	return i18n.Msg{}, false
}

// refusals are the plain refusals, each with its words.
var refusals = []struct {
	err error
	msg i18n.Msg
}{
	{game.ErrAlreadyRunning, i18n.M("server.error.already_running")},
	{game.ErrNotRunning, i18n.M("server.error.not_running")},
	{game.ErrNotReady, i18n.M("server.error.not_ready")},
	{game.ErrImageMissing, i18n.M("server.error.image_missing")},
	{game.ErrRCONUnreachable, i18n.M("server.error.rcon_unreachable")},
	{game.ErrSaveTimeout, i18n.M("server.error.save_timeout")},
	{game.ErrStartFailed, i18n.M("server.error.start_failed")},
	{game.ErrCancelled, i18n.M("server.error.cancelled")},
	{game.ErrStopCancelled, i18n.M("server.error.stop_cancelled")},
	{game.ErrNoCountdown, i18n.M("server.error.no_countdown")},
	{game.ErrDownloading, i18n.M("server.error.downloading")},
	{game.ErrForeign, i18n.M("server.error.foreign")},
	{game.ErrPaused, i18n.M("server.error.paused")},
	{game.ErrNetwork, i18n.M("server.error.network")},
	{world.ErrSaveInFuture, i18n.M("server.error.save_in_future")},
	{game.ErrSwitchSame, i18n.M("server.error.switch_same")},
	{game.ErrSwitchNotActive, i18n.M("server.error.switch_not_active")},
	{game.ErrSwitchNoTarget, i18n.M("server.error.switch_no_target")},
	{game.ErrSwitchTargetRunning, i18n.M("server.error.switch_target_running")},
	{game.ErrSwitchNoActive, i18n.M("server.error.switch_no_active")},
	{game.ErrSwitchUnreadable, i18n.M("server.error.switch_unreadable")},
	{game.ErrSwitchNewer, i18n.M("server.error.switch_newer")},
	{game.ErrSwitchBusy, i18n.M("server.error.switch_busy")},
	{game.ErrSwitchUnsettled, i18n.M("server.error.switch_unsettled")},
	{game.ErrSwitchUnavailable, i18n.M("server.error.switch_unavailable")},
	{game.ErrWorldNotActive, i18n.M("server.error.not_active")},
	{world.ErrVersionNotNewer, i18n.M("server.error.version_not_newer")},
	{world.ErrVersionUnsupported, i18n.M("server.error.version_unsupported")},
	{world.ErrNoRollback, i18n.M("server.error.no_rollback")},
}

// startErrorMsg words why the game did not start (SPEC 7.3, 8.7). The
// game's own words stay English.
func startErrorMsg(e *game.StartError) i18n.Msg {
	switch e.Kind {
	case game.StartLoadFailed:
		if e.Detail != "" {
			return i18n.M("server.start.load_failed_said", "said", i18n.Game(e.Detail))
		}
		return i18n.M("server.start.load_failed")
	case game.StartStopped:
		r := e.Reason
		switch {
		case r.Mods > 0:
			return i18n.M("server.start.mods", "reason", loadModsMsg(r))
		case r.Said != "":
			return i18n.M("server.start.said", "said", i18n.Game(strings.TrimSuffix(r.Said, ".")))
		}
		return i18n.M("server.start.stopped_loading")
	case game.StartLoadTimeout:
		return i18n.M("server.start.load_timeout", "wait", waitWords(e.Timeout))
	case game.StartRCONTimeout:
		return i18n.M("server.start.rcon_timeout", "wait", waitWords(e.Timeout))
	case game.StartRCONRefused:
		return i18n.M("server.start.rcon_refused")
	}
	return i18n.M("server.error.start_failed")
}

// loadModsMsg says which mods were made for another Factorio, from the
// game's log (SPEC 8.7): the clause after "did not start:" or "stopped it:".
func loadModsMsg(r game.LoadReason) i18n.Msg {
	if len(r.Shown) > 0 {
		return i18n.M("server.load.mods_named", "count", r.Mods, "required", r.Required, "current", r.Current, "names", modNames(r.Shown, r.More))
	}
	return i18n.M("server.load.mods", "count", r.Mods, "required", r.Required, "current", r.Current)
}

// crashMsg is the crash box's sentence (SPEC 8.7, 8.9): whether the game
// crashed once or again and again, when, whether a later result leads the
// page, and the game's reason from its log.
func crashMsg(looping, earlier bool, at time.Time, r game.LoadReason) i18n.Msg {
	t, mods, said := momentUTC(at), loadModsMsg(r), i18n.Game(strings.TrimSuffix(r.Said, "."))
	pick := func(none, withMods, withSaid i18n.Msg) i18n.Msg {
		switch {
		case r.Mods > 0:
			return withMods
		case r.Said != "":
			return withSaid
		}
		return none
	}
	known := !at.IsZero()
	switch {
	case looping && !earlier:
		return pick(i18n.M("server.crash.loop.none"), i18n.M("server.crash.loop.mods", "reason", mods),
			i18n.M("server.crash.loop.said", "said", said))
	case looping && known:
		return pick(i18n.M("server.crash.loop_earlier_at.none", "time", t), i18n.M("server.crash.loop_earlier_at.mods", "time", t, "reason", mods),
			i18n.M("server.crash.loop_earlier_at.said", "time", t, "said", said))
	case looping:
		return pick(i18n.M("server.crash.loop_earlier.none"), i18n.M("server.crash.loop_earlier.mods", "reason", mods),
			i18n.M("server.crash.loop_earlier.said", "said", said))
	case !earlier && known:
		return pick(i18n.M("server.crash.once_at.none", "time", t), i18n.M("server.crash.once_at.mods", "time", t, "reason", mods),
			i18n.M("server.crash.once_at.said", "time", t, "said", said))
	case !earlier:
		return pick(i18n.M("server.crash.once.none"), i18n.M("server.crash.once.mods", "reason", mods),
			i18n.M("server.crash.once.said", "said", said))
	case known:
		return pick(i18n.M("server.crash.once_earlier_at.none", "time", t), i18n.M("server.crash.once_earlier_at.mods", "time", t, "reason", mods),
			i18n.M("server.crash.once_earlier_at.said", "time", t, "said", said))
	}
	return pick(i18n.M("server.crash.once_earlier.none"), i18n.M("server.crash.once_earlier.mods", "reason", mods),
		i18n.M("server.crash.once_earlier.said", "said", said))
}

// modNames lists mod names, and how many more there are.
func modNames(shown []string, more int) i18n.Msg {
	if more > 0 {
		return i18n.M("server.list.more", "names", strings.Join(shown, ", "), "count", more)
	}
	return i18n.M("server.list.names", "names", strings.Join(shown, ", "))
}

// waitWords says a wait in whole minutes or seconds, rounding up.
func waitWords(d time.Duration) i18n.Msg {
	secs := int((d + time.Second - 1) / time.Second)
	if secs >= 60 && secs%60 == 0 {
		return i18n.M("common.duration.minutes", "count", secs/60)
	}
	return i18n.M("common.duration.seconds", "count", secs)
}

// networkMsg words a refusal of the game network (SPEC 4.4, 14.4).
func networkMsg(e *game.NetworkError) i18n.Msg {
	switch e.Kind {
	case game.NetworkUnset:
		return i18n.M("server.network.unset")
	case game.NetworkNotJoined:
		return i18n.M("server.network.not_joined", "network", e.Network)
	case game.NetworkNoRange:
		return i18n.M("server.network.no_range", "network", e.Network)
	case game.NetworkProxyOverlap:
		return i18n.M("server.network.proxy_overlap", "network", e.Network, "subnet", e.Subnet, "proxies", e.Proxies)
	case game.NetworkShared:
		var list []i18n.Msg
		for _, m := range e.Members {
			list = append(list, memberMsg(m))
		}
		msg := i18n.M("server.network.shared", "network", e.Network, "members", listMsg(list))
		if removable := e.Removable(); len(removable) > 0 {
			msg = join(msg, i18n.M("server.network.remove", "command", "docker rm "+strings.Join(removable, " ")))
		}
		if keep := e.Keep(); len(keep) > 0 {
			var cmds []i18n.Msg
			for _, n := range keep {
				cmds = append(cmds, i18n.M("server.network.command", "command", "docker network disconnect "+e.Network+" "+n))
			}
			msg = join(msg, i18n.M("server.network.disconnect", "commands", andList(cmds)))
		}
		return join(msg, i18n.M("server.network.shared_end"))
	case game.NetworkHostLookup:
		return i18n.M("server.network.host_lookup", "host", e.Host, "network", e.Network)
	case game.NetworkHostAddress:
		return i18n.M("server.network.host_address", "host", e.Host, "address", e.Address)
	case game.NetworkHostOnGame:
		return i18n.M("server.network.host_on_game", "address", e.Address, "network", e.Network, "subnet", e.Subnet)
	case game.NetworkHostOutside:
		return i18n.M("server.network.host_outside", "address", e.Address, "network", e.Network)
	case game.NetworkSelfFile:
		return i18n.M("server.network.self_file", "file", e.File)
	case game.NetworkSelfTCP:
		return i18n.M("server.network.self_tcp")
	}
	return i18n.M("server.error.network")
}

// memberMsg names a container on the game network and what it is.
func memberMsg(m game.NetworkMember) i18n.Msg {
	state := m.State
	if m.Stopped() {
		state = ""
	}
	switch {
	case m.Install && state == "":
		return i18n.M("server.network.member.install_stopped", "name", m.Name)
	case m.Install:
		return i18n.M("server.network.member.install", "name", m.Name, "state", state)
	case state == "":
		return i18n.M("server.network.member.other_stopped", "name", m.Name)
	}
	return i18n.M("server.network.member.other", "name", m.Name, "state", state)
}

// listMsg lists items with commas.
func listMsg(items []i18n.Msg) i18n.Msg {
	if len(items) == 0 {
		return i18n.Msg{}
	}
	out := items[0]
	for _, m := range items[1:] {
		out = i18n.M("server.list.comma", "first", out, "next", m)
	}
	return out
}

// andList lists items with "and" between each.
func andList(items []i18n.Msg) i18n.Msg {
	out := items[0]
	for _, m := range items[1:] {
		out = i18n.M("server.list.and", "first", out, "next", m)
	}
	return out
}

// noImageLead says Docker Hub has no image for the version.
func noImageLead(de *game.ImageDownloadError) i18n.Msg {
	return i18n.M("server.download.no_image", "image", de.Image)
}

// downloadFailure words a failed image download: the image, the cause the
// admin can act on and the next step. The daemon's own text stays in the log.
func downloadFailure(de *game.ImageDownloadError, switching bool) i18n.Msg {
	switch {
	case de.NotFound:
		return join(noImageLead(de), i18n.M("server.download.no_image_advice"))
	case switching:
		return i18n.M("server.download.failed_switch", "image", de.Image)
	}
	return i18n.M("server.download.failed", "image", de.Image)
}

// gameRefusal is refusal for server control errors.
func (s *server) gameRefusal(w http.ResponseWriter, r *http.Request, u store.User, what string, err error) (status int, msg i18n.Msg, ok bool) {
	status, msg = gameError(err)
	switch status {
	case http.StatusNotFound:
		s.notFound(w, r, u)
		return 0, i18n.Msg{}, false
	case http.StatusInternalServerError:
		s.fail(w, r, what, err)
		return 0, i18n.Msg{}, false
	}
	return status, msg, true
}

// failureText words an action's error; one the admin cannot fix is logged
// when the action ends and is not shown.
func failureText(err error) i18n.Msg {
	status, msg := gameError(err)
	if status == http.StatusInternalServerError || msg.IsZero() {
		return i18n.M("server.error.unfinished")
	}
	return msg
}

// problemMsgs words the pre-start check's problems (SPEC 7.4).
func problemMsgs(problems []world.Problem) []i18n.Msg {
	out := make([]i18n.Msg, len(problems))
	for i, p := range problems {
		out[i] = problemMsg(p)
	}
	return out
}

func problemMsg(p world.Problem) i18n.Msg {
	mods := strings.Join(p.Mods, ", ")
	names := strings.Join(p.Names, ", ")
	switch p.Kind {
	case world.ProblemGameVersion:
		return i18n.M("server.problem.game_version", "version", p.GameVersion)
	case world.ProblemNoSave:
		if p.Save == "" {
			return i18n.M("server.problem.no_save")
		}
		return i18n.M("server.problem.save_gone", "save", p.Save)
	case world.ProblemSaveUnsupported:
		return i18n.M("server.problem.save_unsupported", "save", p.Save, "save_version", p.SaveVersion)
	case world.ProblemSaveUnreadable:
		return i18n.M("server.problem.save_unreadable", "save", p.Save, "detail", saveReadDetail(p))
	case world.ProblemModListSpaceAge:
		if p.SpaceAge {
			return i18n.M("server.problem.modlist_space_age_on", "mods", names)
		}
		return i18n.M("server.problem.modlist_space_age_off", "mods", names)
	case world.ProblemSaveNewer:
		return i18n.M("server.problem.save_newer", "save", p.Save, "save_version", p.SaveVersion, "version", p.GameVersion)
	case world.ProblemSpaceAgeOff:
		return i18n.M("server.problem.space_age_off", "save", p.Save, "mods", names)
	// One mod is "it" and several "them"; the count itself is not shown.
	case world.ProblemModMissing:
		if len(p.Mods) == 1 {
			return i18n.M("server.problem.mod_missing_one", "save", p.Save, "mods", mods)
		}
		return i18n.M("server.problem.mod_missing_many", "save", p.Save, "mods", mods)
	case world.ProblemModDisabled:
		if len(p.Mods) == 1 {
			return i18n.M("server.problem.mod_disabled_one", "save", p.Save, "mods", mods)
		}
		return i18n.M("server.problem.mod_disabled_many", "save", p.Save, "mods", mods)
	case world.ProblemModGameVersion:
		return modVersionMsg(p)
	}
	// A problem this page does not know yet keeps its English.
	return i18n.M("server.problem.other", "text", p.Text)
}

// modVersionMsg words mods made for another Factorio: one sentence per
// version, then the advice.
// saveReadDetail is why a save could not be read, in the page's language
// for the reasons savefile names; any other cause is shown as the English
// it is (SPEC 22.3).
func saveReadDetail(p world.Problem) any {
	for _, r := range []struct {
		err error
		msg i18n.Msg
	}{
		{savefile.ErrNotZip, i18n.M("server.problem.save_detail.not_zip")},
		{savefile.ErrNoLevelData, i18n.M("server.problem.save_detail.no_level_data")},
		{savefile.ErrFormat, i18n.M("server.problem.save_detail.format")},
		{savefile.ErrTruncated, i18n.M("server.problem.save_detail.truncated")},
	} {
		if errors.Is(p.Cause, r.err) {
			return r.msg
		}
	}
	return i18n.Game(p.Detail)
}

func modVersionMsg(p world.Problem) i18n.Msg {
	if len(p.Groups) == 0 {
		return i18n.M("server.problem.other", "text", p.Text)
	}
	g := p.Groups[0]
	msg := i18n.M("server.problem.mod_version_first", "world", p.World, "version", p.GameVersion,
		"count", g.Count, "for", g.Version, "names", modNames(g.Shown, g.More))
	for _, g := range p.Groups[1:] {
		msg = join(msg, i18n.M("server.problem.mod_version_more", "count", g.Count, "for", g.Version, "names", modNames(g.Shown, g.More)))
	}
	if p.UpdateTo != "" {
		return join(msg, i18n.M("server.problem.mod_version_update_game", "line", p.UpdateTo))
	}
	return join(msg, i18n.M("server.problem.mod_version_update_mods", "line", p.Minor))
}

// onlyModVersions reports whether every problem is mods made for another
// Factorio, so the save is not what fails.
func onlyModVersions(check world.PreCheck) bool {
	for _, pr := range check.Problems {
		if pr.Kind != world.ProblemModGameVersion {
			return false
		}
	}
	return true
}

// precheckLeadFor opens the message for a refused start or switch. When the
// only problems are mods made for another Factorio, the save is not what
// fails, so the lead says the world cannot run instead. name is the world
// that cannot run; "" says "the world".
func precheckLeadFor(check world.PreCheck, switching bool, name string) i18n.Msg {
	if len(check.Problems) == 0 || !onlyModVersions(check) {
		if switching {
			return i18n.M("server.precheck.switch")
		}
		return i18n.M("server.precheck.start")
	}
	switch {
	case switching && name == "":
		return i18n.M("server.precheck.switch_cannot_run_unnamed")
	case switching:
		return i18n.M("server.precheck.switch_cannot_run", "world", name)
	case name == "":
		return i18n.M("server.precheck.start_cannot_run_unnamed")
	}
	return i18n.M("server.precheck.start_cannot_run", "world", name)
}

// updatePrecheckLead opens the message for an update the pre-start check
// at the new version refused. Nothing was stopped or changed.
func updatePrecheckLead(check world.PreCheck, name, version string) i18n.Msg {
	if !onlyModVersions(check) {
		return i18n.M("server.precheck.update", "world", name, "version", version)
	}
	return i18n.M("server.precheck.update_cannot_run", "world", name, "version", version)
}

// stepNames are the steps' words (DESIGN 7.6), by the game's step name.
var stepNames = map[string]i18n.Msg{
	game.StepWarn:           i18n.M("server.step.warn"),
	game.StepSave:           i18n.M("server.step.save"),
	game.StepStopContainer:  i18n.M("server.step.stop_container"),
	game.StepCheck:          i18n.M("server.step.check"),
	game.StepSnapshot:       i18n.M("server.step.snapshot_anyway"),
	game.StepImage:          i18n.M("server.step.image"),
	game.StepCreate:         i18n.M("server.step.create"),
	game.StepStartContainer: i18n.M("server.step.start_container"),
	game.StepLoad:           i18n.M("server.step.load"),
	game.StepRCON:           i18n.M("server.step.rcon"),
	game.StepRestore:        i18n.M("server.step.restore"),
	game.StepDownload:       i18n.M("server.step.download"),
	game.StepSnapshotUpdate: i18n.M("server.step.snapshot_update"),
	game.StepSetVersion:     i18n.M("server.step.set_version"),
	game.StepSnapshotBoth:   i18n.M("server.step.snapshot_both"),
	game.StepPresent:        i18n.M("server.step.present"),
}

// stepName is a step's words; a step this page does not know keeps the
// game's English name.
func stepName(name string) i18n.Msg {
	if m, ok := stepNames[name]; ok {
		return m
	}
	return i18n.M("server.step.other", "name", name)
}

// stateWord is the badge's word (DESIGN 7.5).
func stateWord(s game.State) i18n.Msg {
	switch s {
	case game.StateStopped:
		return i18n.M("server.state.stopped")
	case game.StateStarting:
		return i18n.M("server.state.starting")
	case game.StateRunning:
		return i18n.M("server.state.running")
	case game.StateStopping:
		return i18n.M("server.state.stopping")
	case game.StateCrashed:
		return i18n.M("server.state.crashed")
	case game.StateCrashLooping:
		return i18n.M("server.state.crash_looping")
	}
	return i18n.M("server.state.unknown")
}

// announcement is the sentence a live page reads out when the state
// changes (DESIGN 7.5).
func announcement(s game.State) i18n.Msg {
	switch s {
	case game.StateStopped:
		return i18n.M("server.announce.stopped")
	case game.StateStarting:
		return i18n.M("server.announce.starting")
	case game.StateRunning:
		return i18n.M("server.announce.running")
	case game.StateStopping:
		return i18n.M("server.announce.stopping")
	case game.StateCrashed:
		return i18n.M("server.announce.crashed")
	case game.StateCrashLooping:
		return i18n.M("server.announce.crash_looping")
	}
	return i18n.M("server.announce.unknown")
}

// unknownMsg says why the game's state is not known (SPEC 7.1), or is zero.
func unknownMsg(r game.UnknownReason) i18n.Msg {
	switch r.Kind {
	case game.UnknownUnreachable:
		return i18n.M("server.unknown.unreachable", "target", r.Target)
	case game.UnknownDocker:
		return i18n.M("server.unknown.docker")
	case game.UnknownInspect:
		return i18n.M("server.unknown.inspect")
	case game.UnknownPassword:
		return i18n.M("server.unknown.password", "detail", i18n.Game(r.Detail))
	case game.UnknownAsking:
		return i18n.M("server.unknown.asking")
	case game.UnknownPaused:
		return i18n.M("server.unknown.paused")
	}
	return i18n.Msg{}
}

// durationText says a duration in whole minutes, hours and days.
func durationText(d time.Duration) i18n.Msg {
	m := int(d / time.Minute)
	switch {
	case m < 1:
		return i18n.M("server.uptime.under_minute")
	case m < 60:
		return i18n.M("server.uptime.minutes", "minutes", m)
	case m < 24*60:
		return i18n.M("server.uptime.hours", "hours", m/60, "minutes", m%60)
	}
	return i18n.M("server.uptime.days", "days", m/(24*60), "hours", m/60%24)
}
