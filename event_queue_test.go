package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestEventQueue_RunsInFIFOOrder(t *testing.T) {
	q := newEventQueue()

	var got []int

	for i := 0; i < 100; i++ {
		i := i
		q.post(func() { got = append(got, i) })
	}

	q.close()

	if len(got) != 100 {
		t.Fatalf("ran %d events, want 100", len(got))
	}

	for i, v := range got {
		if v != i {
			t.Fatalf("event %d ran out of order: got %d", i, v)
		}
	}
}

func TestEventQueue_CloseDrainsPending(t *testing.T) {
	q := newEventQueue()

	var ran atomic.Int64

	for i := 0; i < 1000; i++ {
		q.post(func() { ran.Add(1) })
	}

	q.close()

	if n := ran.Load(); n != 1000 {
		t.Fatalf("close returned with %d/1000 events drained", n)
	}
}

func TestEventQueue_ConcurrentPosters(t *testing.T) {
	q := newEventQueue()

	var ran atomic.Int64
	var wg sync.WaitGroup

	for p := 0; p < 8; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				q.post(func() { ran.Add(1) })
			}
		}()
	}

	wg.Wait()
	q.close()

	if n := ran.Load(); n != 8*500 {
		t.Fatalf("ran %d events, want %d", n, 8*500)
	}
}
