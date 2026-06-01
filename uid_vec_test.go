package main

import (
	"sync"
	"testing"
)

func TestPageOffset_GeometricLayout(t *testing.T) {
	// Page p must cover exactly the contiguous id range [2^p-1, 2^(p+1)-2] at
	// offsets 0..2^p-1, with no gaps or overlaps.
	wantPage := 0
	wantStart := int64(0)

	for id := int64(0); id < 1<<16; id++ {
		size := int64(1) << uint(wantPage)

		if id == wantStart+size {
			wantPage++
			wantStart = id
			size = int64(1) << uint(wantPage)
		}

		p, off := pageOffset(id)

		if p != wantPage {
			t.Fatalf("id=%d page=%d, want %d", id, p, wantPage)
		}

		if off != id-wantStart {
			t.Fatalf("id=%d off=%d, want %d", id, off, id-wantStart)
		}

		if off < 0 || off >= size {
			t.Fatalf("id=%d off=%d out of page size %d", id, off, size)
		}
	}
}

func TestUidVec_SetGetRoundTrip(t *testing.T) {
	var v uidVec
	const n = 1 << 14

	for id := int64(0); id < n; id++ {
		v.set(id, UID{Hi: uint64(id) * 2, Lo: uint64(id)*2 + 1})
	}

	for id := int64(0); id < n; id++ {
		got := v.get(id)
		want := UID{Hi: uint64(id) * 2, Lo: uint64(id)*2 + 1}

		if got != want {
			t.Fatalf("get(%d) = %+v, want %+v", id, got, want)
		}
	}
}

func TestUidVec_LazyPageAllocation(t *testing.T) {
	var v uidVec

	for _, page := range v.pages {
		if page != nil {
			t.Fatal("fresh uidVec has a non-nil page")
		}
	}

	// Writing id 100 must touch only the pages up to the one containing 100,
	// and each allocated page must have the geometric length 1<<p.
	v.set(100, UID{Hi: 1})
	topPage, _ := pageOffset(100)

	for p, page := range v.pages {
		switch {
		case p < topPage:
			// lower pages stay nil until an id lands in them
			if page != nil {
				t.Fatalf("page %d allocated without a write into it", p)
			}
		case p == topPage:
			if int64(len(page)) != int64(1)<<uint(p) {
				t.Fatalf("page %d len = %d, want %d", p, len(page), int64(1)<<uint(p))
			}
		default:
			if page != nil {
				t.Fatalf("page %d above the written id is allocated", p)
			}
		}
	}
}

// TestUidVec_ConcurrentGetDuringSet models the gen/executor pattern: one writer
// fills ids in order while readers concurrently read already-written ids. Run
// under -race to catch any torn page-table access.
func TestUidVec_ConcurrentGetDuringSet(t *testing.T) {
	var v uidVec
	const n = 1 << 13

	// Pre-seed id 0 so readers always have something resolved to read.
	v.set(0, UID{Hi: 0, Lo: 0})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = v.get(0)
			}
		}()
	}

	for id := int64(1); id < n; id++ {
		v.set(id, UID{Hi: uint64(id)})
	}

	close(stop)
	wg.Wait()

	for id := int64(0); id < n; id++ {
		if got := v.get(id); got.Hi != uint64(id) {
			t.Fatalf("get(%d).Hi = %d, want %d", id, got.Hi, id)
		}
	}
}
