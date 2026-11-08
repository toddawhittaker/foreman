package world

import (
	"os"
	"sync"
	"testing"
)

// A reader racing an atomic replace must see the old file or the new one,
// never an error (SPEC 5, 14.5): a rename between Lstat and open is not a swap.
func TestReadSmallFileDuringAtomicReplace(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := WriteFileAtomic(root, "f.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := WriteFileAtomic(root, "f.json", []byte("{}")); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for range 5000 {
		if _, err := ReadSmallFile(root, "f.json", 1024); err != nil {
			t.Errorf("read during replace: %v", err)
			break
		}
	}
	close(stop)
	wg.Wait()
}
