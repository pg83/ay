//go:build amd64

package main

const uidAccumSIMDMin = 8

func uidAccum(es []uint64) (sum, xor, sq, cb uint64) {
	tail := es

	if useBucketHashAVX2 && len(es) >= uidAccumSIMDMin {
		k := len(es) &^ 3

		sum, xor, sq, cb = uidAccumAVX2(&es[0], k)
		tail = es[k:]
	}

	for _, e := range tail {
		t := e * e

		sum += e
		xor ^= e
		sq += t
		cb += t * e
	}

	return sum, xor, sq, cb
}

//go:noescape
func uidAccumAVX2(p *uint64, n int) (sum, xr, sq, cb uint64)
