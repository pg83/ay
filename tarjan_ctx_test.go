package main

import (
	"reflect"
	"testing"
)

// mockSink is an in-memory closureSink whose emitClosure cache makes a node built
// by one SCC visible to a later one.
type MockSink struct {
	children map[VFS][]VFS
	cache    map[VFS][]VFS // node -> its built closure window
	emits    [][]VFS       // emitted, in completion order
}

func newMockSink(children map[VFS][]VFS, preCached map[VFS][]VFS) *MockSink {
	cache := make(map[VFS][]VFS, len(preCached))
	for k, v := range preCached {
		cache[k] = v
	}

	return &MockSink{children: children, cache: cache}
}

func (m *MockSink) forEachChild(v VFS, fn func(VFS)) {
	for _, c := range m.children[v] {
		fn(c)
	}
}

func (m *MockSink) cachedWindow(v VFS) ([]VFS, bool) {
	w, ok := m.cache[v]
	return w, ok
}

// windowSubsumed always declines: the mock exercises splice mechanics, not the
// subsumption fast path.
func (m *MockSink) windowSubsumed(VFS) bool {
	return false
}

func (m *MockSink) emitClosure(members []VFS, fill func(block []VFS) int) {
	block := make([]VFS, 1<<12)
	k := fill(block)
	closure := append([]VFS(nil), block[:k]...)

	m.emits = append(m.emits, closure)

	for _, u := range members {
		m.cache[u] = closure
	}
}

func sourceVFSList(rels ...string) []VFS {
	out := make([]VFS, len(rels))
	for i, r := range rels {
		out[i] = source(r)
	}

	return out
}

func TestTarjan_TwoNodeCycle(t *testing.T) {
	a, b := source("tj/a.h"), source("tj/b.h")
	m := newMockSink(map[VFS][]VFS{
		a: {b},
		b: {a},
	}, nil)

	tc := &TarjanCtx{}
	hits := tc.runSCC(m, a)

	if hits != 0 {
		t.Errorf("hits = %d, want 0 (no cached children)", hits)
	}

	want := sourceVFSList("tj/a.h", "tj/b.h")
	for _, n := range []VFS{a, b} {
		if got := m.cache[n]; !reflect.DeepEqual(got, want) {
			t.Errorf("closure(%s) = %v, want %v", n.rel(), relsOf(got), relsOf(want))
		}
	}

	if len(m.emits) != 1 {
		t.Errorf("emitted %d closures, want 1 SCC", len(m.emits))
	}
}

func TestTarjan_CycleSplicesCachedExternalDep(t *testing.T) {
	a, b, x, y := source("tj/a.h"), source("tj/b.h"), source("tj/x.h"), source("tj/y.h")
	m := newMockSink(map[VFS][]VFS{
		a: {b, x},
		b: {a},
	}, map[VFS][]VFS{
		x: {x, y}, // already built
	})

	tc := &TarjanCtx{}
	hits := tc.runSCC(m, a)

	if hits != 1 {
		t.Errorf("hits = %d, want 1 (the cached x edge)", hits)
	}

	want := sourceVFSList("tj/a.h", "tj/b.h", "tj/x.h", "tj/y.h")
	if got := m.cache[a]; !reflect.DeepEqual(got, want) {
		t.Errorf("closure(a) = %v, want %v", relsOf(got), relsOf(want))
	}
}

func TestTarjan_SingletonRootWithCachedChild(t *testing.T) {
	// dfs hands non-cyclic roots to runSCC too; it must still build their closure.
	a, x := source("tj/a.h"), source("tj/x.h")
	m := newMockSink(map[VFS][]VFS{
		a: {x},
	}, map[VFS][]VFS{
		x: {x},
	})

	tc := &TarjanCtx{}
	hits := tc.runSCC(m, a)

	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}

	want := sourceVFSList("tj/a.h", "tj/x.h")
	if got := m.cache[a]; !reflect.DeepEqual(got, want) {
		t.Errorf("closure(a) = %v, want %v", relsOf(got), relsOf(want))
	}
}

func TestTarjan_NestedSCCBuiltChildSpliced(t *testing.T) {
	// c finalizes as a singleton SCC first; then {a,b} splices c's window.
	a, b, c := source("tj/a.h"), source("tj/b.h"), source("tj/c.h")
	m := newMockSink(map[VFS][]VFS{
		a: {b, c},
		b: {a},
		c: {},
	}, nil)

	tc := &TarjanCtx{}
	if hits := tc.runSCC(m, a); hits != 0 {
		t.Errorf("hits = %d, want 0", hits)
	}

	if len(m.emits) != 2 {
		t.Fatalf("emitted %d closures, want 2 (singleton c, then {a,b})", len(m.emits))
	}

	if got, want := m.emits[0], sourceVFSList("tj/c.h"); !reflect.DeepEqual(got, want) {
		t.Errorf("first SCC = %v, want %v (c finalizes before a)", relsOf(got), relsOf(want))
	}

	wantAB := sourceVFSList("tj/a.h", "tj/b.h", "tj/c.h")
	if got := m.cache[a]; !reflect.DeepEqual(got, wantAB) {
		t.Errorf("closure(a) = %v, want %v", relsOf(got), relsOf(wantAB))
	}
}

func TestTarjan_DedupesRepeatedWindowEntries(t *testing.T) {
	// Overlapping spliced windows; each node must appear once.
	a, b, x, y, z := source("tj/a.h"), source("tj/b.h"), source("tj/x.h"), source("tj/y.h"), source("tj/z.h")
	m := newMockSink(map[VFS][]VFS{
		a: {b, x},
		b: {a, y},
	}, map[VFS][]VFS{
		x: {x, z},
		y: {y, z},
	})

	tc := &TarjanCtx{}
	tc.runSCC(m, a)

	want := sourceVFSList("tj/a.h", "tj/b.h", "tj/x.h", "tj/z.h", "tj/y.h")
	if got := m.cache[a]; !reflect.DeepEqual(got, want) {
		t.Errorf("closure(a) = %v, want %v (z deduped)", relsOf(got), relsOf(want))
	}
}

func relsOf(vs []VFS) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.rel()
	}

	return out
}
