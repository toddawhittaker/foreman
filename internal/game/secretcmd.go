package game

import (
	"regexp"
	"strings"
)

// SetsSecret lists the console commands whose argument is a secret (R6,
// SPEC 14.8). The game has no other command that sets a password or token:
// /config set takes one option at a time and "password" is the only secret
// among them.
var SetsSecret = []string{"config set password"}

// CommandPatterns returns a regular expression that matches any of the
// commands, in any case with any whitespace between the words.
func CommandPatterns(list []string) string {
	parts := make([]string, len(list))
	for i, words := range list {
		parts[i] = strings.Join(strings.Fields(words), `\s+`)
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

// secretInLog finds a secret-setting command anywhere in a log line, since
// the game may print the command it was given.
var secretInLog = regexp.MustCompile(`(?i)(` + CommandPatterns(SetsSecret) + `)\s+\S.*$`)

// maskSecretCommand hides the secret argument of a command the game printed.
func maskSecretCommand(line string) string {
	return secretInLog.ReplaceAllString(line, "${1} "+Mask)
}
