package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"unsafe"
)

var (
	ownershipOn         = os.Getenv("AY_DEBUG_OWNERSHIP") != ""
	ownershipRanges     []OwnedRange
	ownershipSorted     bool
	ownershipViolations = map[string]*OwnershipViolation{}
	ownershipSite       string
)

type OwnedRange struct {
	lo, hi uintptr
}

type OwnershipViolation struct {
	nodes    int
	backings map[uintptr]struct{}
	sample   string
}

func ownershipCallSite() string {
	var pcs [16]uintptr

	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])

	for {
		f, more := frames.Next()

		if !strings.HasSuffix(f.File, "/ownership_debug.go") && !strings.HasSuffix(f.File, "/node_emitter.go") {
			short := f.File[strings.LastIndexByte(f.File, '/')+1:]

			return fmt.Sprintf("%s:%d", short, f.Line)
		}

		if !more {
			return "?"
		}
	}
}

func init() {
	if ownershipOn {
		registerKnownGlobalChunks()
	}
}

func registerOwnedRange(p unsafe.Pointer, bytes int) {
	if !ownershipOn || bytes == 0 {
		return
	}

	lo := uintptr(p)

	ownershipRanges = append(ownershipRanges, OwnedRange{lo: lo, hi: lo + uintptr(bytes)})
	ownershipSorted = false
}

func registerOwnedSlice[T any](s []T) {
	if len(s) == 0 {
		return
	}

	var zero T

	registerOwnedRange(unsafe.Pointer(&s[0]), cap(s)*int(unsafe.Sizeof(zero)))
}

func ownedPtr(p uintptr) bool {
	if !ownershipSorted {
		sort.Slice(ownershipRanges, func(i, j int) bool { return ownershipRanges[i].lo < ownershipRanges[j].lo })

		ownershipSorted = true
	}

	i := sort.Search(len(ownershipRanges), func(i int) bool { return ownershipRanges[i].hi > p })

	return i < len(ownershipRanges) && ownershipRanges[i].lo <= p
}

func ownershipCheckSlice[T any](field string, s []T, sample func() string) {
	if len(s) == 0 {
		return
	}

	p := uintptr(unsafe.Pointer(&s[0]))

	if ownedPtr(p) {
		return
	}

	key := field + " @ " + ownershipSite
	v := ownershipViolations[key]

	if v == nil {
		v = &OwnershipViolation{sample: sample(), backings: map[uintptr]struct{}{}}
		ownershipViolations[key] = v
	}

	v.nodes++
	v.backings[p] = struct{}{}
}

func ownershipCheckNode(n *Node) {
	if !ownershipOn {
		return
	}

	ownershipSite = ownershipCallSite()

	outSample := func() string {
		if len(n.Outputs) > 0 {
			return n.Outputs[0].string()
		}

		return "?"
	}

	ownershipCheckSlice("Cmds", n.Cmds, outSample)
	ownershipCheckSlice("Env", n.Env, outSample)
	ownershipCheckSlice("Outputs", n.Outputs, outSample)
	ownershipCheckSlice("DepRefs", n.DepRefs, outSample)
	ownershipCheckSlice("ForeignDepRefs", n.ForeignDepRefs, outSample)
	ownershipCheckSlice("Resources", n.Resources, outSample)
	ownershipCheckSlice("Inputs", n.Inputs, outSample)

	for _, ch := range n.Inputs {
		ownershipCheckSlice("Inputs.chunk", ch, outSample)
	}

	for ci := range n.Cmds {
		c := &n.Cmds[ci]

		ownershipCheckSlice("Cmd.Env", c.Env, outSample)
		ownershipCheckSlice("Cmd.CmdArgs", c.CmdArgs, outSample)

		for _, ch := range c.CmdArgs {
			ownershipCheckSlice("Cmd.CmdArgs.chunk", ch, outSample)
		}
	}
}

func ownershipDump() {
	if !ownershipOn {
		return
	}

	type row struct {
		key string
		v   *OwnershipViolation
	}

	rows := make([]row, 0, len(ownershipViolations))

	for k, v := range ownershipViolations {
		rows = append(rows, row{k, v})
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].v.nodes > rows[j].v.nodes })

	fmt.Fprintf(os.Stderr, "ownership: %d violating (field, site) pairs\n", len(rows))

	for _, r := range rows {
		fmt.Fprintf(os.Stderr, "ownership %8d nodes %8d backings  %-42s  e.g. %s\n", r.v.nodes, len(r.v.backings), r.key, r.v.sample)
	}
}
