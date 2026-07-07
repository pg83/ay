//go:build !amd64

package main

func uidAccum(es []uint64) (sum, xor, sq uint64) {
	for _, e := range es {
		sum += e
		xor ^= e
		sq += e * e
	}

	return sum, xor, sq
}
