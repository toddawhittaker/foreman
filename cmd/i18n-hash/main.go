// Command i18n-hash records which English each translation was made from.
// Run it through make i18n-hash after updating every language for a key.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

func main() {
	files, err := i18n.HashFiles()
	if err == nil {
		err = os.MkdirAll(i18n.HashDir, 0o755) //nolint:gosec // a source folder in the repository
	}
	for area, b := range files {
		if err != nil {
			break
		}
		err = os.WriteFile(filepath.Join(i18n.HashDir, area+".json"), b, 0o644) //nolint:gosec // a source file in the repository, read by everyone
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "i18n-hash:", err)
		os.Exit(1)
	}
}
