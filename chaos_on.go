//go:build aychaos

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// The test binary's seam. AY_CHAOS lists NAME=ARG words, read once at start:
//
//	fault=K    the fault point's call number K (counting from 0) fails; one
//	           word per failing call
//	number=N   the number point hands back N in place of what its site passes
//	text=S     the text point hands back S in place of what its site passes
//
// An unknown name or a malformed word stops the process before it does
// anything else.
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
	kind   ChaosKind
	calls  atomic.Uint64
	fires  map[uint64]bool
	set    bool
	number uint64
	text   string
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

	return ChaosPoint{state: state}
}

func init() {
	for _, word := range strings.Fields(os.Getenv("AY_CHAOS")) {
		chaosArm(word)
	}
}

func chaosArm(word string) {
	name, arg, ok := strings.Cut(word, "=")
	state := chaosPoints[name]

	if !ok || state == nil {
		chaosBadWord(word)
	}

	if state.kind == chaosKindText {
		state.set = true
		state.text = arg

		return
	}

	n, err := strconv.ParseUint(arg, 0, 64)

	if err != nil {
		chaosBadWord(word)
	}

	if state.kind == chaosKindNumber {
		state.set = true
		state.number = n

		return
	}

	if state.fires == nil {
		state.fires = map[uint64]bool{}
	}

	state.fires[n] = true
}

func chaosBadWord(word string) {
	fmt.Fprintf(os.Stderr, "AY_CHAOS: bad word %q\n", word)
	os.Exit(2)
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

	return p.state.number
}

func (p ChaosPoint) text(got string) string {
	if !p.state.set {
		return got
	}

	return p.state.text
}

func chaosSwap[T any](p ChaosPoint, got, fault T) T {
	if p.fire() {
		return fault
	}

	return got
}
