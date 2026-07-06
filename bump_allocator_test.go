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

	var got [][]int
	var want [][]int

	for i := 0; i < 50; i++ {
		k := (i % 7) + 1
		vals := make([]int, k)

		for j := range vals {
			vals[j] = i*100 + j
		}

		r := a.alloc(k)
		wrote := fill(r, vals...)
		got = append(got, r[:wrote:wrote]) // retained subslice keeps its backing alive
		a.commit(wrote)

		want = append(want, vals)
	}

	for i := range want {
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("region %d elem %d = %d, want %d", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func TestBumpAllocatorFixedChunkSize(t *testing.T) {
	a := newBumpAllocator[byte](8)

	for i := 0; i < 4; i++ {
		r := a.alloc(1)

		if len(r) != bumpChunkBytes {
			t.Fatalf("alloc %d: fresh chunk len = %d, want fixed %d", i, len(r), bumpChunkBytes)
		}

		a.commit(len(r)) // exhaust the chunk so the next alloc opens a fresh one
	}
}

func TestBumpAllocatorChunkByteBudget(t *testing.T) {
	a := newBumpAllocator[uint64](8)

	r := a.alloc(1)

	if len(r) != bumpChunkBytes/8 {
		t.Fatalf("uint64 fresh chunk len = %d, want %d", len(r), bumpChunkBytes/8)
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
