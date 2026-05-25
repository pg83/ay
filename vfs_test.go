package main

import (
	"strconv"
	"testing"
)

// vfs_bench_test.go — microbenchmarks for the VFS-keyed map perf
// regression hypothesis. Compares:
//   - map[string]struct{} access via the "$(S)/<rel>" key
//     (PREV scanner shape).
//   - map[VFS]struct{} access where VFS = struct{Root uint8; Rel string}
//     (HEAD scanner shape).
//
// Both maps hold the same number of entries with the same rel strings;
// the lookup pattern is the same. Measures cumulative ns/op and
// allocs/op via Go's testing.B.

const bvN = 5000

func bvKeys() []string {
	out := make([]string, bvN)
	for i := 0; i < bvN; i++ {
		out[i] = "devtools/ymake/diag/stats_enums_" + strconv.Itoa(i) + ".h"
	}
	return out
}

func BenchmarkMapAccess_StringKey(b *testing.B) {
	keys := bvKeys()
	m := make(map[string]struct{}, bvN)
	for _, k := range keys {
		m["$(S)/"+k] = struct{}{}
	}
	probes := make([]string, bvN)
	for i, k := range keys {
		probes[i] = "$(S)/" + k
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := m[probes[i%bvN]]
		if !ok {
			b.Fatalf("miss on i=%d", i)
		}
	}
}

func TestVFSLongString(t *testing.T) {
	cases := []struct {
		name string
		vfs  VFS
		want string
	}{
		{name: "source", vfs: Source("a/b.txt"), want: "$(SOURCE_ROOT)/a/b.txt"},
		{name: "build", vfs: Build("x/y.o"), want: "$(BUILD_ROOT)/x/y.o"},
	}

	for _, tc := range cases {
		if got := tc.vfs.LongString(); got != tc.want {
			t.Fatalf("%s LongString = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func BenchmarkMapAccess_VFSStructKey(b *testing.B) {
	keys := bvKeys()
	m := make(map[VFS]struct{}, bvN)
	for _, k := range keys {
		m[Source(k)] = struct{}{}
	}
	probes := make([]VFS, bvN)
	for i, k := range keys {
		probes[i] = Source(k)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := m[probes[i%bvN]]
		if !ok {
			b.Fatalf("miss on i=%d", i)
		}
	}
}

// BenchmarkMapAccess_VFSStructKey_ConstructedAtProbe simulates the
// scanner shape where the probe key is constructed inside the hot
// loop (Source(rel) per lookup) rather than precomputed.
func BenchmarkMapAccess_VFSStructKey_ConstructedAtProbe(b *testing.B) {
	keys := bvKeys()
	m := make(map[VFS]struct{}, bvN)
	for _, k := range keys {
		m[Source(k)] = struct{}{}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := m[Source(keys[i%bvN])]
		if !ok {
			b.Fatalf("miss on i=%d", i)
		}
	}
}

// BenchmarkMapAccess_StringKey_ConstructedAtProbe simulates the alt
// shape: keep map[string], but build the key via concat at each probe.
func BenchmarkMapAccess_StringKey_ConstructedAtProbe(b *testing.B) {
	keys := bvKeys()
	m := make(map[string]struct{}, bvN)
	for _, k := range keys {
		m["$(S)/"+k] = struct{}{}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := m["$(S)/"+keys[i%bvN]]
		if !ok {
			b.Fatalf("miss on i=%d", i)
		}
	}
}

// --- 2-bucket VFS map prototypes ---

// vfsMapNonGeneric: hand-rolled, fixed value type (struct{}). Establishes
// the upper bound — what the dispatched lookup costs with no generics.
type vfsMapNonGeneric [2]map[string]struct{}

func newVFSMapNonGeneric(cap int) vfsMapNonGeneric {
	return vfsMapNonGeneric{
		make(map[string]struct{}, cap),
		make(map[string]struct{}, cap),
	}
}
func (m vfsMapNonGeneric) Has(v VFS) bool {
	_, ok := m[v.Root()-1][v.Rel()]
	return ok
}
func (m vfsMapNonGeneric) Add(v VFS) { m[v.Root()-1][v.Rel()] = struct{}{} }

// vfsMap[T] generic wrapper. Each instantiation specialises to a
// concrete map[string]T; lookups should land on mapaccess2_faststr.
type vfsMap[T any] [2]map[string]T

func newVFSMap[T any](cap int) vfsMap[T] {
	return vfsMap[T]{
		make(map[string]T, cap),
		make(map[string]T, cap),
	}
}
func (m vfsMap[T]) Get(v VFS) (T, bool) {
	val, ok := m[v.Root()-1][v.Rel()]
	return val, ok
}
func (m vfsMap[T]) Set(v VFS, val T) { m[v.Root()-1][v.Rel()] = val }

func BenchmarkMapAccess_VFS2Bucket_NonGeneric(b *testing.B) {
	keys := bvKeys()
	m := newVFSMapNonGeneric(bvN)
	for _, k := range keys {
		m.Add(Source(k))
	}
	probes := make([]VFS, bvN)
	for i, k := range keys {
		probes[i] = Source(k)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !m.Has(probes[i%bvN]) {
			b.Fatalf("miss")
		}
	}
}

func BenchmarkMapAccess_VFS2Bucket_Generic(b *testing.B) {
	keys := bvKeys()
	m := newVFSMap[struct{}](bvN)
	for _, k := range keys {
		m.Set(Source(k), struct{}{})
	}
	probes := make([]VFS, bvN)
	for i, k := range keys {
		probes[i] = Source(k)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := m.Get(probes[i%bvN]); !ok {
			b.Fatalf("miss")
		}
	}
}

// BenchmarkMapAccess_VFS2Bucket_Inline: bypass method, do the dispatch
// inline. Establishes the absolute floor — what the bucket-dispatch
// shape costs with zero indirection.
func BenchmarkMapAccess_VFS2Bucket_Inline(b *testing.B) {
	keys := bvKeys()
	var m [2]map[string]struct{}
	m[0] = make(map[string]struct{}, bvN)
	m[1] = make(map[string]struct{}, bvN)
	for _, k := range keys {
		m[0][k] = struct{}{}
	}
	probes := make([]VFS, bvN)
	for i, k := range keys {
		probes[i] = Source(k)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := probes[i%bvN]
		if _, ok := m[v.Root()-1][v.Rel()]; !ok {
			b.Fatalf("miss")
		}
	}
}

// ParseVFSOrSource is a test-only shim for legacy fixtures that still
// spell VFS values either as canonical "$(S)/..." / "$(B)/..." strings
// or as bare source-relative paths.
func ParseVFSOrSource(s string) VFS {
	if vfsHasPrefix(s) {
		return ParseVFS(s)
	}

	return Source(s)
}

// VFSesFromStrings is the bulk test helper variant of ParseVFSOrSource.
func VFSesFromStrings(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}

	return out
}

// ToVFSSlice keeps old emitter/node test fixtures readable without
// forcing every literal to be rewritten during production refactors.
func ToVFSSlice(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}

	return out
}
