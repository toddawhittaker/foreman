package game

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 5.16: a world deleted while its map is drawn keeps no picture; the
// delete cancels the run.
func TestFactoryMapDeleteDuringDraw(t *testing.T) {
	r := newMapRig(t)
	r.newWorld("Active") // the first world is the active one, which no delete takes
	id := r.newWorld("Gone")
	r.block = make(chan struct{})
	defer close(r.block)
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	<-r.started
	if err := r.worlds.Delete(context.Background(), id, "Gone", false, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.c.ForgetFactoryMap(id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id) // the run ends without the block: it was cancelled
	if r.hasStore(id) {
		t.Error("a deleted world has a picture or a failure kept")
	}
}

// A delete that lands after the run read the save, before the drawing is
// kept, keeps nothing and records no failure.
func TestFactoryMapWorldDeletedBeforeKeep(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Gone")
	r.changeSave = func(id string) {
		if err := os.RemoveAll(r.worldDir(id)); err != nil {
			t.Error(err)
		}
	}
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	if r.hasStore(id) {
		t.Error("a deleted world has a picture or a failure kept")
	}
	r.c.factoryMaps.mu.Lock()
	_, failed := r.c.factoryMaps.failed[id]
	r.c.factoryMaps.mu.Unlock()
	if failed {
		t.Error("a delete was recorded as a failed run")
	}
}

// deletingStore deletes the world as it keeps a drawing: the delete's own
// removal of the pictures came first, so the run must remove what it wrote.
type deletingStore struct {
	*memMapStore
	delete func()
}

func (s deletingStore) Replace(d factorymap.Drawing, pngs map[string][]byte) error {
	err := s.memMapStore.Replace(d, pngs)
	s.delete()
	return err
}

func TestFactoryMapWorldDeletedDuringKeep(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Gone")
	r.c.factoryMaps.openStore = func(id string) (factoryMapStore, error) {
		return deletingStore{r.store(id), func() { _ = os.RemoveAll(r.worldDir(id)) }}, nil
	}
	if err := r.c.replaceIfWorld(id, factorymap.Drawing{}, nil); !errors.Is(err, errFactoryMapWorldGone) {
		t.Fatalf("err %v, want world gone", err)
	}
	if r.hasStore(id) {
		t.Error("the picture written as the world was deleted is still kept")
	}
}

// realStoreRig is a map rig on the real store, whose fake drawing makes real
// PNGs.
func realStoreRig(t *testing.T) *mapRig {
	t.Helper()
	r := &mapRig{rig: newRig(t, rigOptions{noRun: true}), stores: map[string]*memMapStore{}, started: make(chan string, 16)}
	fm := &r.c.factoryMaps
	fm.every = time.Hour
	fm.runSave = r.runSave
	fm.drawFiles = func(*os.Root) (drawnFactoryMap, error) {
		base := pngOf(t, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
		layer := pngOf(t, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Transparent, color.White}))
		return drawnFactoryMap{
			gameVersion: "2.1.24",
			surfaces:    []factorymap.Surface{{Index: 1, Name: "nauvis", Kind: "planet", Width: 4, Height: 4, TilesPerPixel: 1}},
			pngs:        map[string][]byte{factorymap.PNGName(1, ""): base, factorymap.PNGName(1, factorymap.CategoryRail): layer},
		}, nil
	}
	r.stop = r.run(r.c)
	return r
}

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// SPEC 5.16: on the real store, a drawing is served by surface and layer, a
// later failure is shown while the picture stays, and a delete removes it.
func TestFactoryMapRealStore(t *testing.T) {
	r := realStoreRig(t)
	r.newWorld("Active") // the first world is the active one, which no delete takes
	id := r.newWorld("Real")
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	if v := r.view(id); v.State != FactoryMapDrawn || len(v.Drawing.Surfaces) != 1 {
		t.Fatalf("view %+v, want drawn", v)
	}
	store, err := r.c.FactoryMapStore(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, layer := range []factorymap.Category{"", factorymap.CategoryRail} {
		f, err := store.OpenPNG(1, layer)
		if err != nil {
			t.Fatalf("layer %q: %v", layer, err)
		}
		b, _ := io.ReadAll(f)
		_ = f.Close()
		if _, err := png.Decode(bytes.NewReader(b)); err != nil {
			t.Errorf("layer %q is no PNG: %v", layer, err)
		}
	}

	r.runErr = fmt.Errorf("run: %w", ErrSaveCopyTimeout)
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	v := r.view(id)
	if v.State != FactoryMapFailed || v.Failure != "timeout" || len(v.Drawing.Surfaces) != 1 {
		t.Errorf("view %+v, want failed with the picture kept", v)
	}
	// The failure is kept by the store, so it shows after a restart.
	r.c.factoryMaps.mu.Lock()
	delete(r.c.factoryMaps.failed, id)
	r.c.factoryMaps.mu.Unlock()
	if v := r.view(id); v.State != FactoryMapFailed || v.Failure != "timeout" {
		t.Errorf("view from the store %+v, want failed", v)
	}

	if err := r.worlds.Delete(context.Background(), id, "Real", false, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.c.ForgetFactoryMap(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.c.dataDir(), "factorymaps", id)); !os.IsNotExist(err) {
		t.Errorf("a deleted world's pictures are still there: %v", err)
	}
}

// A damaged record shows as no picture, and the loop draws it again.
func TestFactoryMapDamagedRecord(t *testing.T) {
	r := realStoreRig(t)
	id := r.newWorld("Damaged")
	r.setMode(id, world.FactoryMapAutomatic, true)
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	<-r.started
	record := filepath.Join(r.c.dataDir(), "factorymaps", id, "factorymap.json")
	if err := os.WriteFile(record, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := r.view(id); v.State != FactoryMapNever {
		t.Errorf("state %q, want never", v.State)
	}
	if !r.c.factoryMapDue(id) {
		t.Error("a damaged record is not drawn again")
	}
}
