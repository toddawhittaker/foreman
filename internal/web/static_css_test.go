package web

import (
	"fmt"
	"strings"
	"testing"
)

// cssBalance reports the first line where the braces of css go wrong: a
// closing brace with nothing open, a block comment that never ends, or a
// block still open at the end. Braces inside strings and comments do not
// count. It returns "" when the file is balanced.
func cssBalance(css string) string {
	depth, line := 0, 1
	var openedAt []int
	for i := 0; i < len(css); i++ {
		c := css[i]
		switch {
		case c == '\n':
			line++
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			start := line
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				return fmt.Sprintf("line %d: a comment is never closed", start)
			}
			line += strings.Count(css[i:i+2+end], "\n")
			i += end + 3
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(css) && css[j] != c && css[j] != '\n' {
				if css[j] == '\\' {
					j++
				}
				j++
			}
			i = j
		case c == '{':
			depth++
			openedAt = append(openedAt, line)
		case c == '}':
			if depth == 0 {
				return fmt.Sprintf("line %d: a closing brace has nothing to close", line)
			}
			depth--
			openedAt = openedAt[:depth]
		}
	}
	if depth > 0 {
		return fmt.Sprintf("line %d: this block is never closed (depth %d at the end)", openedAt[depth-1], depth)
	}
	return ""
}

func TestCSSBalanceFinder(t *testing.T) {
	for _, c := range []struct{ name, css, want string }{
		{"balanced", "a{b:c}\n@media x{d{e:f}}", ""},
		{"braces in a comment", "/* { */\na{b:c}", ""},
		{"braces in a string", "a::after{content:\"}\"}\nb::after{content:'{'}", ""},
		{"extra close", "a{b:c}\n}\n", "line 2"},
		{"missing close swallows the next rule", "a{b:c\nd{e:f}\n", "line 1"},
		{"open comment", "a{b:c}\n/* oops\nd{e:f}", "line 2"},
	} {
		got := cssBalance(c.css)
		if (c.want == "") != (got == "") || !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: got %q, want prefix %q", c.name, got, c.want)
		}
	}
}

// A hand-resolved merge once dropped a closing brace and swallowed the next
// rule, so the stylesheet must balance.
func TestForemanCSSIsBalanced(t *testing.T) {
	css, err := staticFS.ReadFile("static/foreman.css")
	if err != nil {
		t.Fatal(err)
	}
	if got := cssBalance(string(css)); got != "" {
		t.Errorf("static/foreman.css: %s", got)
	}
}
