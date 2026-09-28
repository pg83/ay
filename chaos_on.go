//go:build aychaos

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/zeebo/xxh3"
)

// The test build's seam. AY_CHAOS lists NAME=ARG words, read once at start:
//
//	fault=K    the fault point's call number K (counting from 0) fails; one
//	           word per failing call
//	number=N   the number point hands back N in place of what it is given
//
// An unknown name or a malformed word stops the process before it does
// anything else.
var (
	chaosAssertFault   = newChaosFault("assert")
	chaosForeignPanic  = newChaosFault("foreign-panic")
	chaosTableHintSize = newChaosNumber("table-hint")
	chaosEpochStart    = newChaosNumber("epoch-start")
	chaosBumpChunk     = newChaosNumber("bump-chunk-bytes")
	chaosXXHInternHi   = newChaosNumber("xxh-intern-hi")
	chaosXXHInternLo   = newChaosNumber("xxh-intern-lo")
	chaosXXHSliceHi    = newChaosNumber("xxh-slice-hi")
	chaosXXHSliceLo    = newChaosNumber("xxh-slice-lo")
	chaosHashBucketH1  = newChaosNumber("hash-bucket-h1")
	chaosHashBucketH2  = newChaosNumber("hash-bucket-h2")
	chaosHashListH1    = newChaosNumber("hash-list-h1")
	chaosHashListH2    = newChaosNumber("hash-list-h2")
)

// chaosAssert fails the assert whose call number the assert word names.
func chaosAssert(ok bool) bool {
	fired := chaosAssertFault.fire()

	return ok && !fired
}

// chaosTry hands try a callback that panics with a value other than an
// *Exception on the call number the foreign-panic word names.
func chaosTry(cb func()) func() {
	if chaosForeignPanic.fire() {
		return func() {
			panic("chaos: foreign panic")
		}
	}

	return cb
}

// chaosTableHint stands in for the capacity hint of every IntMap and IntSet,
// so tests reach growth without inputs of production size.
func chaosTableHint(hint int) int {
	return int(chaosTableHintSize.number(uint64(hint)))
}

// chaosFirstEpoch stands in for the first epoch of every epoch-stamped table,
// so tests reach the wraparound without 2^16 or 2^32 resets.
func chaosFirstEpoch(epoch uint64) uint64 {
	return chaosEpochStart.number(epoch)
}

// chaosBumpChunkBytes stands in for the chunk size cap of the bump allocator,
// which production inputs reach after a few megabytes in one arena.
func chaosBumpChunkBytes(n int) int {
	return int(chaosBumpChunk.number(uint64(n)))
}

// The hash functions below narrow the halves their words mask, so the tables
// keyed by one half and verified by the other meet collisions on real inputs.

func chaosInternHash(h xxh3.Uint128) xxh3.Uint128 {
	h.Hi, h.Lo = chaosMaskPair(chaosXXHInternHi, chaosXXHInternLo, h.Hi, h.Lo)

	return h
}

func chaosSliceHash(h xxh3.Uint128) xxh3.Uint128 {
	h.Hi, h.Lo = chaosMaskPair(chaosXXHSliceHi, chaosXXHSliceLo, h.Hi, h.Lo)

	return h
}

func chaosBucketHash(h1, h2 uint64) (uint64, uint64) {
	return chaosMaskPair(chaosHashBucketH1, chaosHashBucketH2, h1, h2)
}

func chaosBucketListHash(h1, h2 uint64) (uint64, uint64) {
	return chaosMaskPair(chaosHashListH1, chaosHashListH2, h1, h2)
}

func chaosMaskPair(p1, p2 ChaosPoint, h1, h2 uint64) (uint64, uint64) {
	return h1 & p1.number(^uint64(0)), h2 & p2.number(^uint64(0))
}

type ChaosPoint struct {
	state *ChaosState
}

type ChaosState struct {
	number bool
	calls  atomic.Uint64
	fires  map[uint64]bool
	set    bool
	value  uint64
}

var chaosPoints = map[string]*ChaosState{}

func newChaosFault(name string) ChaosPoint {
	return newChaosPoint(name, false)
}

func newChaosNumber(name string) ChaosPoint {
	return newChaosPoint(name, true)
}

func newChaosPoint(name string, number bool) ChaosPoint {
	state := &ChaosState{number: number}

	chaosPoints[name] = state

	// Points arm while the package initializes: some sites run in the
	// initializers of other package-level variables. init() rejects the words
	// that arm nothing; exiting from there still writes coverage counters.
	for _, word := range strings.Fields(os.Getenv("AY_CHAOS")) {
		if strings.HasPrefix(word, name+"=") {
			chaosArm(state, word[len(name)+1:])
		}
	}

	return ChaosPoint{state: state}
}

func init() {
	for _, word := range strings.Fields(os.Getenv("AY_CHAOS")) {
		name, arg, ok := strings.Cut(word, "=")
		state := chaosPoints[name]

		if !ok || state == nil || !chaosArm(state, arg) {
			fmt.Fprintf(os.Stderr, "AY_CHAOS: bad word %q\n", word)
			os.Exit(2)
		}
	}
}

// chaosArm applies a word's argument to its point; arming twice with the same
// argument changes nothing. It reports whether the argument is a number.
func chaosArm(state *ChaosState, arg string) bool {
	n, err := strconv.ParseUint(arg, 0, 64)

	if err != nil {
		return false
	}

	if state.number {
		state.set = true
		state.value = n

		return true
	}

	if state.fires == nil {
		state.fires = map[uint64]bool{}
	}

	state.fires[n] = true

	return true
}

func (p ChaosPoint) fire() bool {
	state := p.state

	if state.fires == nil {
		return false
	}

	return state.fires[state.calls.Add(1)-1]
}

func (p ChaosPoint) number(got uint64) uint64 {
	if !p.state.set {
		return got
	}

	return p.state.value
}
