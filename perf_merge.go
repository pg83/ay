package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"
)

type flatEntry struct {
	key uint64
	v   []uint32
}

type flatCache struct {
	slots []flatEntry
	mask  uint64
}

func pow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

func newFlatCache(n int) *flatCache {
	n = pow2(n)
	return &flatCache{slots: make([]flatEntry, n), mask: uint64(n - 1)}
}

func (c *flatCache) get(k uint64) (*flatEntry, bool) {
	e := &c.slots[mix64(k)&c.mask]
	if e.key == k {
		return e, true
	}
	return nil, false
}

func (c *flatCache) put(k uint64, v []uint32) {
	e := &c.slots[mix64(k)&c.mask]
	e.key = k
	e.v = v
}

func cmdPerfMerge(_ GlobalFlags, args []string) int {
	defer startProfilesFromEnv()()

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ay dev perf merge <dump-prefix>")

		return 2
	}

	lruN := 1 << 16
	if len(args) >= 2 {
		lruN = throw2(strconv.Atoi(args[1]))
	}

	return perfMerge(args[0], lruN)
}

func loadDumpLists(path string) [][]uint32 {
	f := throw2(os.Open(path))
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<25)

	out := make([][]uint32, 0, 1<<17)

	for sc.Scan() {
		b := sc.Bytes()

		var row []uint32
		var x uint32
		has := false

		for _, c := range b {
			if c == ' ' {
				if has {
					row = append(row, x)
					x = 0
					has = false
				}
			} else {
				x = x*10 + uint32(c-'0')
				has = true
			}
		}

		if has {
			row = append(row, x)
		}

		out = append(out, row)
	}

	throw(sc.Err())

	return out
}

func mergeTwo(a, b []uint32) []uint32 {
	out := make([]uint32, 0, len(a)+len(b))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			out = append(out, a[i])
			i++
		case a[i] > b[j]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}

	out = append(out, a[i:]...)
	out = append(out, b[j:]...)

	return out
}

func cseHash(refs []uint32) uint64 {
	h := uint64(1469598103934665603)
	for _, r := range refs {
		h = (h ^ uint64(r)) * 1099511628211
	}
	return h
}

func siftDownU64(h []uint64, i int) {
	n := len(h)

	for {
		l := 2*i + 1
		if l >= n {
			return
		}

		m := l
		if r := l + 1; r < n && h[r] < h[l] {
			m = r
		}

		if h[i] <= h[m] {
			return
		}

		h[i], h[m] = h[m], h[i]
		i = m
	}
}

