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
