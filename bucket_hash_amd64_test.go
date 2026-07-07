//go:build amd64

package main

import (
	"math/rand"
	"strconv"
	"testing"
)

func bucketAccumScalar(elems []VFS) (sum, xr, sq uint32) {
	for _, v := range elems {
		x := uint32(v)

		sum += x
		xr ^= x
		sq += x * x
	}

	return sum, xr, sq
}

func TestBucketAccumAVX2_MatchesScalar(t *testing.T) {
	if !useBucketHashAVX2 {
		t.Skip("no AVX2")
	}

	rng := rand.New(rand.NewSource(1))

	for _, n := range []int{8, 16, 24, 64, 120, 256, 1024} {
		elems := make([]VFS, n)

		for i := range elems {
			elems[i] = VFS(rng.Uint32())
		}

		s0, x0, q0 := bucketAccumScalar(elems)
		s1, x1, q1 := bucketAccumAVX2(&elems[0], n)

		if s0 != s1 || x0 != x1 || q0 != q1 {
			t.Fatalf("n=%d: scalar (%d,%d,%d) != avx2 (%d,%d,%d)", n, s0, x0, q0, s1, x1, q1)
		}
	}
}

func BenchmarkBucketAccum(b *testing.B) {
	for _, n := range []int{8, 16, 32, 64, 256} {
		elems := make([]VFS, n)

		for i := range elems {
			elems[i] = VFS(i*2654435761 + 12345)
		}

		b.Run("scalar/"+strconv.Itoa(n), func(b *testing.B) {
			var s uint32

			for b.Loop() {
				a, x, q := bucketAccumScalar(elems)

				s += a + x + q
			}

			_ = s
		})

		if useBucketHashAVX2 {
			b.Run("avx2/"+strconv.Itoa(n), func(b *testing.B) {
				var s uint32

				for b.Loop() {
					a, x, q := bucketAccumAVX2(&elems[0], n&^7)

					s += a + x + q
				}

				_ = s
			})
		}
	}
}
