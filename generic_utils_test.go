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

func TestRetainMaxLen(t *testing.T) {
	s := make([]int, 3, 4)

	if got := retainMaxLen(s, s[:2]); len(got) != 3 {
		t.Fatalf("short use shrank dirty prefix to %d", len(got))
	}

	if got := retainMaxLen(s, append(s[:0], 1, 2, 3, 4, 5)); len(got) != 5 {
		t.Fatalf("grown use left dirty prefix at %d", len(got))
	}
}
