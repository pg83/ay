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
//	fault=K   the fault point's call number K (counting from 0) fails; one
//	          word per failing call
//	value=V   the value point hands back V in place of what its site passes
//
// An unknown name or a malformed word stops the process before it does
// anything else.
type ChaosPoint struct {
	state *ChaosState
}

type ChaosState struct {
	value bool
	calls atomic.Uint64
	fires map[uint64]bool
	set   bool
	arg   uint64
}

var chaosPoints = map[string]*ChaosState{}

func newChaosFault(name string) ChaosPoint {
	return newChaosPoint(name, false)
}

func newChaosValue(name string) ChaosPoint {
	return newChaosPoint(name, true)
}

func newChaosPoint(name string, value bool) ChaosPoint {
	state := &ChaosState{value: value}

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
	n, err := strconv.ParseUint(arg, 0, 64)

	if !ok || state == nil || err != nil {
		fmt.Fprintf(os.Stderr, "AY_CHAOS: bad word %q\n", word)
		os.Exit(2)
	}

	if state.value {
		state.set = true
		state.arg = n

		return
	}

	if state.fires == nil {
		state.fires = map[uint64]bool{}
	}

	state.fires[n] = true
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

	return p.state.arg
}

func chaosSwap[T any](p ChaosPoint, got, fault T) T {
	if p.fire() {
		return fault
	}

	return got
}
