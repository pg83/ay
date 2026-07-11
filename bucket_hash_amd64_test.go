//go:build amd64

package main

import (
	"math/rand"
	"strconv"
	"testing"
)

var (
	bucketHashBenchSum uint64
	bucketHashBenchXor uint64
)

func TestBucketMix64MatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(2))

	for n := 0; n <= 1024; n++ {
		elems := make([]VFS, n)

		for i := range elems {
			elems[i] = VFS(rng.Uint32())
		}

		sum, xr := bucketMix64Scalar(elems)

		if useBucketHashAVX2 {
			avxSum, avxXor := bucketMix64AVX2(elems)

			if sum != avxSum || xr != avxXor {
				t.Fatalf("avx2 n=%d: scalar (%d,%d) != vector (%d,%d)", n, sum, xr, avxSum, avxXor)
			}
		}

		if useBucketHashAVX512 {
			avxSum, avxXor := bucketMix64AVX512(elems)

			if sum != avxSum || xr != avxXor {
				t.Fatalf("avx512 n=%d: scalar (%d,%d) != vector (%d,%d)", n, sum, xr, avxSum, avxXor)
			}
		}
	}
}

func BenchmarkBucketMix64(b *testing.B) {
	for _, n := range bucketHashBenchSizes() {
		elems := make([]VFS, n)

		for i := range elems {
			elems[i] = VFS(i*2654435761 + 12345)
		}

		benchBucketMix64(b, "scalar/"+strconv.Itoa(n), elems, bucketMix64Scalar)

		if useBucketHashAVX2 {
			benchBucketMix64(b, "avx2/"+strconv.Itoa(n), elems, bucketMix64AVX2)
		}

		if useBucketHashAVX512 {
			benchBucketMix64(b, "avx512/"+strconv.Itoa(n), elems, bucketMix64AVX512)
		}
	}
}

func bucketHashBenchSizes() []int {
	sizes := make([]int, 41)

	for i := range sizes {
		sizes[i] = i
	}

	return append(sizes, 48, 64, 96, 128, 192, 256, 512, 1024, 4096)
}

func benchBucketMix64(b *testing.B, name string, elems []VFS, mix func([]VFS) (uint64, uint64)) {
	b.Run(name, func(b *testing.B) {
		var sum, xr uint64

		for b.Loop() {
			s, x := mix(elems)

			sum += s
			xr ^= x
		}

		bucketHashBenchSum = sum
		bucketHashBenchXor = xr
		b.ReportMetric(float64(len(elems)*b.N)/float64(b.Elapsed().Nanoseconds()), "elem/ns")
	})
}
