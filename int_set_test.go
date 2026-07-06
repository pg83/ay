package main

import "testing"

func TestIntSet_GetAbsent(t *testing.T) {
	s := newIntSet(8)

	if v, ok := s.get(42); ok || v {
		t.Fatalf("get(42) on empty = (%v, %v), want (false, false)", v, ok)
	}
}

func TestIntSet_PutGetRoundTrip(t *testing.T) {
	s := newIntSet(8)

	s.put(1, true)
	s.put(2, false)
	s.put(3, true)

	cases := []struct {
		k  uint64
		v  bool
		ok bool
	}{
		{1, true, true},
		{2, false, true},
		{3, true, true},
		{4, false, false},
	}

	for _, c := range cases {
		if v, ok := s.get(c.k); v != c.v || ok != c.ok {
			t.Errorf("get(%d) = (%v, %v), want (%v, %v)", c.k, v, ok, c.v, c.ok)
		}
	}
}

func TestIntSet_FalseIsNotAbsent(t *testing.T) {
	s := newIntSet(8)

	s.put(7, false)

	if v, ok := s.get(7); !ok || v {
		t.Fatalf("get(7) = (%v, %v), want (false, true)", v, ok)
	}
}

func TestIntSet_Overwrite(t *testing.T) {
	s := newIntSet(8)

	s.put(5, false)
	s.put(5, true)

	if v, ok := s.get(5); !ok || !v {
		t.Fatalf("get(5) after overwrite = (%v, %v), want (true, true)", v, ok)
	}

	s.put(5, false)

	if v, ok := s.get(5); !ok || v {
		t.Fatalf("get(5) after second overwrite = (%v, %v), want (false, true)", v, ok)
	}

	if s.len() != 1 {
		t.Fatalf("len = %d, want 1", s.len())
	}
}

func TestIntSet_GrowKeepsEntries(t *testing.T) {
	s := newIntSet(8)

	const n = 10000

	for i := uint64(1); i <= n; i++ {
		s.put(splitMix64(uint32(i), uint32(i*3)), i%3 == 0)
	}

	if s.len() != n {
		t.Fatalf("len = %d, want %d", s.len(), n)
	}

	for i := uint64(1); i <= n; i++ {
		v, ok := s.get(splitMix64(uint32(i), uint32(i*3)))

		if !ok || v != (i%3 == 0) {
			t.Fatalf("get(#%d) = (%v, %v), want (%v, true)", i, v, ok, i%3 == 0)
		}
	}
}

func TestIntSet_ProbeChainAcrossCollisions(t *testing.T) {
	s := newIntSet(8)

	base := uint64(0x10)

	for j := uint64(0); j < 6; j++ {
		s.put(base+j<<32, j%2 == 0)
	}

	for j := uint64(0); j < 6; j++ {
		if v, ok := s.get(base + j<<32); !ok || v != (j%2 == 0) {
			t.Errorf("get(collision #%d) = (%v, %v), want (%v, true)", j, v, ok, j%2 == 0)
		}
	}
}
