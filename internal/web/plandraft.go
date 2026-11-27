package web

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/mods"
)

// The mods review page keeps its answers in a draft in memory, under a
// random token tied to the signed-in user and the world, so its links and
// redraws carry the token instead of the whole plan (SPEC 9.14, 17). Like
// the map previews (SPEC 5.6), the drafts are bounded: a few per user, a
// cap in all, and an expiry; the oldest goes first.
const (
	draftTTL         = 2 * time.Hour
	maxDraftsPerUser = 20
	maxDrafts        = 200
)

type planDraft struct {
	user    int64
	world   string
	form    planForm
	expires time.Time
}

type draftStore struct {
	mu    sync.Mutex
	now   func() time.Time
	byKey map[string]*planDraft
	order []string // tokens, least recently kept first
}

func newDraftStore(now func() time.Time) *draftStore {
	return &draftStore{now: now, byKey: map[string]*planDraft{}}
}

// get returns the draft under token when it is the user's, for this world,
// and has not expired.
func (d *draftStore) get(token string, user int64, world string) (planForm, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sweepLocked()
	e, ok := d.byKey[token]
	if !ok || e.user != user || e.world != world {
		return planForm{}, false
	}
	return e.form.clone(), true
}

// keep stores f under token when token is the user's draft for this world,
// or under a new token otherwise, and returns the token. The redraw's
// one-time notes are not kept.
func (d *draftStore) keep(token string, user int64, world string, f planForm) string {
	f = f.clone()
	f.Removed, f.Added, f.Fixed, f.Hash, f.Fingerprint, f.DropsConfirmed = "", "", "", "", "", false
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sweepLocked()
	if e, ok := d.byKey[token]; !ok || e.user != user || e.world != world {
		token = newToken()
	}
	d.dropLocked(token)
	for d.countLocked(user) >= maxDraftsPerUser {
		d.dropLocked(d.oldestLocked(user))
	}
	for len(d.order) >= maxDrafts {
		d.dropLocked(d.order[0])
	}
	d.byKey[token] = &planDraft{user: user, world: world, form: f, expires: d.now().Add(draftTTL)}
	d.order = append(d.order, token)
	return token
}

func (d *draftStore) sweepLocked() {
	now := d.now()
	for _, t := range slices.Clone(d.order) {
		if !now.Before(d.byKey[t].expires) {
			d.dropLocked(t)
		}
	}
}

func (d *draftStore) dropLocked(token string) {
	delete(d.byKey, token)
	d.order = slices.DeleteFunc(d.order, func(t string) bool { return t == token })
}

func (d *draftStore) countLocked(user int64) int {
	n := 0
	for _, e := range d.byKey {
		if e.user == user {
			n++
		}
	}
	return n
}

func (d *draftStore) oldestLocked(user int64) string {
	for _, t := range d.order {
		if d.byKey[t].user == user {
			return t
		}
	}
	return ""
}

// clone copies f's lists and maps, so a kept draft never shares them with
// a page being drawn.
func (f planForm) clone() planForm {
	f.Names, f.Optional, f.Declined, f.Kept = slices.Clone(f.Names), slices.Clone(f.Optional), slices.Clone(f.Declined), slices.Clone(f.Kept)
	f.AlsoDisable, f.AlsoRemove = slices.Clone(f.AlsoDisable), slices.Clone(f.AlsoRemove)
	f.Choices, f.Confirmed, f.Versions = maps.Clone(f.Choices), maps.Clone(f.Confirmed), maps.Clone(f.Versions)
	if f.Choices == nil {
		f.Choices = map[string]mods.ChoiceKind{}
	}
	if f.Confirmed == nil {
		f.Confirmed = map[string]bool{}
	}
	return f
}
