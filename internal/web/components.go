package web

import (
	"fmt"
	"slices"
)

// emptyThings are the things an empty state can draw (DESIGN 7.3), each with
// its icon in messages.css.
var emptyThings = []string{"snapshots", "worlds", "mods", "saves", "deleted", "players", "console", "settings", "notify", "chat"}

// Empty is the empty partial's input: what is missing, the sentence that names
// the next step (a string or HTML), and an id for scripts, if any.
type Empty struct {
	Thing string
	Text  any
	ID    string
}

// emptyState makes the empty partial's input:
// {{template "empty" (empty "worlds" (t "worlds.empty"))}}, with an optional id last.
func emptyState(thing string, text any, id ...string) (Empty, error) {
	if !slices.Contains(emptyThings, thing) {
		return Empty{}, fmt.Errorf("empty state: no drawing for %q", thing)
	}
	if len(id) > 1 {
		return Empty{}, fmt.Errorf("empty state: one id at most, got %d", len(id))
	}
	e := Empty{Thing: thing, Text: text}
	if len(id) == 1 {
		e.ID = id[0]
	}
	return e, nil
}

// workingWords is the working partial's words. The bar alone says nothing,
// and under reduced motion it stands still, so the words are required
// (DESIGN 7.6).
func workingWords(words any) (any, error) {
	if words == nil || fmt.Sprint(words) == "" {
		return nil, fmt.Errorf("working bar: no words")
	}
	return words, nil
}

// FormFoot is the form-foot partial's input. Submit is the primary button's
// words; Cancel, when set, is where Cancel leads. Note leads the bar; with
// Status it is a live region a script fills, so it may start empty.
type FormFoot struct {
	Note       any
	NoteID     string
	Status     bool
	Cancel     string
	CancelText string
	Submit     string
	SubmitID   string
}

// formFoot makes the form-foot partial's input from name and value pairs:
// {{template "form-foot" (formFoot "submit" (t "x.save") "cancel" .BackPath "cancelText" (t "worlds.cancel"))}}.
func formFoot(pairs ...any) (FormFoot, error) {
	var f FormFoot
	if len(pairs)%2 != 0 {
		return f, fmt.Errorf("form foot: %d arguments, want name and value pairs", len(pairs))
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		name, _ := pairs[i].(string)
		value := pairs[i+1]
		var ok bool
		switch name {
		case "note":
			f.Note, ok = value, true
		case "noteID":
			f.NoteID, ok = value.(string)
		case "status":
			f.Status, ok = value.(bool)
		case "cancel":
			f.Cancel, ok = value.(string)
		case "cancelText":
			f.CancelText, ok = value.(string)
		case "submit":
			f.Submit, ok = value.(string)
		case "submitID":
			f.SubmitID, ok = value.(string)
		default:
			return f, fmt.Errorf("form foot: no field %v", pairs[i])
		}
		if !ok {
			return f, fmt.Errorf("form foot: %s is a %T", name, value)
		}
	}
	if f.Submit == "" {
		return f, fmt.Errorf("form foot: no submit words")
	}
	if f.Status && f.NoteID == "" {
		return f, fmt.Errorf("form foot: a status note needs a noteID for the script that fills it")
	}
	if (f.Cancel == "") != (f.CancelText == "") {
		return f, fmt.Errorf("form foot: cancel and cancelText go together")
	}
	return f, nil
}
