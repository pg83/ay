package main

import (
	"strings"
	"testing"
)

func TestExecutorKeepGoingSyntheticDepFailure(t *testing.T) {
	events := newEventQueue()

	defer events.close()

	ex := newExecutor(t.TempDir(), t.TempDir(), newMemFS(nil), 2, true, true, false, nil, events)
	fetchRefs := &DenseMap[STR, NodeRef]{}

	failCmd := Cmd{CmdArgs: ArgChunks{[]ANY{internStr("/bin/false").any()}}}
	a := &Node{
		Ref:       1,
		Outputs:   []VFS{build("a/out.txt")},
		KV:        &KV{},
		PresetUID: resourceFetchUID("test:exec-a", "a/out.txt"),
		Cmds:      []Cmd{failCmd},
	}
	b := &Node{
		Ref:       2,
		Outputs:   []VFS{build("b/out.txt")},
		KV:        &KV{},
		PresetUID: resourceFetchUID("test:exec-b", "b/out.txt"),
		DepRefs:   []NodeRef{1},
	}

	ex.onNode(a, fetchRefs)
	ex.onNode(b, fetchRefs)
	ex.run([]NodeRef{2})

	bErr := ex.futs.get(2).err

	if bErr == nil {
		t.Fatal("dependent node reported no error")
	}

	msg := bErr.Error()

	if !strings.Contains(msg, "broken by dep") ||
		!strings.Contains(msg, "$(B)/b/out.txt") ||
		!strings.Contains(msg, "$(B)/a/out.txt") {
		t.Errorf("synthetic dep error = %q, want \"{out} broken by dep {dep out}\"", msg)
	}

	if got := ex.failed.Load(); got != 2 {
		t.Errorf("failed counter = %d, want 2", got)
	}

	if done, pending := ex.done.Load(), ex.pending.Load(); done != pending {
		t.Errorf("done/pending = %d/%d, want equal", done, pending)
	}

	if failed := ex.failedRoots([]NodeRef{2}); len(failed) != 1 {
		t.Errorf("failedRoots = %v, want [2]", failed)
	}
}
