package main

import "testing"

func TestPendingEmitFiresOnceBeforeCallback(t *testing.T) {
	na := newNodeArenas()
	calls := 0

	var pending *PendingEmit

	pending = na.pendingEmit(func() {
		calls++
		pending.fire()
	})

	pending.fire()
	pending.fire()

	if calls != 1 {
		t.Fatalf("pending callback calls = %d, want 1", calls)
	}
}

func TestCodegenRegistryLookupSplitJoinsRegisteredPath(t *testing.T) {
	r := newCodegenRegistry(newNodeArenas())
	want := r.register(GeneratedFileInfo{OutputPath: build("pkg/gen/out.h"), ProducerRef: 7})

	for _, tc := range []struct {
		prefix VFS
		suffix STR
	}{
		{prefix: source(""), suffix: internStr("pkg/gen/out.h")},
		{prefix: source("pkg"), suffix: internStr("gen/out.h")},
		{prefix: source("pkg/gen"), suffix: internStr("out.h")},
	} {
		if got := r.lookupSplit(tc.prefix, tc.suffix.any()); got != want {
			t.Fatalf("lookupSplit(%s, %s) = %p, want %p", tc.prefix.string(), tc.suffix.string(), got, want)
		}
	}

	if got := r.lookupSplit(source("pkg"), source("gen/out.h").any()); got != nil {
		t.Fatalf("lookupSplit accepted VFS suffix: %v", got)
	}

	if got := r.lookupSplit(source("other"), internStr("gen/out.h").any()); got != nil {
		t.Fatalf("lookupSplit returned mismatched prefix: %v", got)
	}
}
