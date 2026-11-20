package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// The confirm step for lowering snapshot retention (SPEC 17.5, 6.6): only a
// save whose new figure would remove automatic snapshots asks first.

// The hidden fields that carry the count the confirm page showed back to
// the save, so a count that moved since is shown again.
const (
	trimSnapshotsField = "trim_snapshots"
	trimWorldsField    = "trim_worlds"
)

// GeneralHidden is one submitted value carried through the confirm form.
type GeneralHidden struct{ Name, Value string }

// GeneralChange is one setting the save changes. From and To are numbers,
// or words for a switch.
type GeneralChange struct {
	Label    i18n.Msg
	From, To any
}

// GeneralConfirmPage asks before a save that lowers retention removes
// snapshots (DESIGN 7.4).
type GeneralConfirmPage struct {
	Hidden  []GeneralHidden
	Changes []GeneralChange
	Trim    trimCount
}

func (GeneralConfirmPage) templateName() string { return "generalconfirm" }
func (GeneralConfirmPage) navItem() string      { return generalPath() }

// Path is where the confirm form posts, and CancelPath where Cancel goes.
func (GeneralConfirmPage) Path() string       { return generalPath() }
func (GeneralConfirmPage) CancelPath() string { return generalPath() }

// generalConfirmPage returns the confirm page when the retention in want
// would remove snapshots and the request does not already carry the count
// that is true now. It decides on the real count, not on whether the figure
// is lower, since a raise can still remove snapshots (a world that has not
// yet taken an automatic snapshot since an earlier lowering). Archived worlds
// count like any other, as they take automatic snapshots too. needed is false when nothing would go, and the save
// then goes ahead at once.
func (s *server) generalConfirmPage(ctx context.Context, r *http.Request, want map[string]int) (_ GeneralConfirmPage, needed bool, _ error) {
	n := want[settingSnapshotRetention.Key]
	snapshots, worlds, err := s.snapshotTrim(ctx, n)
	if err != nil || snapshots == 0 {
		return GeneralConfirmPage{}, false, err
	}
	if r.PostFormValue(trimSnapshotsField) == strconv.Itoa(snapshots) && r.PostFormValue(trimWorldsField) == strconv.Itoa(worlds) {
		return GeneralConfirmPage{}, false, nil
	}
	p := GeneralConfirmPage{Trim: trimCount{Snapshots: snapshots, Worlds: worlds}}
	current := s.generalValues(ctx)
	for _, d := range generalSettings {
		p.Hidden = append(p.Hidden, GeneralHidden{Name: d.Key, Value: strconv.Itoa(want[d.Key])})
		if want[d.Key] != current[d.Key] {
			p.Changes = append(p.Changes, GeneralChange{Label: i18n.M(d.Label), From: current[d.Key], To: want[d.Key]})
		}
	}
	// The switch rides along as posted, so confirming keeps what the admin ticked.
	wantCheck := r.PostFormValue(settingUpdateCheck.Key) == "on"
	if wantCheck {
		p.Hidden = append(p.Hidden, GeneralHidden{Name: settingUpdateCheck.Key, Value: "on"})
	}
	if was := s.readBool(ctx, settingUpdateCheck); was != wantCheck {
		p.Changes = append(p.Changes, GeneralChange{Label: i18n.M(settingUpdateCheck.Label), From: switchWord(was), To: switchWord(wantCheck)})
	}
	p.Hidden = append(p.Hidden,
		GeneralHidden{Name: trimSnapshotsField, Value: strconv.Itoa(snapshots)},
		GeneralHidden{Name: trimWorldsField, Value: strconv.Itoa(worlds)})
	return p, true, nil
}
