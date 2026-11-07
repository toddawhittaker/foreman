package web

import (
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// defaultLabel names a manual snapshot taken without a label.
const defaultLabel = "Manual snapshot"

// snapshotKind pairs a reason code with the label Foreman stores for it,
// so a snapshot Foreman named itself can be shown in the page's language.
// A label a person typed is never matched, and shows as written
// (SPEC 22.3).
type snapshotKind struct {
	reason, label string
	// text is the label in the catalogue; why says the reason alone.
	text, why i18n.Msg
}

var snapshotKinds = []snapshotKind{
	{world.ReasonManual, defaultLabel, i18n.M("worlds.snapshot.label.manual"), i18n.M("worlds.snapshot.reason.manual")},
	{world.ReasonBeforeRestore, world.LabelBeforeRestore, i18n.M("worlds.snapshot.label.before_restore"), i18n.M("worlds.snapshot.label.before_restore")},
	{world.ReasonBeforeDelete, world.LabelBeforeDelete, i18n.M("worlds.snapshot.label.before_delete"), i18n.M("worlds.snapshot.label.before_delete")},
	{world.ReasonSwitch, world.LabelBeforeSwitch, i18n.M("worlds.snapshot.label.before_switch"), i18n.M("worlds.snapshot.label.before_switch")},
	{world.ReasonGameUpdate, world.LabelBeforeUpdate, i18n.M("worlds.snapshot.label.before_update"), i18n.M("worlds.snapshot.label.before_update")},
	{world.ReasonSettings, world.LabelBeforeSettings, i18n.M("worlds.snapshot.label.before_settings"), i18n.M("worlds.snapshot.label.before_settings")},
	{world.ReasonMods, world.LabelBeforeMods, i18n.M("worlds.snapshot.label.before_mods"), i18n.M("worlds.snapshot.label.before_mods")},
	{game.ReasonStartAnyway, game.LabelStartAnyway, i18n.M("worlds.snapshot.label.before_start_anyway"), i18n.M("worlds.snapshot.label.before_start_anyway")},
}

func kindOf(s world.Snapshot) (snapshotKind, bool) {
	for _, k := range snapshotKinds {
		if k.reason == s.Reason {
			return k, true
		}
	}
	return snapshotKind{}, false
}

// snapshotLabelArg is a snapshot's label as a message argument: a Msg for
// one Foreman named, else the text as written.
func snapshotLabelArg(s world.Snapshot) any {
	if k, ok := kindOf(s); ok && k.label == s.Label {
		return k.text
	}
	return s.Label
}

// snapshotLabel is the label to show for s in loc.
func snapshotLabel(loc i18n.Locale, s world.Snapshot) string {
	if m, ok := snapshotLabelArg(s).(i18n.Msg); ok {
		return loc.Format(m)
	}
	return s.Label
}

// snapshotReason says why s was taken, or "" when its label already says
// so, which includes a label that starts with the same words, so "Manual"
// is not shown above "Manual snapshot".
func snapshotReason(loc i18n.Locale, s world.Snapshot) string {
	k, ok := kindOf(s)
	if !ok || k.label == s.Label {
		return ""
	}
	text := loc.Format(k.why)
	have, lead := strings.Fields(strings.ToLower(s.Label)), strings.Fields(strings.ToLower(text))
	if len(have) >= len(lead) && slices.Equal(have[:len(lead)], lead) {
		return ""
	}
	return text
}
