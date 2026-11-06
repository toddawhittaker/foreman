package gamesettings

import "errors"

// Visibility is where a game announces itself. The form offers LAN and
// hidden only; public listing needs factorio.com credentials Foreman does not
// take. A raw edit may still hold public, so reading can return it.
type Visibility string

const (
	VisibilityLAN    Visibility = "lan"
	VisibilityHidden Visibility = "hidden"
	VisibilityPublic Visibility = "public"
)

// FormVisibilities are the choices the settings form offers, in order.
var FormVisibilities = []Visibility{VisibilityLAN, VisibilityHidden}

// ReadVisibility reads the visibility of a server-settings.json. ok is false
// when the file has no visibility object with boolean public and lan, because
// the game's default is then not Foreman's to guess.
func ReadVisibility(b []byte) (v Visibility, ok bool) {
	if len(b) > MaxBytes {
		return "", false
	}
	root, fe := parse(b)
	if fe != nil {
		return "", false
	}
	m := lastMember(root, "visibility")
	if m == nil || m.node.kind != kObject {
		return "", false
	}
	pub, lan := lastMember(m.node, "public"), lastMember(m.node, "lan")
	if pub == nil || lan == nil || pub.node.kind != kBool || lan.node.kind != kBool {
		return "", false
	}
	switch {
	case pub.node.str == "true":
		return VisibilityPublic, true
	case lan.node.str == "true":
		return VisibilityLAN, true
	}
	return VisibilityHidden, true
}

// SetVisibility returns b with its visibility set to v, which must be one of
// FormVisibilities. The rest of the file keeps its text. The visibility
// object is rewritten whole, so any key in it other than public and lan is
// dropped.
func SetVisibility(b []byte, v Visibility) ([]byte, error) {
	var raw []byte
	switch v {
	case VisibilityLAN:
		raw = []byte(`{"public": false, "lan": true}`)
	case VisibilityHidden:
		raw = []byte(`{"public": false, "lan": false}`)
	default:
		return nil, errors.New("visibility must be lan or hidden")
	}
	if len(b) > MaxBytes {
		return nil, errors.New("settings file too large")
	}
	root, fe := parse(b)
	if fe != nil {
		return nil, errors.New("settings file is not one JSON object")
	}
	return setMember(b, root, "visibility", raw), nil
}
