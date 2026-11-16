package world

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
)

// MaxFavoriteUpload is the size limit of an imported favorite file.
const MaxFavoriteUpload = 8 << 20

// FavoriteFormatFile names the file in a favorite file that says its
// format, so a later Foreman can tell which layout it reads.
const FavoriteFormatFile = "foreman-favorite.json"

// favoriteFileFormat is the format version of an exported favorite file.
const favoriteFileFormat = 1

// ErrFavoriteFileBad refuses an imported file. It never names what is in it.
var ErrFavoriteFileBad = errors.New("this is not a favorite file Foreman can read, or it is damaged")

// favoriteFileFormatDoc is the content of FavoriteFormatFile.
type favoriteFileFormatDoc struct {
	FormatVersion int `json:"format_version"`
}

// favoriteEntryLimits is every entry a favorite file may hold, flat with no
// folders, and the most each may unpack to.
var favoriteEntryLimits = map[string]int64{
	FavoriteFormatFile: 1 << 10,
	FavoriteFile:       maxFavoriteJSON,
	MapGenSettingsFile: maxConfigFile,
	MapSettingsFile:    maxConfigFile,
	ServerSettingsFile: maxConfigFile,
	ModSettingsFile:    MaxModSettings,
}

// Export returns favorite id as a zip of its folder plus the format file.
// mod-settings.dat goes with it as it is; a mod may keep a token there, so
// the page that offers this says so.
func (f *Favorites) Export(id string) (Favorite, []byte, error) {
	fav, files, err := f.Get(id)
	if err != nil {
		return Favorite{}, nil, err
	}
	desc, err := json.MarshalIndent(fav, "", "  ")
	if err != nil {
		return Favorite{}, nil, err
	}
	format, err := json.Marshal(favoriteFileFormatDoc{FormatVersion: favoriteFileFormat})
	if err != nil {
		return Favorite{}, nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct {
		name string
		data []byte
	}{
		{FavoriteFormatFile, format},
		{FavoriteFile, desc},
		{MapGenSettingsFile, files.MapGenSettings},
		{MapSettingsFile, files.MapSettings},
		{ServerSettingsFile, files.ServerSettings},
		{ModSettingsFile, files.ModSettings},
	} {
		if len(e.data) == 0 {
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: fav.CreatedAt})
		if err != nil {
			return Favorite{}, nil, err
		}
		if _, err := w.Write(e.data); err != nil {
			return Favorite{}, nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return Favorite{}, nil, err
	}
	return fav, buf.Bytes(), nil
}

// Import checks a favorite file in full, then saves it as a new favorite
// and returns its id. Nothing is written until every check passes. The
// file's mods and game version need not be available here: the review
// after Generate shows those as conflicts. A name already in use is
// refused with ErrFavoriteNameTaken rather than renamed, so the person
// sees which favorite they already have. The imported favorite is dated
// now and names no source world.
func (f *Favorites) Import(ctx context.Context, data []byte) (string, error) {
	fav, files, err := readFavoriteZip(data)
	if err != nil {
		return "", err
	}
	// The source names a world on another Foreman, and the date is when the
	// favorite arrived here.
	fav.SourceWorld = ""
	fav.CreatedAt = f.now().UTC()
	return f.save(ctx, fav, files)
}

// readFavoriteZip reads and checks every part of a favorite file. Its
// contents are untrusted: any entry outside the fixed set, a folder, a
// link, an unknown field or a bad value refuses the whole file.
func readFavoriteZip(data []byte) (Favorite, FavoriteFiles, error) {
	if len(data) > MaxFavoriteUpload {
		return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
	}
	got := map[string][]byte{}
	for _, e := range zr.File {
		limit, ok := favoriteEntryLimits[e.Name]
		if !ok || !e.Mode().IsRegular() {
			return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
		}
		if _, dup := got[e.Name]; dup || e.UncompressedSize64 > uint64(limit) { //nolint:gosec // limit is a positive constant
			return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
		}
		b, err := readZipEntry(e, limit)
		if err != nil {
			return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
		}
		got[e.Name] = b
	}

	var format favoriteFileFormatDoc
	if strictJSON(got[FavoriteFormatFile], &format) != nil || format.FormatVersion != favoriteFileFormat {
		return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
	}
	fav, err := decodeFavorite(got[FavoriteFile], true)
	if err != nil {
		return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
	}
	files, err := checkFavoriteFiles(FavoriteFiles{
		MapGenSettings: got[MapGenSettingsFile],
		MapSettings:    got[MapSettingsFile],
		ServerSettings: got[ServerSettingsFile],
		ModSettings:    got[ModSettingsFile],
	})
	if err != nil {
		return Favorite{}, FavoriteFiles{}, ErrFavoriteFileBad
	}
	return fav, files, nil
}

// readZipEntry unpacks one entry, refusing it once it passes limit whatever
// size its header claims.
func readZipEntry(e *zip.File, limit int64) ([]byte, error) {
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrFavoriteFileBad
	}
	return b, nil
}

// strictJSON decodes exactly one JSON value into v, refusing unknown fields.
func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return ErrFavoriteFileBad
	}
	return nil
}
