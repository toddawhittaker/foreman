package web

import "github.com/toddawhittaker/foreman/internal/i18n"

// DropZone is one upload field drawn by the "dropzone" partial (DESIGN
// 7.10). The limits it shows are hints; the server checks every upload
// again (SPEC 14.6).
type DropZone struct {
	// ID is the file input's id; Name is the form field it posts as.
	ID   string
	Name string
	// Label is the field's visible label.
	Label i18n.Msg
	// Accept is the one file ending the field takes, such as ".zip".
	Accept string
	// MaxBytes is the largest file the server takes.
	MaxBytes uint64
	// Error is the server's refusal of the last upload, if any.
	Error i18n.Msg
}
