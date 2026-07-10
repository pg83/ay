package main

import "testing"

func TestDeDuperPoolWithReturnsAfterPanic(t *testing.T) {
	var pool DeDuperPool
	var got any

	func() {
		defer func() { got = recover() }()

		pool.with(func(*DeDuper) {
			panic("stop")
		})
	}()

	if got != "stop" {
		t.Fatalf("recovered %v, want stop", got)
	}

	pool.with(func(deduper *DeDuper) {
		if !deduper.add(1) {
			t.Fatal("deduper was not reset after panic")
		}
	})
}

func TestDeDuperPoolRejectsNestedBorrow(t *testing.T) {
	var pool DeDuperPool
	var got any

	func() {
		defer func() { got = recover() }()

		pool.with(func(*DeDuper) {
			pool.with(func(*DeDuper) {})
		})
	}()

	if got == nil {
		t.Fatal("nested deduper borrow did not panic")
	}

	pool.with(func(*DeDuper) {})
}

func TestDeDuper_AddHas(t *testing.T) {
	var dd DeDuper
	dd.reset()

	if dd.has(3) {
		t.Fatal("fresh deduper reports a member")
	}

	if !dd.add(3) {
		t.Fatal("first add reported duplicate")
	}

	if dd.add(3) {
		t.Fatal("repeated add reported new")
	}

	if !dd.has(3) {
		t.Fatal("added id not present")
	}

	if dd.has(4) {
		t.Fatal("never-added id present")
	}
}

func TestDeDuper_FilterSeenNoCollisionReturnsInput(t *testing.T) {
	a := source("dd_filter_a")
	b := source("dd_filter_b")
	c := source("dd_filter_c")

	var dd DeDuper
	dd.reset()
	dd.add(a.strID())

	list := []VFS{b, c}
	got := newNodeArenas().filterSeen(&dd, list)

	if &got[0] != &list[0] {
		t.Fatal("collision-free filterSeen did not return the input slice")
	}

	if !dd.has(b.strID()) || !dd.has(c.strID()) {
		t.Fatal("survivors not added to the set")
	}
}

func TestDeDuper_FilterSeenDropsDuplicates(t *testing.T) {
	a := source("dd_dup_a")
	b := source("dd_dup_b")
	c := source("dd_dup_c")
	d := source("dd_dup_d")

	var dd DeDuper
	dd.reset()
	dd.add(b.strID())

	list := []VFS{a, b, c, c, d}
	got := newNodeArenas().filterSeen(&dd, list)

	want := []VFS{a, c, d}

	if len(got) != len(want) {
		t.Fatalf("filtered length = %d, want %d (%v)", len(got), len(want), got)
	}

	for i, v := range want {
		if got[i] != v {
			t.Fatalf("filtered[%d] = %v, want %v", i, got[i], v)
		}
	}

	if list[1] != b {
		t.Fatal("filterSeen mutated the input slice")
	}
}

func TestDeDuper_ResetClearsMembership(t *testing.T) {
	var dd DeDuper
	dd.reset()
	dd.add(2)

	dd.reset()

	if dd.has(2) {
		t.Fatal("member survived reset")
	}

	if !dd.add(2) {
		t.Fatal("re-add after reset reported duplicate")
	}
}
