package main

import (
	"fmt"
	"time"
)

// bucketHashMix64 is the original per-element-avalanche multiset hash
// (MSet-Add-Hash): sum of mix64(v). Benchmarked against the current bucketHash.
func bucketHashMix64(elems []VFS) uint64 {
	h := mix64(uint64(len(elems)))

	for _, v := range elems {
		h += mix64(uint64(v.strID()))
	}

	if h == 0 {
		h = 1
	}

	return h
}

// hSumSq is the two-moment variant (Σv, Σv²) without the xor invariant — faster
// but not collision-safe (e.g. {1,5,6} and {2,3,7} share Σ and Σ²), kept only to
// mark the speed/safety frontier against the production bucketHash.
func hSumSq(elems []VFS) uint64 {
	sum := uint32(len(elems))
	sq := uint32(0)

	for _, v := range elems {
		sum += uint32(v)
		sq += uint32(v) * uint32(v)
	}

	return splitMix64(sum, sq)
}

// cmdPerfBucketHash first benchmarks the production bucketHash against the old
// mix64 one and the unsafe two-moment lower bound over random sequences (clean
// field, ns/element), then stress-tests bucketHash for collisions.
func cmdPerfBucketHash(_ GlobalFlags, args []string) int {
	const maxLen = 10000
	const maxVal = 100000
	const totalTarget = 1_000_000

	rng := uint64(0x9e3779b97f4a7c15)
	next := func() uint64 {
		rng ^= rng << 13
		rng ^= rng >> 7
		rng ^= rng << 17

		return rng
	}

	var seqs [][]VFS

	total := 0

	for total < totalTarget {
		n := int(next() % (maxLen + 1))
		s := make([]VFS, n)

		for j := range s {
			s[j] = VFS(uint32(next()%(maxVal+1)) << 1)
		}

		seqs = append(seqs, s)
		total += n
	}

	fmt.Printf("corpus: %d sequences, %d elements\n", len(seqs), total)

	bench := func(name string, hash func([]VFS) uint64) time.Duration {
		best := time.Duration(1) << 62

		var sink uint64

		for pass := 0; pass < 15; pass++ {
			t := time.Now()

			for _, s := range seqs {
				sink ^= hash(s)
			}

			if d := time.Since(t); d < best {
				best = d
			}
		}

		fmt.Printf("  %-16s %.3f ns/elem  (%.2f ms/pass, sink=%#016x)\n",
			name, float64(best.Nanoseconds())/float64(total),
			float64(best.Microseconds())/1000, sink)

		return best
	}

	fmt.Println("=== throughput (min of 15 passes, interleaved) ===")

	var mx, sq, prod time.Duration

	for i := 0; i < 3; i++ {
		mx = bench("mix64", bucketHashMix64)
		sq = bench("sum+sq (unsafe)", hSumSq)
		prod = bench("bucketHash (prod)", bucketHash)
	}

	fmt.Printf("=> bucketHash %.2fx faster than mix64 (2-moment lower bound %.2fx)\n\n",
		float64(mx)/float64(prod), float64(mx)/float64(sq))

	fmt.Println("=== collision stress (bucketHash) ===")

	seen := make(map[uint64]uint64, 1<<24)
	elems := make([]VFS, 0, maxLen)

	start := time.Now()
	lastPrint := start

	var count uint64

	for {
		n := int(next() % (maxLen + 1))
		elems = elems[:0]

		ident := uint64(0)

		for i := 0; i < n; i++ {
			id := uint32(next() % (maxVal + 1))
			elems = append(elems, VFS(id)<<1)
			ident += mix64(uint64(id))
		}

		h := bucketHash(elems)
		count++

		if prev, ok := seen[h]; ok {
			if prev != ident {
				fmt.Printf("COLLISION after %d sequences (%d distinct, %.1fs): bucketHash=%#016x\n",
					count, len(seen), time.Since(start).Seconds(), h)

				return 0
			}
		} else {
			seen[h] = ident
		}

		if now := time.Now(); now.Sub(lastPrint) >= time.Second {
			lastPrint = now

			fmt.Printf("  ... %d sequences, %d distinct, no collision, %.0fs\n",
				count, len(seen), now.Sub(start).Seconds())
		}
	}
}
