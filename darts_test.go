package main

import "testing"

func TestDartsLongestMatch(t *testing.T) {
	keys := []string{"arc/", "arc/api/", "util/", "april/arf/"}
	d := NewDarts(keys)

	cases := []struct {
		parts   []string
		wantIdx int
		wantOk  bool
	}{
		{[]string{"arc/api/proto/"}, 1, true},     // longest of arc/ , arc/api/
		{[]string{"arc/api/proto", "/"}, 1, true}, // variadic stream, no concat
		{[]string{"arc/", "api/x/"}, 1, true},     // split mid-key across parts
		{[]string{"arc/x/"}, 0, true},             // only arc/ matches
		{[]string{"arc/"}, 0, true},               // exact key
		{[]string{"arcfoo/x/"}, 0, false},         // component boundary: arc/ not a prefix
		{[]string{"util/x/"}, 2, true},
		{[]string{"april/arf/x/"}, 3, true},
		{[]string{"april/"}, 0, false}, // april/ is not a key (only april/arf/)
		{[]string{"contrib/x/"}, 0, false},
		{[]string{""}, 0, false},
		{nil, 0, false},
	}

	for _, c := range cases {
		idx, ok := d.longestMatch(c.parts...)
		if ok != c.wantOk || (ok && idx != c.wantIdx) {
			t.Errorf("longestMatch(%q) = (%d,%v), want (%d,%v)", c.parts, idx, ok, c.wantIdx, c.wantOk)
		}
	}
}

func TestDartsEmptyAndExact(t *testing.T) {
	if _, ok := NewDarts(nil).longestMatch("anything"); ok {
		t.Error("empty Darts matched")
	}

	d := NewDarts([]string{"a", "ab", "abc"})

	for _, tc := range []struct {
		q   string
		idx int
		ok  bool
	}{
		{"abcd", 2, true},
		{"abc", 2, true},
		{"ab", 1, true},
		{"a", 0, true},
		{"b", 0, false},
		{"", 0, false},
	} {
		idx, ok := d.longestMatch(tc.q)
		if ok != tc.ok || (ok && idx != tc.idx) {
			t.Errorf("longestMatch(%q) = (%d,%v), want (%d,%v)", tc.q, idx, ok, tc.idx, tc.ok)
		}
	}
}

func TestDartsLastDuplicateWins(t *testing.T) {
	d := NewDarts([]string{"x/", "x/"})

	if idx, ok := d.longestMatch("x/y"); !ok || idx != 1 {
		t.Errorf("dup key: got (%d,%v), want (1,true)", idx, ok)
	}
}
