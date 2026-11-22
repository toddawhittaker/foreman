package web

// Names the world file export and import share (SPEC 5.13, 5.14). The audit
// actions "world-export" and "world-import" (12.9) are not here: a test needs
// words for each in auditActionWords, so task T3 adds both constants with
// their words. The
// element ids, for the UI and e2e tasks: act-export-world, export-saves (the
// radios export-saves-all and export-saves-loaded), export-warning,
// world-file (the drop zone's input), act-upload-world-file, world-file-name,
// world-file-download, act-import-world-file and world-file-password-warning.
const (
	// patternWorldExport is the export form's post: a post, because it does
	// work and writes an audit entry.
	patternWorldExport = "POST /worlds/{id}/export"
	// PathWorldFileImport takes the upload of a world file, and
	// PathWorldFileConfirm the confirmed preview.
	PathWorldFileImport  = "/import/world-file"
	PathWorldFileConfirm = "/import/world-file/confirm"
)
