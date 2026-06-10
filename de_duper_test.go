package main

import "testing"

func TestDeDuper_AddHas(t *testing.T) {
	var dd deDuper
	dd.reset()

	if dd.has(VFS(3)) {
		t.Fatal("fresh deduper reports a member")
	}

	if !dd.add(VFS(3)) {
		t.Fatal("first add reported duplicate")
	}

	if dd.add(VFS(3)) {
		t.Fatal("repeated add reported new")
	}

	if !dd.has(VFS(3)) {
		t.Fatal("added id not present")
	}

	if dd.has(VFS(4)) {
		t.Fatal("never-added id present")
	}
}

func TestDeDuper_FilterSeenNoCollisionReturnsInput(t *testing.T) {
	var dd deDuper
	dd.reset()
	dd.add(VFS(1))

	list := []VFS{VFS(2), VFS(3)}
	got := dd.filterSeen(list)

	if &got[0] != &list[0] {
		t.Fatal("collision-free filterSeen did not return the input slice")
	}

	if !dd.has(VFS(2)) || !dd.has(VFS(3)) {
		t.Fatal("survivors not added to the set")
	}
}

func TestDeDuper_FilterSeenDropsDuplicates(t *testing.T) {
	var dd deDuper
	dd.reset()
	dd.add(VFS(2))

	list := []VFS{VFS(1), VFS(2), VFS(3), VFS(3), VFS(4)}
	got := dd.filterSeen(list)

	want := []VFS{VFS(1), VFS(3), VFS(4)}

	if len(got) != len(want) {
		t.Fatalf("filtered length = %d, want %d (%v)", len(got), len(want), got)
	}

	for i, v := range want {
		if got[i] != v {
			t.Fatalf("filtered[%d] = %v, want %v", i, got[i], v)
		}
	}

	if list[1] != VFS(2) {
		t.Fatal("filterSeen mutated the input slice")
	}
}

func TestDeDuper_ResetClearsMembership(t *testing.T) {
	var dd deDuper
	dd.reset()
	dd.add(VFS(2))

	dd.reset()

	if dd.has(VFS(2)) {
		t.Fatal("member survived reset")
	}

	if !dd.add(VFS(2)) {
		t.Fatal("re-add after reset reported duplicate")
	}
}
