package web

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/toddawhittaker/foreman/internal/world"
)

// Upload size limits (SPEC 14.6).
const (
	MaxSaveUpload  = 1 << 30
	MaxOtherUpload = 200 << 20
)

// uploadError carries words safe to show the admin.
type uploadError struct{ msg string }

func (e uploadError) Error() string { return e.msg }

// uploaded is a file received into a temp file beside its destination.
// The caller checks it, then commits it with world.Commit or removes it.
type uploaded struct {
	Temp     string // relative to the root it was written in
	FileName string // the base name the browser sent
	Size     int64
}

// receiveFile streams the file in form field into a temp file in dir inside
// root, without holding it in memory. spaceDir is a real path on the same
// filesystem, used to refuse up front an upload larger than the free space.
// On any failure the temp file is removed and nothing else has changed.
func receiveFile(r *http.Request, root *os.Root, dir, field, spaceDir string, limit int64) (uploaded, error) {
	return receiveFileWithFields(r, root, dir, field, spaceDir, limit, nil)
}

// maxFieldBytes caps each text field read beside an upload.
const maxFieldBytes = 1 << 10

// receiveFileWithFields is receiveFile for a form that also has text
// fields: each part named in values, sent before the file, is read into
// values. Text fields sent after the file are not read.
func receiveFileWithFields(r *http.Request, root *os.Root, dir, field, spaceDir string, limit int64, values map[string]string) (uploaded, error) {
	if r.ContentLength > limit {
		return uploaded{}, uploadError{fmt.Sprintf("The file is larger than the limit of %s.", world.FormatBytes(uint64(limit)))} //nolint:gosec // limit is a positive constant
	}
	if r.ContentLength > 0 {
		free, err := world.FreeBytes(spaceDir)
		if err != nil {
			return uploaded{}, err
		}
		if uint64(r.ContentLength) > free {
			return uploaded{}, uploadError{fmt.Sprintf("There is not enough free disk space for this file: it is %s and only %s is free.",
				world.FormatBytes(uint64(r.ContentLength)), world.FormatBytes(free))}
		}
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return uploaded{}, uploadError{"Choose a file to upload."}
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return uploaded{}, uploadError{"Choose a file to upload."}
		}
		if err != nil {
			return uploaded{}, bodyError(err, limit)
		}
		if _, want := values[part.FormName()]; want && part.FileName() == "" {
			b, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
			if err != nil {
				return uploaded{}, bodyError(err, limit)
			}
			if len(b) > maxFieldBytes {
				return uploaded{}, uploadError{"A form field is too long."}
			}
			values[part.FormName()] = string(b)
			continue
		}
		if part.FormName() != field || part.FileName() == "" {
			continue
		}
		// Browsers on Windows may send a full path with backslashes.
		up := uploaded{FileName: path.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))}
		f, temp, err := world.CreateTemp(root, dir)
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
		return uploadError{fmt.Sprintf("The file is larger than the limit of %s.", world.FormatBytes(uint64(limit)))} //nolint:gosec // limit is a positive constant
	case errors.Is(err, syscall.ENOSPC):
		return uploadError{"The disk filled up during the upload, so it was stopped and nothing was changed. Free some space and try again."}
	}
	return err
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
		return uploadError{"This file is not a zip file."}
	}
	var total uint64
	for _, e := range zr.File {
		if !filepath.IsLocal(e.Name) {
			return uploadError{"This zip file holds an entry with an unsafe name, so it was refused."}
		}
		total += e.UncompressedSize64
		if total > maxUnpacked || total < e.UncompressedSize64 {
			return uploadError{fmt.Sprintf("This zip file would unpack to more than %s, so it was refused.", world.FormatBytes(maxUnpacked))}
		}
	}
	return nil
}
