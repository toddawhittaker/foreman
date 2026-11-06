// Command i18n-hash records which English each translation was made from.
// Run it through make i18n-hash after updating every language for a key.
package main

import (
	"fmt"
	"os"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

func main() {
	b, err := i18n.HashJSON()
	if err == nil {
		err = os.WriteFile(i18n.HashFile, b, 0o644) //nolint:gosec // a source file in the repository, read by everyone
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "i18n-hash:", err)
		os.Exit(1)
	}
}
