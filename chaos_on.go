//go:build aychaos

package main

import (
	"bufio"
	"fmt"
	"math/rand/v2"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/zeebo/xxh3"
)

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
	chaosSlotMask      = newChaosNumber("hash-source-under-slot")
	chaosLinkEXDEV     = newChaosFault("link-exdev")
	chaosReadDirFail   = newChaosFault("readdir-fail")
	chaosReadEIO       = newChaosFault("read-eio")
	chaosReadFileEIO   = newChaosFault("readfile-eio")
	chaosUserFail      = newChaosFault("user-current-fail")
	chaosCPUAVX512     = newChaosNumber("cpu-avx512")
	chaosHostGOOS      = newChaosText("host-os")
	chaosHostGOARCH    = newChaosText("host-arch")
	chaosInternSeed    = newChaosNumber("intern-seed")
	chaosInternOrder   = newChaosNumber("intern-shuffle")
	chaosInternShuffle = chaosInternOrder.number(1) != 0
	chaosPoints        = map[string]*ChaosState{}
	chaosInternPerms   [][]uint32
)

const (
	chaosInternWindow      = 1 << 12
	chaosInternDefaultSeed = 0x9e3779b97f4a7c15
)

const (
	chaosKindFault ChaosKind = iota
	chaosKindNumber
	chaosKindText
)

func chaosInternID(seq uint32) uint32 {
	block := int(seq / chaosInternWindow)

	for len(chaosInternPerms) <= block {
		chaosInternPerms = append(chaosInternPerms, chaosInternPerm(len(chaosInternPerms)))
	}

	return uint32(block)*chaosInternWindow + chaosInternPerms[block][seq%chaosInternWindow]
}

func chaosInternPerm(block int) []uint32 {
	rng := rand.New(rand.NewPCG(chaosInternSeed.number(chaosInternDefaultSeed), uint64(block)))
	perm := make([]uint32, chaosInternWindow)

	for i := range perm {
		perm[i] = uint32(i)
	}

	first := 0

	if block == 0 {
		first = 1
	}

	rng.Shuffle(len(perm)-first, func(i, j int) {
		perm[first+i], perm[first+j] = perm[first+j], perm[first+i]
	})

	return perm
}

func chaosAssert(ok bool) bool {
	fired := chaosAssertFault.fire()

	return ok && !fired
}

func chaosTry(cb func()) func() {
	if chaosForeignPanic.fire() {
		return func() {
			panic("chaos: foreign panic")
		}
	}

	return cb
}

func chaosTableHint(hint int) int {
	return int(chaosTableHintSize.number(uint64(hint)))
}

func chaosFirstEpoch(epoch uint64) uint64 {
	return chaosEpochStart.number(epoch)
}

func chaosBumpChunkBytes(n int) int {
	return int(chaosBumpChunk.number(uint64(n)))
}

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

func chaosSourceUnderSlot(slot uint32) uint32 {
	return slot & uint32(chaosSlotMask.number(^uint64(0)))
}

func chaosMaskPair(p1, p2 ChaosPoint, h1, h2 uint64) (uint64, uint64) {
	return h1 & p1.number(^uint64(0)), h2 & p2.number(^uint64(0))
}

func chaosLink(src, dst string) error {
	if chaosLinkEXDEV.fire() {
		return &os.LinkError{Op: "link", Old: src, New: dst, Err: syscall.EXDEV}
	}

	return os.Link(src, dst)
}

func chaosReadDir(dir string) ([]os.DirEntry, error) {
	if chaosReadDirFail.fire() {
		return nil, &os.PathError{Op: "open", Path: dir, Err: syscall.ENOENT}
	}

	return os.ReadDir(dir)
}

func chaosReadString(r *bufio.Reader) (string, error) {
	if chaosReadEIO.fire() {
		return "", syscall.EIO
	}

	return r.ReadString('\n')
}

func chaosReadFile(name string) ([]byte, error) {
	if chaosReadFileEIO.fire() {
		return nil, &os.PathError{Op: "read", Path: name, Err: syscall.EIO}
	}

	return os.ReadFile(name)
}

func chaosCurrentUser() (*user.User, error) {
	if chaosUserFail.fire() {
		return nil, user.UnknownUserIdError(os.Getuid())
	}

	return user.Current()
}

func chaosAVX512(has bool) bool {
	return has && chaosCPUAVX512.number(1) != 0
}

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

type ChaosState struct {
	kind  ChaosKind
	calls atomic.Uint64
	fires map[uint64]bool
	set   bool
	value uint64
	text  string
}

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
