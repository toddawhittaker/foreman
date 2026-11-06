package web

import (
	"errors"
	"io/fs"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// worldError turns an error from the world manager or an upload into a
// status and words for the admin that say what to do next. A 500 means the
// error is not the admin's to fix: the caller logs it and shows the plain
// error page, never these words. A 404 has no words either.
func worldError(err error) (int, i18n.Msg) {
	if ue, ok := errors.AsType[uploadError](err); ok {
		return http.StatusUnprocessableEntity, ue.msg
	}
	if ns, ok := errors.AsType[*world.NoSpaceError](err); ok {
		return http.StatusInsufficientStorage, i18n.M("worlds.error.nospace",
			"need", i18n.Size(ns.Need), "reserve", i18n.Size(ns.Reserve), "free", i18n.Size(ns.Free))
	}
	if de, ok := errors.AsType[*world.DamagedError](err); ok {
		return http.StatusConflict, damagedMsg(de)
	}
	if ue, ok := errors.AsType[*world.UnsupportedFileError](err); ok {
		return http.StatusConflict, i18n.M("worlds.error.unsupported_file", "path", ue.Path)
	}
	if ce, ok := errors.AsType[world.SaveClashError](err); ok {
		return http.StatusBadRequest, clashMsg(ce)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, world.ErrNoSnapshot), errors.Is(err, world.ErrNotFolder):
		return http.StatusNotFound, i18n.Msg{}
	case errors.Is(err, world.ErrWorldChanged):
		return http.StatusConflict, i18n.M("worlds.error.world_changed")
	case errors.Is(err, world.ErrRestoreNameTaken):
		return http.StatusConflict, i18n.M("worlds.error.restore_name_taken")
	case errors.Is(err, world.ErrBusy):
		return http.StatusConflict, msgBusy()
	case errors.Is(err, world.ErrNewerFormat):
		return http.StatusConflict, i18n.M("worlds.error.newer_format")
	case errors.Is(err, world.ErrActive):
		return http.StatusConflict, i18n.M("worlds.error.active")
	case errors.Is(err, world.ErrSaveExists):
		return http.StatusConflict, i18n.M("worlds.error.save_exists")
	case errors.Is(err, world.ErrUnreadable):
		return http.StatusConflict, i18n.M("worlds.error.unreadable")
	case errors.Is(err, world.ErrSaveName):
		return http.StatusUnprocessableEntity, i18n.M("worlds.error.save_name")
	case errors.Is(err, world.ErrSaveNotFound):
		return http.StatusUnprocessableEntity, i18n.M("worlds.error.save_not_found")
	case errors.Is(err, world.ErrNameEmpty):
		return http.StatusBadRequest, i18n.M("worlds.error.name_empty")
	case errors.Is(err, world.ErrNameTaken):
		return http.StatusBadRequest, i18n.M("worlds.error.name_taken")
	case errors.Is(err, world.ErrBadGameVersion):
		return http.StatusBadRequest, msgBadGameVersion()
	case errors.Is(err, world.ErrNoFolder):
		return http.StatusBadRequest, msgNoFolder()
	case errors.Is(err, world.ErrNoSaves):
		return http.StatusBadRequest, msgNoSaves()
	case errors.Is(err, world.ErrWrongName):
		return http.StatusBadRequest, i18n.M("worlds.error.wrong_name")
	}
	return http.StatusInternalServerError, i18n.Msg{}
}

// The messages more than one place shows. Each is a function so a caller
// cannot change a shared value.
func msgBusy() i18n.Msg             { return i18n.M("worlds.error.busy") }
func msgOperationRunning() i18n.Msg { return i18n.M("worlds.error.operation_running") }
func msgBadGameVersion() i18n.Msg   { return i18n.M("worlds.error.bad_game_version") }
func msgNoFolder() i18n.Msg         { return i18n.M("worlds.error.no_folder") }
func msgNoSaves() i18n.Msg          { return i18n.M("worlds.error.no_saves") }
func msgSpaceAgeOld() i18n.Msg      { return i18n.M("worlds.error.space_age_old") }
func msgNameEmpty() i18n.Msg        { return i18n.M("worlds.error.name_empty") }

func clashMsg(e world.SaveClashError) i18n.Msg {
	return i18n.M("worlds.error.save_clash", "name", e.Name)
}

func damagedMsg(e *world.DamagedError) i18n.Msg {
	switch e.Kind {
	case world.DamageBadPath:
		return i18n.M("worlds.error.damaged.bad_path", "path", e.Path)
	case world.DamageMissing:
		return i18n.M("worlds.error.damaged.missing", "path", e.Path)
	case world.DamageChangedSince:
		return i18n.M("worlds.error.damaged.changed_since", "path", e.Path)
	}
	return i18n.M("worlds.error.damaged.changed", "path", e.Path)
}

// refusal maps err with worldError. When err has no words for the page it
// answers the request itself, 404 or 500, and returns ok false.
func (s *server) refusal(w http.ResponseWriter, r *http.Request, u store.User, what string, err error) (status int, msg i18n.Msg, ok bool) {
	status, msg = worldError(err)
	switch status {
	case http.StatusNotFound:
		s.notFound(w, r, u)
		return 0, i18n.Msg{}, false
	case http.StatusInternalServerError:
		s.fail(w, r, what, err)
		return 0, i18n.Msg{}, false
	}
	return status, msg, true
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
