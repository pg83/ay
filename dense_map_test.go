package main

import "testing"

func TestDenseMap_GetPut(t *testing.T) {
	var m DenseMap[STR, int]

	if _, ok := m.get(0); ok {
		t.Fatal("empty map returned present for key 0")
	}
	if _, ok := m.get(1000); ok {
		t.Fatal("empty map returned present for out-of-range key")
	}

	m.put(5, 50)
	m.put(0, 100) // key 0 must work despite slot-0-is-sentinel
	m.put(1000, 9)

	for _, c := range []struct {
		k    STR
		want int
	}{{5, 50}, {0, 100}, {1000, 9}} {
		got, ok := m.get(c.k)
		if !ok || got != c.want {
			t.Fatalf("Get(%d) = (%d, %v), want (%d, true)", c.k, got, ok, c.want)
		}
	}

	if _, ok := m.get(7); ok {
		t.Fatal("unset key 7 reported present")
	}

	if m.Len() != 3 {
		t.Fatalf("Len = %d, want 3", m.Len())
	}
}

func TestDenseMap_Overwrite(t *testing.T) {
	var m DenseMap[STR, int]
	m.put(42, 1)
	m.put(42, 2)
	m.put(42, 3)

	if got, _ := m.get(42); got != 3 {
		t.Fatalf("Get(42) = %d, want 3 (last write)", got)
	}
	if m.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (overwrite must not grow vals)", m.Len())
	}
}

func TestDenseMap_PointerValuesShareKeys(t *testing.T) {
	// CodegenRegistry stores one *info under several keys; mutating via one key
	// must be visible through the others.
	type info struct{ n int }
	var m DenseMap[STR, *info]
	shared := &info{n: 1}
	m.put(3, shared)
	m.put(99, shared)

	got, _ := m.get(3)
	got.n = 7

	other, _ := m.get(99)
	if other.n != 7 {
		t.Fatalf("mutation via key 3 not visible via key 99: got %d", other.n)
	}
}

func TestDenseMap_GeometricGrowth(t *testing.T) {
	var m DenseMap[STR, int]
	for k := STR(0); k < 1<<12; k++ {
		m.put(k, int(k)*2)
	}
	for k := STR(0); k < 1<<12; k++ {
		if got, ok := m.get(k); !ok || got != int(k)*2 {
			t.Fatalf("Get(%d) = (%d, %v), want (%d, true)", k, got, ok, int(k)*2)
		}
	}
}
