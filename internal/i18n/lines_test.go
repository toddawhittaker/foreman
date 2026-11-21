package i18n

import (
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// Some messages list the supported game lines in words, because a message
// cannot be built from a list in every language. This test fails when a line
// is added to mapgen.Lines before these messages name it.
func TestMessagesNameEverySupportedGameLine(t *testing.T) {
	keys := []string{
		"server.problem.game_version",
		"server.problem.save_unsupported",
		"server.error.version_unsupported",
		"worlds.error.bad_game_version",
		"worlds.generate.error.exchange_unsupported",
		"worlds.generate.error.exchange_other_line",
	}
	for _, l := range Languages {
		for _, key := range keys {
			e, ok := catalogue[l.Tag][key]
			if !ok {
				t.Errorf("%s: %s is missing", l.Tag, key)
				continue
			}
			for _, line := range mapgen.Lines {
				if !strings.Contains(e.text, line) {
					t.Errorf("%s: %s does not mention game line %s. Add the line to the sentence in "+
						"internal/i18n/locales/%s, for every language and for each key in this test, "+
						"and add any new key that lists the lines to this test",
						l.Tag, key, line, l.Tag)
				}
			}
		}
	}
}
