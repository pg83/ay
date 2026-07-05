package main

import (
	"fmt"
	"time"
)

func bucketHashMix64(elems []VFS) uint64 {
	h := mix64(uint64(len(elems)))

	for _, v := range elems {
		h += mix64(uint64(v))
	}

	if h == 0 {
		h = 1
	}

	return h
}

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

		fmt.Printf("  %-18s %.3f ns/elem  (%.2f ms/pass, sink=%#016x)\n",
			name, float64(best.Nanoseconds())/float64(total),
			float64(best.Microseconds())/1000, sink)

		return best
	}

	fmt.Println("=== throughput (min of 15 passes, interleaved) ===")

	pairAsOne := func(elems []VFS) uint64 {
		h1, h2 := bucketHash(elems)

		return h1 ^ h2
	}

	var mx, pair time.Duration

	for i := 0; i < 3; i++ {
		mx = bench("mix64 (old)", bucketHashMix64)
		pair = bench("pair (prod)", pairAsOne)
	}

	fmt.Printf("=> pair hash %.2fx faster than mix64\n\n", float64(mx)/float64(pair))

	fmt.Println("=== pair collision stress ===")

	type pairVal struct {
		h2    uint64
		ident uint64
	}

	seen := make(map[uint64]pairVal, 1<<24)
	elems := make([]VFS, 0, maxLen)

	start := time.Now()
	lastPrint := start

	var count, h1Hits uint64

	for {
		n := int(next() % (maxLen + 1))
		elems = elems[:0]

		ident := uint64(0)

		for i := 0; i < n; i++ {
			id := uint32(next() % (maxVal + 1))
			elems = append(elems, VFS(id)<<1)
			ident += mix64(uint64(id))
		}

		h1, h2 := bucketHash(elems)
		count++

		if prev, ok := seen[h1]; ok {
			if prev.ident != ident {
				h1Hits++

				if prev.h2 == h2 {
					fmt.Printf("PAIR COLLISION after %d sequences (%d distinct, %.1fs): h1=%#016x h2=%#016x\n",
						count, len(seen), time.Since(start).Seconds(), h1, h2)

					return 1
				}
			}
		} else {
			seen[h1] = pairVal{h2: h2, ident: ident}
		}

		if now := time.Now(); now.Sub(lastPrint) >= time.Second {
			lastPrint = now

			fmt.Printf("  ... %d sequences, %d distinct, %d h1-collisions, no pair collision, %.0fs\n",
				count, len(seen), h1Hits, now.Sub(start).Seconds())
		}
	}
}
