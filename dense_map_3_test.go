package main

import "testing"

func TestDenseMap3_IndependentColumns(t *testing.T) {
	var m DenseMap3[STR, int, int, int]

	m.put1(0, 100)
	m.put2(1, 200)
	m.put3(2, 300)

	if m.len() != 3 {
		t.Fatalf("Len = %d, want 3", m.len())
	}

	if got, ok := m.get1(0); !ok || got != 100 {
		t.Fatalf("Get1(0) = (%d, %v), want (100, true)", got, ok)
	}

	if _, ok := m.get2(0); ok {
		t.Fatalf("Get2(0) present, want absent")
	}

	if _, ok := m.get3(0); ok {
		t.Fatalf("Get3(0) present, want absent")
	}

	if got, ok := m.get2(1); !ok || got != 200 {
		t.Fatalf("Get2(1) = (%d, %v), want (200, true)", got, ok)
	}

	if _, ok := m.get1(1); ok {
		t.Fatalf("Get1(1) present, want absent")
	}

	if _, ok := m.get3(1); ok {
		t.Fatalf("Get3(1) present, want absent")
	}

	if got, ok := m.get3(2); !ok || got != 300 {
		t.Fatalf("Get3(2) = (%d, %v), want (300, true)", got, ok)
	}

	if _, ok := m.get1(2); ok {
		t.Fatalf("Get1(2) present, want absent")
	}

	if _, ok := m.get2(2); ok {
		t.Fatalf("Get2(2) present, want absent")
	}
}

func TestDenseMap3_AllColumnsOneKey(t *testing.T) {
	var m DenseMap3[STR, int, int, int]
	const k = 1000

	m.put1(k, 1)
	m.put2(k, 2)
	m.put3(k, 3)

	if got, ok := m.get1(k); !ok || got != 1 {
		t.Fatalf("Get1(k) = (%d, %v), want (1, true)", got, ok)
	}

	if got, ok := m.get2(k); !ok || got != 2 {
		t.Fatalf("Get2(k) = (%d, %v), want (2, true)", got, ok)
	}

	if got, ok := m.get3(k); !ok || got != 3 {
		t.Fatalf("Get3(k) = (%d, %v), want (3, true)", got, ok)
	}

	if m.len() != 1 {
		t.Fatalf("Len = %d, want 1", m.len())
	}
}

func TestDenseMap3_OverwriteAndAbsent(t *testing.T) {
	var m DenseMap3[STR, int, int, int]

	if _, ok := m.get1(0); ok {
		t.Fatal("empty map reported key 0 present")
	}

	if _, ok := m.get1(1 << 20); ok {
		t.Fatal("empty map reported out-of-range key present")
	}

	m.put1(7, 1)
	m.put1(7, 2)
	m.put1(7, 3)

	if got, _ := m.get1(7); got != 3 {
		t.Fatalf("Get1(7) = %d, want 3 (last write)", got)
	}

	if m.len() != 1 {
		t.Fatalf("Len = %d, want 1 (overwrite must not add a key)", m.len())
	}
}
