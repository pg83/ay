//go:build amd64

package main

// morton interleaves p and s into a Z-order uint64 via BMI2 PDEP (morton_amd64.s).
// All amd64 targets here have BMI2 (the build/run platform); see mortonGeneric
// for the portable definition and morton_noasm.go for the fallback.
func morton(p, s uint32) uint64
