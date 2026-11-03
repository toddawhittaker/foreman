package config

import (
	"fmt"
	"strings"

	"git.thewhittakers.org/todd/foreman/internal/secret"
)

// env reads variables, and the files named by their _FILE variants.
type env struct {
	getenv   func(string) (string, bool)
	readFile func(string) ([]byte, error)
}

// get returns the variable's value; unset and empty both report false.
func (e env) get(name string) (string, bool) {
	v, ok := e.getenv(name)
	return v, ok && v != ""
}

// lookup returns the value of name, or the contents of the file named by
// name_FILE. Setting both is an error (SPEC 3.2).
func (e env) lookup(name string) (string, bool, error) {
	fileVar := name + "_FILE"
	v, haveValue := e.get(name)
	path, haveFile := e.get(fileVar)
	switch {
	case haveValue && haveFile:
		return "", false, fmt.Errorf("%s and %s are both set; remove one of them", name, fileVar)
	case haveValue:
		return v, true, nil
	case !haveFile:
		return "", false, nil
	}
	b, err := e.readFile(path)
	if err != nil {
		return "", false, fmt.Errorf("%s: cannot read the file: %w", fileVar, err)
	}
	v = strings.TrimRight(string(b), "\r\n")
	if v == "" {
		return "", false, fmt.Errorf("%s names an empty file", fileVar)
	}
	return v, true, nil
}

// lookupSecret is lookup for a secret variable.
func (e env) lookupSecret(name string) (secret.Secret, bool, error) {
	v, ok, err := e.lookup(name)
	return secret.New(v), ok, err
}
