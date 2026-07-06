package main

import "testing"

func TestIdValueMap_PutGet(t *testing.T) {
	var m IdValueMap
	m.reset(8)

	if _, ok := m.get(VFS(3)); ok {
		t.Fatal("fresh map reports a key")
	}

	m.put(VFS(3), 42)
	m.put(VFS(5), -7)

	if v, ok := m.get(VFS(3)); !ok || v != 42 {
		t.Fatalf("get(3) = %d,%v, want 42,true", v, ok)
	}

	if v, ok := m.get(VFS(5)); !ok || v != -7 {
		t.Fatalf("get(5) = %d,%v, want -7,true", v, ok)
	}

	if _, ok := m.get(VFS(4)); ok {
		t.Fatal("never-put key present")
	}
}

func TestIdValueMap_PutOverwritesSameKey(t *testing.T) {
	var m IdValueMap
	m.reset(8)

	m.put(VFS(2), 1)
	m.put(VFS(2), 9)

	if v, ok := m.get(VFS(2)); !ok || v != 9 {
		t.Fatalf("get(2) after overwrite = %d,%v, want 9,true", v, ok)
	}
}

func TestIdValueMap_ZeroKeyAndZeroValue(t *testing.T) {
	var m IdValueMap
	m.reset(4)

	m.put(VFS(0), 0)

	if v, ok := m.get(VFS(0)); !ok || v != 0 {
		t.Fatalf("get(0) = %d,%v, want 0,true", v, ok)
	}
}

func TestIdValueMap_ResetClearsEntriesReusingArrays(t *testing.T) {
	var m IdValueMap
	m.reset(8)
	m.put(VFS(2), 5)

	before := cap(m.gen.s)
	m.reset(8)

	if _, ok := m.get(VFS(2)); ok {
		t.Fatal("entry survived reset")
	}

	if cap(m.gen.s) != before {
		t.Fatalf("reset reallocated backing arrays (cap %d -> %d) for an unchanged size", before, cap(m.gen.s))
	}

	m.put(VFS(2), 6)

	if v, ok := m.get(VFS(2)); !ok || v != 6 {
		t.Fatalf("get(2) after re-put = %d,%v, want 6,true", v, ok)
	}
}

func TestIdValueMap_ResetGrowsWhenSizeExceedsCapacity(t *testing.T) {
	var m IdValueMap
	m.reset(4)
	m.put(VFS(1), 3)
	m.reset(64)

	if _, ok := m.get(VFS(1)); ok {
		t.Fatal("entry survived growing reset")
	}

	if m.gen.len() < 64 || len(m.val) < 64 {
		t.Fatalf("backing arrays not grown: gen=%d val=%d, want >= 64", m.gen.len(), len(m.val))
	}
}

func TestIdValueMap_PutGrowsBeyondLen(t *testing.T) {
	var m IdValueMap
	m.reset(4)
	m.put(VFS(2), 11)
	m.put(VFS(100), 22)

	if v, ok := m.get(VFS(100)); !ok || v != 22 {
		t.Fatalf("get(100) = %d,%v, want 22,true", v, ok)
	}

	if v, ok := m.get(VFS(2)); !ok || v != 11 {
		t.Fatalf("get(2) after growth = %d,%v, want 11,true", v, ok)
	}
}

func TestIdValueMap_GetBeyondLenMisses(t *testing.T) {
	var m IdValueMap
	m.reset(4)

	if _, ok := m.get(VFS(1 << 20)); ok {
		t.Fatal("get far beyond backing length reports a key")
	}
}

func TestIdValueMap_EpochWraparoundDoesNotResurrect(t *testing.T) {
	var m IdValueMap
	m.reset(8)

	m.epoch = ^uint32(0)

	m.put(VFS(3), 77)
	m.reset(8)

	if _, ok := m.get(VFS(3)); ok {
		t.Fatal("entry resurrected across epoch wraparound")
	}

	m.put(VFS(4), 5)

	if v, ok := m.get(VFS(4)); !ok || v != 5 {
		t.Fatalf("get(4) after wraparound reset = %d,%v, want 5,true", v, ok)
	}
}

func TestIdValueMap_ManyEpochsIndependent(t *testing.T) {
	var m IdValueMap
	m.reset(16)

	for round := int32(0); round < 100; round++ {
		m.reset(16)

		k := VFS(uint32(round) % 16)

		if _, ok := m.get(k); ok {
			t.Fatalf("round %d: stale entry visible after reset", round)
		}

		m.put(k, round)

		if v, ok := m.get(k); !ok || v != round {
			t.Fatalf("round %d: get = %d,%v, want %d,true", round, v, ok, round)
		}
	}
}
