//go:build !aychaos

package main

import (
	"bufio"
	"os"

	"github.com/zeebo/xxh3"
)

// The fault seam. A call site hands a chaos function the value it has just
// got and carries on with whatever comes back. The production build hands
// every value back untouched, so the compiler folds the calls away; the test
// build (chaos_on.go) breaks what AY_CHAOS asks for.

func chaosAssert(ok bool) bool {
	return ok
}

func chaosTry(cb func()) func() {
	return cb
}

func chaosTableHint(hint int) int {
	return hint
}

func chaosFirstEpoch(epoch uint64) uint64 {
	return epoch
}

func chaosBumpChunkBytes(n int) int {
	return n
}

func chaosInternHash(h xxh3.Uint128) xxh3.Uint128 {
	return h
}

func chaosSliceHash(h xxh3.Uint128) xxh3.Uint128 {
	return h
}

func chaosBucketHash(h1, h2 uint64) (uint64, uint64) {
	return h1, h2
}

func chaosBucketListHash(h1, h2 uint64) (uint64, uint64) {
	return h1, h2
}

func chaosLink(src, dst string) error {
	return os.Link(src, dst)
}

func chaosReadDir(dir string) ([]os.DirEntry, error) {
	return os.ReadDir(dir)
}

func chaosReadString(r *bufio.Reader) (string, error) {
	return r.ReadString('\n')
}

func chaosAVX512(has bool) bool {
	return has
}

func chaosHostOS(goos string) string {
	return goos
}

func chaosHostArch(goarch string) string {
	return goarch
}
