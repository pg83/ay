//go:build !aychaos

package main

// ChaosPoint is a fault seam. A call site hands it the result it has just
// got and carries on with whatever comes back. The production build hands
// every result back untouched, so the compiler folds the seam away; the test
// build (chaos_on.go) breaks what AY_CHAOS asks for.
type ChaosPoint struct{}

func newChaosFault(string) ChaosPoint {
	return ChaosPoint{}
}

func newChaosNumber(string) ChaosPoint {
	return ChaosPoint{}
}

func newChaosText(string) ChaosPoint {
	return ChaosPoint{}
}

func (ChaosPoint) fire() bool {
	return false
}

func (ChaosPoint) number(got uint64) uint64 {
	return got
}

func (ChaosPoint) text(got string) string {
	return got
}

func chaosSwap[T any](_ ChaosPoint, got, _ T) T {
	return got
}
