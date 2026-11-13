package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
)

// PathDesign is the component page: every component in every state, in
// both themes, drawn with the real stylesheet (docs/DESIGN.md, section 7).
// It is for whoever works on the UI, so no menu links to it.
const PathDesign = "/design"

// designGroup is one section of the component page, and one file of the
// design export.
type designGroup struct {
	Key     string
	Heading i18n.Msg
	// Card is the export's card group, the word after @dsCard.
	Card string
}

// designGroups are the page's sections in order.
var designGroups = []designGroup{
	{"buttons", i18n.M("common.design.buttons"), "Components"},
	{"fields", i18n.M("common.design.fields"), "Components"},
	{"messages", i18n.M("common.design.messages"), "Components"},
	{"badges", i18n.M("common.design.badges"), "Components"},
	{"cards", i18n.M("common.design.cards"), "Components"},
	{"navigation", i18n.M("common.design.navigation"), "Components"},
	{"tables", i18n.M("common.design.tables"), "Components"},
	{"icons", i18n.M("common.design.icons"), "Icons"},
	{"colours", i18n.M("common.design.colours"), "Colors"},
	{"type", i18n.M("common.design.type"), "Type"},
	{"spacing", i18n.M("common.design.spacing"), "Spacing"},
}

// designStates are the server states a badge and the status card can show
// (DESIGN 7.5).
var designStates = []game.State{
	game.StateStopped, game.StateStarting, game.StateRunning, game.StateStopping,
	game.StateCrashed, game.StateCrashLooping, game.StateUnknown,
}

// DesignPage is the component page's view model. Its tokens and icons are
// read from foreman.css, so the page never lists one the stylesheet lacks.
type DesignPage struct {
	Groups []designGroup
	Tokens []designToken
	Icons  []string
}

func (DesignPage) templateName() string { return "design" }

// newDesignPage reads the tokens and icons from the shipped stylesheet.
func newDesignPage() (DesignPage, error) {
	css, err := staticFS.ReadFile("static/foreman.css")
	if err != nil {
		return DesignPage{}, err
	}
	return DesignPage{Groups: designGroups, Tokens: designTokens(string(css)), Icons: iconNames(string(css))}, nil
}

// TokensWith are the tokens whose names start with prefix, such as "--color-".
func (p DesignPage) TokensWith(prefix string) []designToken {
	var out []designToken
	for _, t := range p.Tokens {
		if strings.HasPrefix(t.Name, prefix) {
			out = append(out, t)
		}
	}
	return out
}

// States are every server state, for the badges and the status cards.
func (DesignPage) States() []LiveState {
	out := make([]LiveState, len(designStates))
	for i, st := range designStates {
		out[i] = LiveState{State: st, Have: true}
	}
	return out
}

// Mod is a sample mod's name, for the mod row.
func (DesignPage) Mod() string { return "flow-control" }

// Loads are sample CPU figures, one per state, so the plates show the
// meter at rest, above 80% and above 95%.
func (DesignPage) Loads() []float64 {
	out := make([]float64, len(designStates))
	for i := range out {
		out[i] = 24.5
	}
	out[1], out[3] = 86.0, 97.5
	return out
}

// WorldTabs is a world's sub-navigation, on a world that does not exist.
func (DesignPage) WorldTabs() WorldTabs {
	return WorldTabs{Path: PathWorlds + "/example", Current: "saves", Settings: true}
}

// designPlayer is a row of the sample table.
type designPlayer struct {
	Name string
	For  time.Duration
}

// Players are the sample table's rows: names as a person would type them,
// which no page translates.
func (DesignPage) Players() []designPlayer {
	return []designPlayer{{"engineer", 2*time.Hour + 5*time.Minute}, {"Ada", 47 * time.Minute}, {"builder_42", 9 * time.Second}}
}

// Other is true for a token the scales and swatches do not show.
func (DesignPage) Other(name string) bool {
	for _, prefix := range []string{"--color-", "--text-", "--font-", "--space-", "--radius", "--shadow-"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

// Section is one group as the page shows it.
func (p DesignPage) Section(g designGroup) designSection {
	return designSection{Page: p, Group: g}
}

// designSection is one group, shown once in each theme.
type designSection struct {
	Page  DesignPage
	Group designGroup
}

// Pane is the section drawn in one theme. Ids in a pane start with its
// theme, so the two copies never share one.
func (s designSection) Pane(theme string) designPane {
	return designPane{Page: s.Page, Group: s.Group.Key, Theme: theme}
}

type designPane struct {
	Page  DesignPage
	Group string
	Theme string
}

// Value is a token's value in this pane's theme.
func (p designPane) Value(t designToken) string {
	if p.Theme == "light" {
		return t.Light
	}
	return t.Dark
}

// Tip is the sample toggletip (DESIGN 7.7).
func (p designPane) Tip() Toggletip {
	return Toggletip{ID: p.Theme + "-seed", About: i18n.M("mapgen.seed.label"), Text: i18n.M("mapgen.seed.hint")}
}

func (s *server) designRoutes() {
	s.mux.HandleFunc("GET "+PathDesign, s.requireUser(s.handleDesign))
}

func (s *server) handleDesign(w http.ResponseWriter, r *http.Request, u store.User) {
	p, err := newDesignPage()
	if err != nil {
		s.fail(w, r, "read the stylesheet", err)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}
