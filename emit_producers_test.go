package main

import (
	"strings"
	"testing"
)

func TestScheduleProducers_NoEdgesKeepsDeclarationOrder(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/a.bin")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/b.bin")}},
		{kind: prodRunPython, index: 0, outs: []VFS{build("m/c.txt")}},
	}

	var m IdValueMap

	order := scheduleProducers(&m, positions, "m")

	for i, pi := range order {
		if pi != i {
			t.Fatalf("no-edge order = %v, want identity", order)
		}
	}
}

func TestScheduleProducers_ReversedChainReordered(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/third.bin")}, ins: []VFS{build("m/second.bin")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/second.bin")}, ins: []VFS{build("m/first.txt")}},
		{kind: prodRunPython, index: 0, outs: []VFS{build("m/first.txt")}},
	}

	var m IdValueMap

	order := scheduleProducers(&m, positions, "m")

	if order[0] != 2 || order[1] != 1 || order[2] != 0 {
		t.Fatalf("chain order = %v, want [2 1 0]", order)
	}
}

func TestScheduleProducers_TieBreaksByDeclarationOrder(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/late.bin")}, ins: []VFS{build("m/base.txt")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/mid.bin")}},
		{kind: prodRunPython, index: 0, outs: []VFS{build("m/base.txt")}},
	}

	var m IdValueMap

	order := scheduleProducers(&m, positions, "m")

	if order[0] != 1 || order[1] != 2 || order[2] != 0 {
		t.Fatalf("tie order = %v, want [1 2 0]", order)
	}
}

func TestScheduleProducers_CycleThrows(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/a.bin")}, ins: []VFS{build("m/b.bin")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/b.bin")}, ins: []VFS{build("m/a.bin")}},
	}

	var m IdValueMap

	exc := try(func() {
		scheduleProducers(&m, positions, "m")
	})

	if exc == nil {
		t.Fatal("cycle did not throw")
	}

	if !strings.Contains(exc.Error(), "dependency cycle") {
		t.Fatalf("cycle threw %q, want dependency-cycle diagnostics", exc.Error())
	}
}