func perfMerge(prefix string, lruN int) int {
	closures := loadDumpLists(prefix + ".closures")
	builds := loadDumpLists(prefix + ".builds")

	var universe uint32
	var mergeElems int64
	maxN := 1

	for _, cl := range closures {
		for _, v := range cl {
			if v > universe {
				universe = v
			}
		}
	}

	for _, b := range builds {
		if len(b) > maxN {
			maxN = len(b)
		}

		for _, cid := range b {
			mergeElems += int64(len(closures[cid]))
		}
	}

	fmt.Printf("merge bench: %d closures, %d builds, universe=%d, merge-elems=%d, maxChildren=%d\n",
		len(closures), len(builds), universe, mergeElems, maxN)

	gen := make([]uint16, universe+1)
	block := make([]uint32, universe+1)
	var epoch uint16

	genSplice := func(b []uint32) []uint32 {
		epoch++
		if epoch == 0 {
			for i := range gen {
				gen[i] = 0
			}
			epoch = 1
		}

		k := 0
		for _, cid := range b {
			for _, v := range closures[cid] {
				if gen[v] != epoch {
					gen[v] = epoch
					block[k] = v
					k++
				}
			}
		}

		return block[:k]
	}

	sorted := make([][]uint32, len(closures))
	for i, cl := range closures {
		c := append([]uint32(nil), cl...)
		sort.Slice(c, func(a, b int) bool { return c[a] < c[b] })
		sorted[i] = c
	}

	outB := make([]uint32, universe+1)
	posBuf := make([]int, maxN)
	heapBuf := make([]uint64, 0, maxN)

	mergeSorted := func(b []uint32) []uint32 {
		n := len(b)
		if n == 0 {
			return outB[:0]
		}

		pos := posBuf[:n]
		h := heapBuf[:0]

		for i := 0; i < n; i++ {
			cl := sorted[b[i]]
			pos[i] = 0
			if len(cl) > 0 {
				pos[i] = 1
				h = append(h, uint64(cl[0])<<20|uint64(i))
			}
		}

		for i := len(h)/2 - 1; i >= 0; i-- {
			siftDownU64(h, i)
		}

		k := 0
		last := ^uint32(0)

		for len(h) > 0 {
			top := h[0]
			v := uint32(top >> 20)
			ci := int(top & 0xFFFFF)

			if v != last {
				outB[k] = v
				k++
				last = v
			}

			cl := sorted[b[ci]]
			if pos[ci] < len(cl) {
				h[0] = uint64(cl[pos[ci]])<<20 | uint64(ci)
				pos[ci]++
			} else {
				h[0] = h[len(h)-1]
				h = h[:len(h)-1]
			}

			siftDownU64(h, 0)
		}

		return outB[:k]
	}

	cseMemo := map[uint64][]uint32{}
	var cseWork int64
	var cseHits, cseCompute int64

	var cse func(refs []uint32) []uint32
	cse = func(refs []uint32) []uint32 {
		n := len(refs)
		if n == 1 {
			return sorted[refs[0]]
		}

		key := cseHash(refs)
		if r, ok := cseMemo[key]; ok {
			cseHits++
			return r
		}
		cseCompute++

		p := 1
		for p*2 < n {
			p *= 2
		}

		l := cse(refs[:p])
		r := cse(refs[p:])
		cseWork += int64(len(l) + len(r))
		res := mergeTwo(l, r)
		cseMemo[key] = res

		return res
	}

	sbuf := make([]uint32, maxN)
	cseRun := func(b []uint32) []uint32 {
		if len(b) == 0 {
			return nil
		}
		s := sbuf[:len(b)]
		copy(s, b)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return cse(s)
	}

	verifyMerge(builds, genSplice, mergeSorted)
	verifyCSE(builds, genSplice, cseRun)

	var sinkA, sinkB, sinkC int

	t := time.Now()
	for _, b := range builds {
		sinkA += len(genSplice(b))
	}
	durA := time.Since(t)

	t = time.Now()
	for _, b := range builds {
		sinkB += len(mergeSorted(b))
	}
	durB := time.Since(t)

	// reset memo so timing measures a full run
	cseMemo = map[uint64][]uint32{}
	cseWork, cseHits, cseCompute = 0, 0, 0

	t = time.Now()
	for _, b := range builds {
		sinkC += len(cseRun(b))
	}
	durC := time.Since(t)

	var memoElems int64
	for _, v := range cseMemo {
		memoElems += int64(len(v))
	}

	genD := make([]uint16, universe+1)
	pairGen := make([]uint16, universe+1)
	pairScratch := make([]uint32, 0, 1<<16)
	lru := newFlatCache(lruN)
	var epD, pairEp uint16
	var spliceWork, computeWork, dHits, dMiss int64

	runD := func(b []uint32) []uint32 {
		epD++
		if epD == 0 {
			for i := range genD {
				genD[i] = 0
			}
			epD = 1
		}

		k := 0
		n := len(b)
		i := 0

		for i < n {
			if i+1 < n {
				key := (uint64(b[i])+1)<<32 | (uint64(b[i+1]) + 1)

				e, ok := lru.get(key)

				if ok && e.v != nil {
					dHits++
					for _, v := range e.v {
						spliceWork++
						if genD[v] != epD {
							genD[v] = epD
							block[k] = v
							k++
						}
					}
					i += 2
					continue
				}

				if ok {
					dMiss++
					pairEp++
					if pairEp == 0 {
						for j := range pairGen {
							pairGen[j] = 0
						}
						pairEp = 1
					}

					u := pairScratch[:0]
					for _, v := range closures[b[i]] {
						computeWork++
						if pairGen[v] != pairEp {
							pairGen[v] = pairEp
							u = append(u, v)
						}
					}
					for _, v := range closures[b[i+1]] {
						computeWork++
						if pairGen[v] != pairEp {
							pairGen[v] = pairEp
							u = append(u, v)
						}
					}

					e.v = append([]uint32(nil), u...)
					pairScratch = u[:0]

					for _, v := range e.v {
						spliceWork++
						if genD[v] != epD {
							genD[v] = epD
							block[k] = v
							k++
						}
					}
					i += 2
					continue
				}

				dMiss++
				lru.put(key, nil)
				for _, v := range closures[b[i]] {
					spliceWork++
					if genD[v] != epD {
						genD[v] = epD
						block[k] = v
						k++
					}
				}
				i++
				continue
			}

			for _, v := range closures[b[i]] {
				spliceWork++
				if genD[v] != epD {
					genD[v] = epD
					block[k] = v
					k++
				}
			}
			i++
		}

		return block[:k]
	}

	verifyUnsorted(builds, genSplice, runD)

	lru = newFlatCache(lruN)
	epD, pairEp = 0, 0
	spliceWork, computeWork, dHits, dMiss = 0, 0, 0, 0

	var sinkD int
	t = time.Now()
	for _, b := range builds {
		sinkD += len(runD(b))
	}
	durD := time.Since(t)

	nsPer := func(d time.Duration) float64 { return float64(d.Nanoseconds()) / float64(mergeElems) }
	msOf := func(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1e6 }

	fmt.Printf("  gen-splice    %9.1f ms   %6.3f ns/elem   sink=%d\n", msOf(durA), nsPer(durA), sinkA)
	fmt.Printf("  sorted-merge  %9.1f ms   %6.3f ns/elem   sink=%d\n", msOf(durB), nsPer(durB), sinkB)
	fmt.Printf("  union-CSE     %9.1f ms   %6.3f ns/elem   sink=%d\n", msOf(durC), nsPer(durC), sinkC)
	fmt.Printf("  CSE: mergeElems=%d (%.0f%% of baseline) nodes=%d hits=%d (%.1f%% hit) memoElems=%d (%.0f MB)\n",
		cseWork, 100*float64(cseWork)/float64(mergeElems), cseCompute, cseHits,
		100*float64(cseHits)/float64(cseHits+cseCompute), memoElems, float64(memoElems*4)/(1<<20))

	fmt.Printf("  pair-LRU(%d)  %9.1f ms   %6.3f ns/elem   sink=%d\n", lruN, msOf(durD), nsPer(durD), sinkD)
	fmt.Printf("  pair-LRU: spliceWork=%d computeWork=%d total=%d (%.0f%% of baseline) hits=%d miss=%d (%.1f%% hit)\n",
		spliceWork, computeWork, spliceWork+computeWork,
		100*float64(spliceWork+computeWork)/float64(mergeElems), dHits, dMiss,
		100*float64(dHits)/float64(dHits+dMiss))

	lazyMaterialize(prefix, closures, builds, mergeElems, universe)

	return 0
}

