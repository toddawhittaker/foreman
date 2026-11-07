package web

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// MaxSaveUpload is the size limit of a save upload (SPEC 14.6).
const MaxSaveUpload = 1 << 30

// maxZipRatio caps a zip's stated unpacked size at this many times its own
// size; every caller of checkZip passes that cap.
const maxZipRatio = 50

// maxUploads is how many uploads may run at once across the whole server;
// each holds a disk reserve and a temp file (SPEC 14.6).
const maxUploads = 2

// guardUpload wraps an upload handler. Before the body is read it refuses a
// request that does not say how big it is (411) and one that arrives while
// maxUploads others run (429). The place is given back on every way out.
func (s *server) guardUpload(h func(http.ResponseWriter, *http.Request, store.User)) func(http.ResponseWriter, *http.Request, store.User) {
	return func(w http.ResponseWriter, r *http.Request, u store.User) {
		if r.ContentLength < 0 {
			s.page(w, r, http.StatusLengthRequired, u.Username, ErrorPage{Status: http.StatusLengthRequired,
				Detail: i18n.M("worlds.upload.no_length")})
			return
		}
		select {
		case s.uploadSlots <- struct{}{}:
			defer func() { <-s.uploadSlots }()
		default:
			s.page(w, r, http.StatusTooManyRequests, u.Username, ErrorPage{Status: http.StatusTooManyRequests,
				Detail: i18n.M("worlds.upload.busy")})
			return
		}
		h(w, r, u)
	}
}

// uploadError carries words safe to show the admin.
type uploadError struct{ msg i18n.Msg }

func (e uploadError) Error() string { return i18n.English.Format(e.msg) }

// tooLarge says the limit as the page's hint does: the mod limits in whole
// megabytes, the save limit in bytes' decimal units.
func tooLarge(limit int64) uploadError {
	switch limit {
	case MaxModUpload:
		return uploadError{i18n.M("worlds.upload.too_large", "limit", megabytes(modUploadMB))}
	case MaxSettingsUpload:
		return uploadError{i18n.M("worlds.upload.too_large", "limit", megabytes(settingsUploadMB))}
	}
	return uploadError{i18n.M("worlds.upload.too_large", "limit", i18n.Size(limit))} //nolint:gosec // limit is a positive constant
}

// uploaded is a file received into a temp file beside its destination.
// The caller checks it, then commits it with world.Commit or removes it.
type uploaded struct {
	Temp     string // relative to the root it was written in
	FileName string // the base name the browser sent
	Size     int64
}

// receiveFile streams the file in form field into a temp file in dir inside
// root, without holding it in memory. spaceDir is a real path on the same
// filesystem: the upload holds a world.Reserve for its declared length
// until it is on disk (SPEC 6.11). On
// any failure the temp file is removed and nothing else has changed.
func receiveFile(r *http.Request, root *os.Root, dir, field, spaceDir string, limit int64) (uploaded, error) {
	return receiveFileWithFields(r, root, dir, field, spaceDir, limit, nil)
}

// maxFieldBytes caps each text field read beside an upload.
const maxFieldBytes = 1 << 10

// receiveFileWithFields is receiveFile for a form that also has text
// fields: each part named in values, sent before the file, is read into
// values. Text fields sent after the file are not read.
func receiveFileWithFields(r *http.Request, root *os.Root, dir, field, spaceDir string, limit int64, values map[string]string) (uploaded, error) {
	create := func() (*os.File, string, error) { return world.CreateTemp(root, dir) }
	return receiveInto(r, root, create, field, spaceDir, limit, values)
}

// receiveInto is receiveFileWithFields with the temp file made by create,
// which returns it open with its name relative to root.
func receiveInto(r *http.Request, root *os.Root, create func() (*os.File, string, error), field, spaceDir string, limit int64, values map[string]string) (uploaded, error) {
	if r.ContentLength > limit {
		return uploaded{}, tooLarge(limit)
	}
	// A body of unknown length still needs the reserve free; if it then
	// fills the disk, bodyError says so and the temp file goes.
	release, err := world.Reserve(spaceDir, uint64(max(r.ContentLength, 0))) //nolint:gosec // not negative
	if err != nil {
		return uploaded{}, err
	}
	defer release()
	mr, err := r.MultipartReader()
	if err != nil {
		return uploaded{}, uploadError{i18n.M("worlds.upload.choose_file")}
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return uploaded{}, uploadError{i18n.M("worlds.upload.choose_file")}
		}
		if err != nil {
			return uploaded{}, partError(err, limit)
		}
		if _, want := values[part.FormName()]; want && part.FileName() == "" {
			b, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
			if err != nil {
				return uploaded{}, bodyError(err, limit)
			}
			if len(b) > maxFieldBytes {
				return uploaded{}, uploadError{i18n.M("worlds.upload.field_long")}
			}
			values[part.FormName()] = string(b)
			continue
		}
		if part.FormName() != field || part.FileName() == "" {
			continue
		}
		// Browsers on Windows may send a full path with backslashes.
		up := uploaded{FileName: path.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))}
		f, temp, err := create()
		if err != nil {
			return uploaded{}, err
		}
		up.Temp = temp
		up.Size, err = io.Copy(f, part)
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = root.Remove(temp)
			return uploaded{}, bodyError(err, limit)
		}
		return up, nil
	}
}

// bodyError turns a failure while reading or writing the body into words
// for the admin where it has a cause they can act on.
func bodyError(err error, limit int64) error {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return tooLarge(limit)
	case errors.Is(err, syscall.ENOSPC):
		return uploadError{i18n.M("worlds.upload.disk_full")}
	}
	return err
}

// partError turns a failure to read the next part's header into words.
// Reading a header writes nothing, so every cause is the request's: a
// malformed form, or a body cut short or too large.
func partError(err error, limit int64) error {
	if err = bodyError(err, limit); errors.As(err, new(uploadError)) {
		return err
	}
	return uploadError{i18n.M("worlds.upload.unreadable")}
}

// checkZip reads only a zip's directory, never unpacking it, and refuses
// one that is not a zip, has an entry name that would leave its folder, or
// states a total unpacked size over maxUnpacked.
func checkZip(root *os.Root, name string, maxUnpacked uint64) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return uploadError{i18n.M("worlds.upload.not_zip")}
	}
	var total uint64
	for _, e := range zr.File {
		if !filepath.IsLocal(e.Name) {
			return uploadError{i18n.M("worlds.upload.unsafe_entry")}
		}
		total += e.UncompressedSize64
		if total > maxUnpacked || total < e.UncompressedSize64 {
			return uploadError{i18n.M("worlds.upload.unpack_limit", "limit", i18n.Size(maxUnpacked))}
		}
	}
	return nil
}
