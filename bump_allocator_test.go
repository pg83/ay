package main

import "testing"

func fill[T any](region []T, vals ...T) int {
	for i, v := range vals {
		region[i] = v
	}

	return len(vals)
}

func TestBumpAllocatorAllocAtLeastN(t *testing.T) {
	a := newBumpAllocator[int](4)

	for _, n := range []int{1, 4, 7, 100} {
		r := a.alloc(n)

		if len(r) < n {
			t.Fatalf("alloc(%d) returned region of len %d, want >= %d", n, len(r), n)
		}

		a.commit(0)
	}
}

func TestBumpAllocatorPacksCommittedRegions(t *testing.T) {
	a := newBumpAllocator[int](16)

	type span struct {
		chunk int
		off   int
		n     int
	}

	var spans []span
	var want [][]int

	for i := 0; i < 50; i++ {
		k := (i % 7) + 1
		vals := make([]int, k)

		for j := range vals {
			vals[j] = i*100 + j
		}

		r := a.alloc(k)
		wrote := fill(r, vals...)

		ci := len(a.chunks) - 1
		off := a.off
		a.commit(wrote)

		spans = append(spans, span{chunk: ci, off: off, n: k})
		want = append(want, vals)
	}

	for i, sp := range spans {
		got := a.chunks[sp.chunk][sp.off : sp.off+sp.n]

		for j := range want[i] {
			if got[j] != want[i][j] {
				t.Fatalf("span %d elem %d = %d, want %d", i, j, got[j], want[i][j])
			}
		}
	}
}

func TestBumpAllocatorFixedChunkSize(t *testing.T) {
	a := newBumpAllocator[byte](8)

	for i := 0; i < 4; i++ {
		r := a.alloc(1)
		a.commit(len(r)) // exhaust the chunk so the next alloc opens a fresh one

		if got := len(a.chunks[len(a.chunks)-1]); got != bumpChunkBytes {
			t.Fatalf("chunk %d size = %d, want fixed %d", i, got, bumpChunkBytes)
		}
	}
}

func TestBumpAllocatorOversizedAllocFits(t *testing.T) {
	a := newBumpAllocator[byte](8)

	big := bumpChunkBytes * 3
	r := a.alloc(big)

	if len(r) < big {
		t.Fatalf("alloc(%d) returned len %d, want >= %d", big, len(r), big)
	}

	a.commit(big)
}

func TestBumpAllocatorChunkFitsLargeAlloc(t *testing.T) {
	a := newBumpAllocator[int](4)

	r := a.alloc(100)

	if len(r) < 100 {
		t.Fatalf("alloc(100) returned len %d, want >= 100", len(r))
	}

	n := fill(r, make([]int, 100)...)
	a.commit(n)
}

func TestBumpAllocatorCommitOutOfRangePanics(t *testing.T) {
	a := newBumpAllocator[int](4)
	r := a.alloc(2)

	defer func() {
		if recover() == nil {
			t.Fatal("commit past region length did not panic")
		}
	}()

	a.commit(len(r) + 1)
}
