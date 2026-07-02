//go:build amd64

package main

import (
	"math/rand"
	"testing"
)

func TestSpliceBenchKernelsMatchScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(0x59110ce))

	const bound = 4096

	kernels := []struct {
		name string
		fn   func([]uint32, uint32, []VFS, []VFS, int) int
	}{
		{"avx512", benchAVX512},
		{"prefetch", benchPrefetch},
	}

	for trial := 0; trial < 3000; trial++ {
		n := rng.Intn(400)

		perm := rng.Perm(bound)[:n]
		win := make([]VFS, n)
		for i, p := range perm {
			win[i] = VFS(p)
		}

		epoch := uint32(1 + rng.Intn(1<<20))

		base := make([]uint32, bound)
		for i := range base {
			if rng.Intn(3) == 0 {
				base[i] = epoch
			} else {
				base[i] = uint32(rng.Intn(1 << 20))
				if base[i] == epoch {
					base[i]++
				}
			}
		}

		startK := rng.Intn(8)

		genRef := append([]uint32(nil), base...)
		blockRef := make([]VFS, startK+n)
		kRef := benchScalar(genRef, epoch, win, blockRef, startK)

		for _, kern := range kernels {
			genGot := append([]uint32(nil), base...)
			blockGot := make([]VFS, startK+n)
			kGot := kern.fn(genGot, epoch, win, blockGot, startK)

			if kGot != kRef {
				t.Fatalf("%s trial %d n=%d: k = %d, want %d", kern.name, trial, n, kGot, kRef)
			}

			for i := startK; i < kRef; i++ {
				if blockGot[i] != blockRef[i] {
					t.Fatalf("%s trial %d n=%d: block[%d] = %d, want %d", kern.name, trial, n, i, blockGot[i], blockRef[i])
				}
			}

			for i := range genRef {
				if genGot[i] != genRef[i] {
					t.Fatalf("%s trial %d n=%d: gen[%d] = %d, want %d", kern.name, trial, n, i, genGot[i], genRef[i])
				}
			}
		}
	}
}
