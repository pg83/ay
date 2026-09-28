//go:build aychaos

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/zeebo/xxh3"
)

// The test build's seam. AY_CHAOS lists NAME=ARG words, read once at start:
//
//	fault=K    the fault point's call number K (counting from 0) fails; one
//	           word per failing call
//	number=N   the number point hands back N in place of what it is given
//	text=S     the text point hands back S in place of what it is given
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
	chaosLinkEXDEV     = newChaosFault("link-exdev")
	chaosReadDirFail   = newChaosFault("readdir-fail")
	chaosReadEIO       = newChaosFault("read-eio")
	chaosCPUAVX512     = newChaosNumber("cpu-avx512")
	chaosHostGOOS      = newChaosText("host-os")
	chaosHostGOARCH    = newChaosText("host-arch")
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

// chaosLink fails a hard link as linking across filesystems does.
func chaosLink(src, dst string) error {
	if chaosLinkEXDEV.fire() {
		return &os.LinkError{Op: "link", Old: src, New: dst, Err: syscall.EXDEV}
	}

	return os.Link(src, dst)
}

// chaosReadDir fails a listing as a directory removed meanwhile does.
func chaosReadDir(dir string) ([]os.DirEntry, error) {
	if chaosReadDirFail.fire() {
		return nil, &os.PathError{Op: "open", Path: dir, Err: syscall.ENOENT}
	}

	return os.ReadDir(dir)
}

// chaosReadString fails a line read as a failing disk does.
func chaosReadString(r *bufio.Reader) (string, error) {
	if chaosReadEIO.fire() {
		return "", syscall.EIO
	}

	return r.ReadString('\n')
}

// chaosAVX512 hides AVX-512 while cpu-avx512 is set to 0, as a CPU without it
// does; a CPU cannot be given a feature it lacks.
func chaosAVX512(has bool) bool {
	return has && chaosCPUAVX512.number(1) != 0
}

// chaosHostOS and chaosHostArch stand in for the host the binary runs on.
func chaosHostOS(goos string) string {
	return chaosHostGOOS.text(goos)
}

func chaosHostArch(goarch string) string {
	return chaosHostGOARCH.text(goarch)
}

type ChaosPoint struct {
	state *ChaosState
}

type ChaosKind int

const (
	chaosKindFault ChaosKind = iota
	chaosKindNumber
	chaosKindText
)

type ChaosState struct {
	kind  ChaosKind
	calls atomic.Uint64
	fires map[uint64]bool
	set   bool
	value uint64
	text  string
}

var chaosPoints = map[string]*ChaosState{}

func newChaosFault(name string) ChaosPoint {
	return newChaosPoint(name, chaosKindFault)
}

func newChaosNumber(name string) ChaosPoint {
	return newChaosPoint(name, chaosKindNumber)
}

func newChaosText(name string) ChaosPoint {
	return newChaosPoint(name, chaosKindText)
}

func newChaosPoint(name string, kind ChaosKind) ChaosPoint {
	state := &ChaosState{kind: kind}

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
// argument changes nothing. It reports whether the argument suits the point.
func chaosArm(state *ChaosState, arg string) bool {
	if state.kind == chaosKindText {
		state.set = true
		state.text = arg

		return true
	}

	n, err := strconv.ParseUint(arg, 0, 64)

	if err != nil {
		return false
	}

	if state.kind == chaosKindNumber {
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

func (p ChaosPoint) text(got string) string {
	if !p.state.set {
		return got
	}

	return p.state.text
}
