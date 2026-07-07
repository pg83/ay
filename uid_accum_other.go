//go:build !amd64

package main

func uidAccum(es []uint64) (sum, xor, sq, cb uint64) {
	for _, e := range es {
		t := e * e

		sum += e
		xor ^= e
		sq += t
		cb += t * e
	}

	return sum, xor, sq, cb
}
