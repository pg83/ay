package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

const (
	splBenchArrays = 10000
	splBenchMaxLen = 10000
	splBenchMaxEl  = 10000000
	splBenchMerges = 100000
	splBenchVerify = 3000
	fnvOffset      = 1469598103934665603
	fnvPrime       = 1099511628211
)

type spliceAlgo struct {
	name string
	fn   func(gen []uint32, epoch uint32, win []VFS, block []VFS, k int) int
}

func benchScalar(gen []uint32, epoch uint32, win []VFS, block []VFS, k int) int {
	for _, v := range win {
		if gen[v] == epoch {
			continue
		}

		gen[v] = epoch
		block[k] = v
		k++
	}

	return k
}

func cmdPerfSplice(_ GlobalFlags, args []string) int {
	defer startProfilesFromEnv()()

	merges := splBenchMerges
	maxEl := splBenchMaxEl

	if len(args) >= 1 {
		merges = throw2(strconv.Atoi(args[0]))
	}

	if len(args) >= 2 {
		maxEl = throw2(strconv.Atoi(args[1]))
	}

	if maxEl < splBenchMaxLen {
		throwFmt("perf splice: max-element %d must be >= max array length %d", maxEl, splBenchMaxLen)
	}

	return perfSplice(merges, maxEl)
}

func perfSplice(merges, maxEl int) int {
	rng := rand.New(rand.NewSource(1))

	arrs := make([][]VFS, splBenchArrays)
	seen := make([]int32, maxEl+1)
	totalElems := 0

	for a := range arrs {
		l := 1 + rng.Intn(splBenchMaxLen)
		arr := make([]VFS, 0, l)
		mark := int32(a + 1)

		for len(arr) < l {
			v := 1 + rng.Intn(maxEl)

			if seen[v] != mark {
				seen[v] = mark
				arr = append(arr, VFS(v))
			}
		}

		arrs[a] = arr
		totalElems += l
	}

	pairs := make([][2]int32, merges)
	elemOps := 0

	for i := range pairs {
		x := int32(rng.Intn(splBenchArrays))
		y := int32(rng.Intn(splBenchArrays))
		pairs[i] = [2]int32{x, y}
		elemOps += len(arrs[x]) + len(arrs[y])
	}

	gen := make([]uint32, maxEl+1)
	block := make([]VFS, 2*splBenchMaxLen)

	algos := []spliceAlgo{
		{"scalar", benchScalar},
		{"avx512", benchAVX512},
		{"prefetch", benchPrefetch},
	}

	fmt.Printf("splice bench: %d arrays, %d total elements, max-element %d (gen %.1f MB), %d merges, %d element-ops\n",
		splBenchArrays, totalElems, maxEl, float64(len(gen)*4)/(1<<20), merges, elemOps)

	verifySplice(arrs, pairs, gen, block, algos)

	for _, al := range algos {
		for i := range gen {
			gen[i] = 0
		}

		var epoch uint32
		var sink uint64

		start := time.Now()

		for _, p := range pairs {
			epoch++

			k := al.fn(gen, epoch, arrs[p[0]], block, 0)
			k = al.fn(gen, epoch, arrs[p[1]], block, k)
			sink += uint64(k)
		}

		dur := time.Since(start)
		nsPerElem := float64(dur.Nanoseconds()) / float64(elemOps)

		fmt.Printf("  %-9s %9.2f ms   %6.3f ns/elem   sink=%d\n",
			al.name, float64(dur.Nanoseconds())/1e6, nsPerElem, sink)
	}

	return 0
}

func verifySplice(arrs [][]VFS, pairs [][2]int32, gen []uint32, block []VFS, algos []spliceAlgo) {
	vn := len(pairs)

	if vn > splBenchVerify {
		vn = splBenchVerify
	}

	ref := spliceChecksum(algos[0].fn, arrs, pairs[:vn], gen, block)

	for _, al := range algos[1:] {
		got := spliceChecksum(al.fn, arrs, pairs[:vn], gen, block)

		if got != ref {
			throwFmt("perf splice: %s diverges from %s (checksum %d != %d over %d merges)",
				al.name, algos[0].name, got, ref, vn)
		}
	}

	fmt.Printf("  verified: 3 algorithms byte-identical over %d merges\n", vn)
}

func spliceChecksum(fn func([]uint32, uint32, []VFS, []VFS, int) int, arrs [][]VFS, pairs [][2]int32, gen []uint32, block []VFS) uint64 {
	for i := range gen {
		gen[i] = 0
	}

	var epoch uint32
	h := uint64(fnvOffset)

	for _, p := range pairs {
		epoch++

		k := fn(gen, epoch, arrs[p[0]], block, 0)
		k = fn(gen, epoch, arrs[p[1]], block, k)

		h = (h ^ uint64(k)) * fnvPrime

		for _, v := range block[:k] {
			h = (h ^ uint64(v)) * fnvPrime
		}
	}

	return h
}
