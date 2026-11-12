// Command design-export writes Foreman's design bundle, the component page
// split into one standalone file per group, to the folder given. Run it
// through make design-export.
package main

import (
	"fmt"
	"os"

	"github.com/toddawhittaker/foreman/internal/web"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: design-export <folder>")
		os.Exit(2)
	}
	if err := web.ExportDesign(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "design-export:", err)
		os.Exit(1)
	}
}
