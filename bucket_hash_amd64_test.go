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

func TestBucketHashPlatform(t *testing.T) {
	rng := rand.New(rand.NewSource(2))

	for n := 0; n <= 1024; n++ {
		elems := make([]VFS, n)

		for i := range elems {
			elems[i] = VFS(rng.Uint32())
		}

		sum, xr := bucketMix64Reference(elems)
		platformSum, platformXor := bucketHashPlatform(elems)

		if sum != platformSum || xr != platformXor {
			t.Fatalf("n=%d: reference (%d,%d) != platform (%d,%d)", n, sum, xr, platformSum, platformXor)
		}
	}
}

func BenchmarkBucketMix64(b *testing.B) {
	for _, n := range bucketHashBenchSizes() {
		elems := make([]VFS, n)

		for i := range elems {
			elems[i] = VFS(i*2654435761 + 12345)
		}

		benchBucketMix64(b, strconv.Itoa(n), elems, bucketHashPlatform)
	}
}

func bucketMix64Reference(elems []VFS) (sum, xr uint64) {
	for _, v := range elems {
		z := mix64(uint64(v))

		sum += z
		xr ^= z
	}

	return sum, xr
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
