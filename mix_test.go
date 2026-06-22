package main

import (
	"math/rand"
	"testing"
)

var sinkU64 uint64

// splitMix64 must be a bijection: distinct (p,s) pairs never share a key.
func TestSplitMix64Bijective(t *testing.T) {
	seen := map[uint64][2]uint32{}
	rng := rand.New(rand.NewSource(1))

	for i := 0; i < 500_000; i++ {
		p, s := rng.Uint32(), rng.Uint32()
		k := splitMix64(p, s)

		if prev, ok := seen[k]; ok && prev != [2]uint32{p, s} {
			t.Fatalf("collision: splitMix64%v == splitMix64%v == %#x", prev, [2]uint32{p, s}, k)
		}

		seen[k] = [2]uint32{p, s}
	}
}

// For dense sequential ids (the real workload) the key's low bits must spread
// across the table — what Morton keying lacked. Insert a dense block via open
// addressing and assert the average probe stays near 1 at LF 0.5.
func TestSplitMix64SpreadsDenseIDs(t *testing.T) {
	const n = 1 << 16
	const capacity = 1 << 17 // LF 0.5
	const mask = uint64(capacity - 1)
	occupied := make([]bool, capacity)
	totalProbe, maxProbe := 0, 0

	insert := func(h uint64) {
		i := h & mask
		probe := 1
		for occupied[i] {
			i = (i + 1) & mask
			probe++
		}
		occupied[i] = true
		totalProbe += probe
		if probe > maxProbe {
			maxProbe = probe
		}
	}

	for p := uint32(0); p < 256; p++ {
		for s := uint32(0); s < n/256; s++ {
			insert(splitMix64(p, s))
		}
	}

	avg := float64(totalProbe) / float64(n)
	t.Logf("dense-id: avg probe %.2f, max cluster %d (LF 0.5)", avg, maxProbe)

	if avg > 2.0 {
		t.Fatalf("avg probe %.2f too high — keys cluster (Morton-like)", avg)
	}
}

func BenchmarkSplitMix64(b *testing.B) {
	var acc uint64
	for i := 0; i < b.N; i++ {
		acc += splitMix64(uint32(i), uint32(i*2654435761))
	}
	sinkU64 = acc
}
