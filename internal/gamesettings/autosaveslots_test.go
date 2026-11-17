package gamesettings

import "testing"

func TestAutosaveSlots(t *testing.T) {
	cases := []struct {
		in              string
		slots, interval int
		ok              bool
	}{
		{`{}`, 5, 10, true},
		{`{"autosave_slots": 10, "autosave_interval": 1}`, 10, 1, true},
		{`{"autosave_slots": 0}`, 0, 10, true},
		{`{"autosave_slots": 3, "autosave_slots": 7}`, 7, 10, true},
		{`{"autosave_slots": "10"}`, 0, 0, false},
		{`{"autosave_slots": -1}`, 0, 0, false},
		{`{"autosave_slots": 2.5}`, 0, 0, false},
		{`[]`, 0, 0, false},
		{`not json`, 0, 0, false},
	}
	for _, c := range cases {
		s, i, ok := AutosaveSlots([]byte(c.in))
		if s != c.slots || i != c.interval || ok != c.ok {
			t.Errorf("AutosaveSlots(%s) = %d, %d, %v; want %d, %d, %v", c.in, s, i, ok, c.slots, c.interval, c.ok)
		}
	}
}
