package gamesettings

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDiff(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		want     []Change
	}{
		{name: "same", old: `{"a": 1}`, new: "{\n \"a\": 1\n}"},
		{name: "number spelling is not a change", old: `{"a": 1}`, new: `{"a": 1.0}`},
		{name: "string escape is not a change", old: `{"a": "A"}`, new: `{"a": "A"}`},
		{name: "added removed changed", old: `{"a": 1, "b": "x", "c": true}`, new: `{"a": 2, "c": true, "d": [1, 2]}`,
			want: []Change{
				{Key: "a", Kind: KindChanged, Old: "1", New: "2"},
				{Key: "d", Kind: KindAdded, New: "[1,2]"},
				{Key: "b", Kind: KindRemoved, Old: `"x"`},
			}},
		{name: "nested keys are dotted", old: `{"visibility": {"public": false, "lan": true}}`,
			new: `{"visibility": {"public": false, "lan": false}}`,
			want: []Change{
				{Key: "visibility.lan", Kind: KindChanged, Old: "true", New: "false"},
			}},
		{name: "unknown keys are listed", old: `{}`, new: `{"mod_thing": {"deep": {"x": 1}}}`,
			want: []Change{{Key: "mod_thing.deep.x", Kind: KindAdded, New: "1"}}},
		{name: "whole object removed", old: `{"o": {"a": 1, "b": 2}}`, new: `{}`,
			want: []Change{
				{Key: "o.a", Kind: KindRemoved, Old: "1"},
				{Key: "o.b", Kind: KindRemoved, Old: "2"},
			}},
		{name: "object becomes a value", old: `{"o": {"a": 1}}`, new: `{"o": 5}`,
			want: []Change{
				{Key: "o", Kind: KindAdded, New: "5"},
				{Key: "o.a", Kind: KindRemoved, Old: "1"},
			}},
		{name: "array reorder is a change", old: `{"t": ["a", "b"]}`, new: `{"t": ["b", "a"]}`,
			want: []Change{{Key: "t", Kind: KindChanged, Old: `["a","b"]`, New: `["b","a"]`}}},
		{name: "array spacing is not", old: `{"t": ["a","b"]}`, new: `{"t": [ "a", "b" ]}`},
		{name: "last repeated key wins", old: `{"a": 1, "a": 2}`, new: `{"a": 2}`},
		{name: "unusable old counts as empty", old: `{`, new: `{"a": 1}`,
			want: []Change{{Key: "a", Kind: KindAdded, New: "1"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Diff([]byte(tc.old), []byte(tc.new))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDiffSecrets(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		want     []Change
	}{
		{name: "set", old: `{}`, new: `{"game_password": "hunter2"}`,
			want: []Change{{Key: "game_password", Kind: KindAdded, Secret: true}}},
		{name: "set over empty", old: `{"token": ""}`, new: `{"token": "abc"}`,
			want: []Change{{Key: "token", Kind: KindAdded, Secret: true}}},
		{name: "changed", old: `{"password": "one"}`, new: `{"password": "two"}`,
			want: []Change{{Key: "password", Kind: KindChanged, Secret: true}}},
		{name: "same", old: `{"password": "one"}`, new: `{"password": "one"}`},
		{name: "cleared by empty string", old: `{"username": "todd"}`, new: `{"username": ""}`,
			want: []Change{{Key: "username", Kind: KindRemoved, Secret: true}}},
		{name: "cleared by null", old: `{"username": "todd"}`, new: `{"username": null}`,
			want: []Change{{Key: "username", Kind: KindRemoved, Secret: true}}},
		{name: "removed key", old: `{"game_password": "x"}`, new: `{}`,
			want: []Change{{Key: "game_password", Kind: KindRemoved, Secret: true}}},
		{name: "empty to absent is nothing", old: `{"token": ""}`, new: `{}`},
		{name: "empty stays empty", old: `{"token": ""}`, new: `{"token": ""}`},
		{name: "object secret is one change", old: `{}`, new: `{"token": {"a": "hunter2", "b": 2}}`,
			want: []Change{{Key: "token", Kind: KindAdded, Secret: true}}},
		{name: "object secret removed", old: `{"token": {"a": "hunter2"}}`, new: `{}`,
			want: []Change{{Key: "token", Kind: KindRemoved, Secret: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Diff([]byte(tc.old), []byte(tc.new))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// No secret value reaches any Change field, whatever the change (SPEC 11.4, 14.8).
func TestDiffNeverHoldsASecret(t *testing.T) {
	secrets := []string{"hunter2", "hunter3", "tok-AAA", "tok-BBB", "todd-1", "todd-2", "pw-1", "pw-2"}
	olds := []string{
		`{}`,
		`{"game_password": "hunter2", "token": "tok-AAA", "username": "todd-1", "password": "pw-1", "name": "a"}`,
		`{"game_password": {"v": "hunter2"}, "token": ["tok-AAA"]}`,
	}
	news := []string{
		`{}`,
		`{"game_password": "hunter3", "token": "tok-BBB", "username": "todd-2", "password": "pw-2", "name": "b"}`,
		`{"game_password": "hunter2", "token": ["tok-BBB"], "username": {"u": "todd-2"}}`,
	}
	for i, o := range olds {
		for j, n := range news {
			t.Run(fmt.Sprint(i, "-", j), func(t *testing.T) {
				for _, c := range Diff([]byte(o), []byte(n)) {
					if c.Secret != isSecretKey(c.Key) {
						t.Errorf("Secret flag wrong on %+v", c)
					}
					if c.Secret && (c.Old != "" || c.New != "") {
						t.Errorf("secret change carries a value: %+v", c)
					}
					all := c.Key + c.Old + c.New + string(c.Kind)
					for _, s := range secrets {
						if strings.Contains(all, s) {
							t.Errorf("change holds %q: %+v", s, c)
						}
					}
				}
			})
		}
	}
}
