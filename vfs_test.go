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
		{name: "source", vfs: intern("$(S)/a/b.txt"), want: "$(SOURCE_ROOT)/a/b.txt"},
		{name: "build", vfs: intern("$(B)/x/y.o"), want: "$(BUILD_ROOT)/x/y.o"},
	}

	for _, tc := range cases {
		if got := tc.vfs.longString(); got != tc.want {
			t.Fatalf("%s LongString = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func BenchmarkMapAccess_VFSStructKey(b *testing.B) {
	keys := bvKeys()
	m := make(map[VFS]struct{}, bvN)

	for _, k := range keys {
		m[source(k)] = struct{}{}
	}

	probes := make([]VFS, bvN)

	for i, k := range keys {
		probes[i] = source(k)
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
		m[source(k)] = struct{}{}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, ok := m[source(keys[i%bvN])]

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

type VfsMapNonGeneric [2]map[string]struct{}

func newVFSMapNonGeneric(cap int) VfsMapNonGeneric {
	return VfsMapNonGeneric{
		make(map[string]struct{}, cap),
		make(map[string]struct{}, cap),
	}
}

func (m VfsMapNonGeneric) has(v VFS) bool {
	_, ok := m[uint32(v)&1][v.relString()]

	return ok
}

func (m VfsMapNonGeneric) add(v VFS) {
	m[uint32(v)&1][v.relString()] = struct{}{}
}

type VfsMap[T any] [2]map[string]T

func newVFSMap[T any](cap int) VfsMap[T] {
	return VfsMap[T]{
		make(map[string]T, cap),
		make(map[string]T, cap),
	}
}

func (m VfsMap[T]) get(v VFS) (T, bool) {
	val, ok := m[uint32(v)&1][v.relString()]

	return val, ok
}

func (m VfsMap[T]) set(v VFS, val T) {
	m[uint32(v)&1][v.relString()] = val
}

func BenchmarkMapAccess_VFS2Bucket_NonGeneric(b *testing.B) {
	keys := bvKeys()
	m := newVFSMapNonGeneric(bvN)

	for _, k := range keys {
		m.add(source(k))
	}

	probes := make([]VFS, bvN)

	for i, k := range keys {
		probes[i] = source(k)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if !m.has(probes[i%bvN]) {
			b.Fatalf("miss")
		}
	}
}

func BenchmarkMapAccess_VFS2Bucket_Generic(b *testing.B) {
	keys := bvKeys()
	m := newVFSMap[struct{}](bvN)

	for _, k := range keys {
		m.set(source(k), struct{}{})
	}

	probes := make([]VFS, bvN)

	for i, k := range keys {
		probes[i] = source(k)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, ok := m.get(probes[i%bvN]); !ok {
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
		probes[i] = source(k)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		v := probes[i%bvN]

		if _, ok := m[uint32(v)&1][v.relString()]; !ok {
			b.Fatalf("miss")
		}
	}
}

func ParseVFSOrSource(s string) VFS {
	if vfsHasPrefix(s) {
		return intern(s)
	}

	return source(s)
}

func VFSesFromStrings(ss []string) []VFS {
	out := make([]VFS, len(ss))

	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}

	return out
}

func genericStrs[T interface {
	~uint32
	string() string
}](as []T) []string {
	out := make([]string, 0, len(as))

	for _, a := range as {
		out = append(out, a.string())
	}

	return out
}

func ToAnySlice(ss []STR) []ANY {
	out := make([]ANY, len(ss))

	for i, s := range ss {
		out[i] = s.any()
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