func lazyMaterialize(prefix string, closures, builds [][]uint32, baseOps int64, universe uint32) {
	keys := loadDumpLists(prefix + ".keys")

	referenced := make([]bool, len(closures))
	for _, b := range builds {
		for _, c := range b {
			referenced[c] = true
		}
	}

	materialized := make([]bool, len(closures))
	var storedBase, storedLazy int64
	nMat := 0

	for g := range closures {
		storedBase += int64(len(closures[g]))

		key := uint32(1)
		if len(keys[g]) > 0 {
			key = keys[g][0]
		}

		mat := key%2 == 0 || len(builds[g]) == 0 || !referenced[g]
		materialized[g] = mat

		if mat {
			nMat++
			storedLazy += int64(len(closures[g]))
		} else {
			storedLazy += int64(1 + len(builds[g]))
		}
	}

	var opsLazy, subsumeHits int64
	gen := make([]int32, universe+1)
	var ep int32

	var expand func(c uint32)
	expand = func(c uint32) {
		if len(closures[c]) == 0 {
			return
		}

		self := closures[c][0]
		if gen[self] == ep {
			subsumeHits++
			return
		}

		if materialized[c] {
			for _, v := range closures[c] {
				opsLazy++
				gen[v] = ep
			}
			return
		}

		gen[self] = ep
		opsLazy++
		for _, gc := range builds[c] {
			expand(gc)
		}
	}

	t := time.Now()
	for g := range closures {
		if !materialized[g] || len(builds[g]) == 0 {
			continue
		}
		ep++
		gen[closures[g][0]] = ep
		for _, c := range builds[g] {
			expand(c)
		}
	}
	dur := time.Since(t)

	fmt.Printf("  lazy-mat: materialized=%d/%d (%.0f%%)  memory: base=%d lazy=%d (%.1f%% saved)\n",
		nMat, len(closures), 100*float64(nMat)/float64(len(closures)),
		storedBase, storedLazy, 100*(1-float64(storedLazy)/float64(storedBase)))
	fmt.Printf("  lazy-mat: build-ops base=%d lazy=%d (%.0f%% of baseline) subsumeHits=%d in %.0f ms\n",
		baseOps, opsLazy, 100*float64(opsLazy)/float64(baseOps), subsumeHits, float64(dur.Nanoseconds())/1e6)
}

