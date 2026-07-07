//go:build amd64

package main

import (
	"math/rand"
	"testing"
)

func uidAccumScalar(es []uint64) (sum, xor, sq uint64) {
	for _, e := range es {
		sum += e
		xor ^= e
		sq += e * e
	}

	return sum, xor, sq
}

func TestUidAccumAVX2_MatchesScalar(t *testing.T) {
	if !useBucketHashAVX2 {
		t.Skip("no AVX2")
	}

	rng := rand.New(rand.NewSource(2))

	for _, n := range []int{4, 8, 12, 64, 128, 1024} {
		es := make([]uint64, n)

		for i := range es {
			es[i] = rng.Uint64()
		}

		s0, x0, q0 := uidAccumScalar(es)
		s1, x1, q1 := uidAccumAVX2(&es[0], n)

		if s0 != s1 || x0 != x1 || q0 != q1 {
			t.Fatalf("n=%d: scalar (%x,%x,%x) != avx2 (%x,%x,%x)", n, s0, x0, q0, s1, x1, q1)
		}
	}
}

func TestUidAccum_MatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(3))

	for _, n := range []int{0, 1, 5, 7, 8, 9, 33, 1000} {
		es := make([]uint64, n)

		for i := range es {
			es[i] = rng.Uint64()
		}

		s0, x0, q0 := uidAccumScalar(es)
		s1, x1, q1 := uidAccum(es)

		if s0 != s1 || x0 != x1 || q0 != q1 {
			t.Fatalf("n=%d: scalar (%x,%x,%x) != uidAccum (%x,%x,%x)", n, s0, x0, q0, s1, x1, q1)
		}
	}
}
