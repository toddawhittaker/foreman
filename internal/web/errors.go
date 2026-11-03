package web

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// msgWorldChanged is shown when a world was restored or deleted while a
// file for it was uploading.
const msgWorldChanged = "This world was changed or deleted while your upload was running. Nothing was saved; upload it again."

// worldError turns an error from the world manager or an upload into a
// status and words for the admin that say what to do next. A 500 means the
// error is not the admin's to fix: the caller logs it and shows the plain
// error page, never these words.
func worldError(err error) (int, string) {
	var ue uploadError
	switch {
	case errors.As(err, &ue):
		return http.StatusUnprocessableEntity, ue.msg
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, world.ErrNoSnapshot), errors.Is(err, world.ErrNotFolder):
		return http.StatusNotFound, ""
	case errors.Is(err, world.ErrWorldChanged):
		return http.StatusConflict, msgWorldChanged
	case errors.Is(err, world.ErrNoSpace):
		return http.StatusInsufficientStorage, "Nothing was changed: " + sentence(err)
	case errors.Is(err, world.ErrDamaged):
		return http.StatusConflict, sentence(err) + " The world was not changed. Restore a different snapshot."
	case errors.Is(err, world.ErrUnsupportedFile), errors.Is(err, world.ErrRestoreNameTaken),
		errors.Is(err, world.ErrBusy), errors.Is(err, world.ErrNewerFormat),
		errors.Is(err, world.ErrActive), errors.Is(err, world.ErrSaveExists):
		return http.StatusConflict, sentence(err)
	case errors.Is(err, world.ErrSaveName), errors.Is(err, world.ErrSaveNotFound):
		return http.StatusUnprocessableEntity, sentence(err)
	case errors.Is(err, world.ErrNameEmpty), errors.Is(err, world.ErrNameTaken),
		errors.Is(err, world.ErrBadGameVersion), errors.Is(err, world.ErrNoFolder),
		errors.Is(err, world.ErrNoSaves), errors.Is(err, world.ErrWrongName):
		return http.StatusBadRequest, sentence(err)
	}
	return http.StatusInternalServerError, ""
}

// refusal maps err with worldError. When err has no words for the page it
// answers the request itself, 404 or 500, and returns ok false.
func (s *server) refusal(w http.ResponseWriter, r *http.Request, u store.User, what string, err error) (status int, msg string, ok bool) {
	status, msg = worldError(err)
	switch status {
	case http.StatusNotFound:
		s.notFound(w, r, u)
		return 0, "", false
	case http.StatusInternalServerError:
		s.fail(w, r, what, err)
		return 0, "", false
	}
	return status, msg, true
}

// sentence turns a world error into a sentence: capital first letter and
// a full stop.
func sentence(err error) string {
	msg := err.Error()
	r, n := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[n:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}

// notFoundOrFail answers an error that stops a page from being shown at
// all: 404 when the thing asked for is not there, otherwise 500.
func (s *server) notFoundOrFail(w http.ResponseWriter, r *http.Request, u store.User, what string, err error) {
	if status, _ := worldError(err); status == http.StatusNotFound {
		s.notFound(w, r, u)
		return
	}
	s.fail(w, r, what, err)
}
