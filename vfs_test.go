package main

import (
	"strconv"
	"testing"
)

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
		{name: "source", vfs: Intern("$(S)/a/b.txt"), want: "$(SOURCE_ROOT)/a/b.txt"},
		{name: "build", vfs: Intern("$(B)/x/y.o"), want: "$(BUILD_ROOT)/x/y.o"},
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

func ParseVFSOrSource(s string) VFS {
	if vfsHasPrefix(s) {
		return Intern(s)
	}

	return Source(s)
}

func VFSesFromStrings(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}

	return out
}

func ToVFSSlice(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}

	return out
}
