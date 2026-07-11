package main

import "testing"

func TestScrubRange(t *testing.T) {
	a, b := 1, 2
	s := []*int{&a, &b}

	got := scrub(s[:1])

	if len(got) != 0 || cap(got) != 2 || s[0] != nil || s[1] != &b {
		t.Fatalf("scrub cleared outside len: len=%d cap=%d values=%v", len(got), cap(got), s)
	}

	got = scrubCap(s[:0])

	if len(got) != 0 || cap(got) != 2 || s[1] != nil {
		t.Fatalf("scrubCap left capacity tail alive: len=%d cap=%d values=%v", len(got), cap(got), s)
	}
}