func verifyUnsorted(builds [][]uint32, a, d func([]uint32) []uint32) {
	lim := len(builds)
	if lim > 3000 {
		lim = 3000
	}

	for i := 0; i < lim; i++ {
		ra := append([]uint32(nil), a(builds[i])...)
		rd := append([]uint32(nil), d(builds[i])...)

		sort.Slice(ra, func(x, y int) bool { return ra[x] < ra[y] })
		sort.Slice(rd, func(x, y int) bool { return rd[x] < rd[y] })

		if len(ra) != len(rd) {
			throwFmt("perf merge pair-LRU: build %d size %d != %d", i, len(ra), len(rd))
		}

		for j := range ra {
			if ra[j] != rd[j] {
				throwFmt("perf merge pair-LRU: build %d elem %d: %d != %d", i, j, ra[j], rd[j])
			}
		}
	}

	fmt.Printf("  verified: pair-LRU agrees with gen-splice on %d builds\n", lim)
}

func verifyCSE(builds [][]uint32, a func([]uint32) []uint32, c func([]uint32) []uint32) {
	lim := len(builds)
	if lim > 3000 {
		lim = 3000
	}

	for i := 0; i < lim; i++ {
		ra := append([]uint32(nil), a(builds[i])...)
		rc := append([]uint32(nil), c(builds[i])...)

		sort.Slice(ra, func(x, y int) bool { return ra[x] < ra[y] })

		if len(ra) != len(rc) {
			throwFmt("perf merge CSE: build %d size %d != %d", i, len(ra), len(rc))
		}

		for j := range ra {
			if ra[j] != rc[j] {
				throwFmt("perf merge CSE: build %d elem %d: %d != %d", i, j, ra[j], rc[j])
			}
		}
	}

	fmt.Printf("  verified: union-CSE agrees with gen-splice on %d builds\n", lim)
}

func verifyMerge(builds [][]uint32, a, b func([]uint32) []uint32) {
	lim := len(builds)
	if lim > 3000 {
		lim = 3000
	}

	for i := 0; i < lim; i++ {
		ra := append([]uint32(nil), a(builds[i])...)
		rb := append([]uint32(nil), b(builds[i])...)

		sort.Slice(ra, func(x, y int) bool { return ra[x] < ra[y] })

		if len(ra) != len(rb) {
			throwFmt("perf merge: build %d size %d != %d", i, len(ra), len(rb))
		}

		for j := range ra {
			if ra[j] != rb[j] {
				throwFmt("perf merge: build %d elem %d: %d != %d", i, j, ra[j], rb[j])
			}
		}
	}

	fmt.Printf("  verified: gen-splice and sorted-merge agree on %d builds\n", lim)
}
