package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"sort"
	"strings"
)

type DiffFieldHashes [10]uint64

type DiffKindRec struct {
	kind string
	h    [10]uint64
}

func cmdDumpDiff(_ GlobalFlags, args []string) int {
	var leftPath, rightPath, outPath, mode, pairOut, groupSpec string
	var wantRoots bool

	setMode := func(m string) {
		if mode != "" {
			throwFmt("dump diff: modes --%s and --%s are mutually exclusive", mode, m)
		}

		mode = m
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--left":
			i++
			leftPath = arg(args, i)
		case "--right":
			i++
			rightPath = arg(args, i)
		case "--out":
			i++
			outPath = arg(args, i)
		case "--summary":
			setMode("summary")
		case "--by-field":
			setMode("by-field")
		case "--by-token":
			setMode("by-token")
		case "--by-kind":
			setMode("by-kind")
		case "--roots":

			wantRoots = true
		case "--group":
			i++
			groupSpec = arg(args, i)
		case "--pair":
			setMode("pair")
			i++
			pairOut = arg(args, i)
		default:
			throwFmt("dump diff: unknown argument %q", args[i])
		}
	}

	if leftPath == "" || rightPath == "" {
		throwFmt("dump diff: --left and --right are required")
	}

	if wantRoots && mode != "" && mode != "by-token" {
		throwFmt("dump diff: --roots cannot combine with --%s", mode)
	}

	if groupSpec != "" && mode != "by-token" {
		throwFmt("dump diff: --group is only valid with --by-token")
	}

	var w io.Writer = os.Stdout

	if outPath != "" && outPath != "-" {
		f := throw2(os.Create(outPath))

		defer func() { throw(f.Close()) }()

		w = f
	}

	bw := bufio.NewWriterSize(w, 1<<20)

	defer func() { throw(bw.Flush()) }()

	switch mode {
	case "summary":
		diffSummary(leftPath, rightPath, bw)
	case "by-field":
		diffByField(leftPath, rightPath, bw)
	case "by-token":
		diffByToken(leftPath, rightPath, bw, byTokenOpts{rootsOnly: wantRoots, groupBy: parseGroupSpec(groupSpec)})
	case "by-kind":
		diffByKind(leftPath, rightPath, bw)
	case "pair":
		diffPair(leftPath, rightPath, pairOut, bw)
	default:
		if wantRoots {
			diffRoots(leftPath, rightPath, bw)
		} else {
			diffSections(leftPath, rightPath, bw)
		}
	}

	return 0
}

func parseGroupSpec(spec string) []string {
	if spec == "" {
		return nil
	}

	var dims []string

	for _, d := range strings.Split(spec, ",") {
		d = strings.TrimSpace(d)

		if d != "kind" && d != "dir" {
			throwFmt("dump diff: --group dimension %q must be one of kind,dir", d)
		}

		dims = append(dims, d)
	}

	return dims
}

func diffSections(leftPath, rightPath string, bw *bufio.Writer) {
	leftSelf, leftOut := scanDiffIndex(leftPath)
	rightSelf, rightOut := scanDiffIndex(rightPath)

	writeDiffSection(bw, "self_uid only in LEFT", setMinus(leftSelf, rightSelf))
	writeDiffSection(bw, "self_uid only in RIGHT", setMinus(rightSelf, leftSelf))
	writeDiffSection(bw, "outputs only in LEFT", keysMinus(leftOut, rightOut))
	writeDiffSection(bw, "outputs only in RIGHT", keysMinus(rightOut, leftOut))

	var mismatched []string

	for out, ls := range leftOut {
		if rs, ok := rightOut[out]; ok && !setsEqual(ls, rs) {
			mismatched = append(mismatched, out)
		}
	}

	sort.Strings(mismatched)

	throw2(fmt.Fprintf(bw, "=== outputs in both with mismatched self_uid (%d) ===\n", len(mismatched)))

	for _, out := range mismatched {
		throw2(fmt.Fprintf(bw, "%s  left=%s right=%s\n", out, joinSet(leftOut[out]), joinSet(rightOut[out])))
	}
}

func scanDiffIndex(path string) (map[string]bool, map[string]map[string]bool) {
	selfUIDs := map[string]bool{}
	outToUIDs := map[string]map[string]bool{}

	streamJSONL(path, func(n map[string]any) {
		su := getString(n, "self_uid")

		selfUIDs[su] = true

		for _, out := range toStrings(n["outputs"]) {
			if outToUIDs[out] == nil {
				outToUIDs[out] = map[string]bool{}
			}

			outToUIDs[out][su] = true
		}
	})

	return selfUIDs, outToUIDs
}

func diffSummary(leftPath, rightPath string, bw *bufio.Writer) {
	leftKind := scanOutputKind(leftPath)
	rightKind := scanOutputKind(rightPath)

	summarize := func(title string, only map[string]string) {
		throw2(fmt.Fprintf(bw, "=== %s (%d) ===\n", title, len(only)))

		byKind, byExt, byDir := map[string]int{}, map[string]int{}, map[string]int{}

		for out, kind := range only {
			byKind[kind]++
			byExt[outputExt(out)]++
			byDir[outputTopDir(out)]++
		}

		writeCountMap(bw, "  by kind", byKind, 12)
		writeCountMap(bw, "  by ext", byExt, 12)
		writeCountMap(bw, "  by dir", byDir, 15)
	}

	onlyLeft := map[string]string{}

	for out, k := range leftKind {
		if _, ok := rightKind[out]; !ok {
			onlyLeft[out] = k
		}
	}

	onlyRight := map[string]string{}

	for out, k := range rightKind {
		if _, ok := leftKind[out]; !ok {
			onlyRight[out] = k
		}
	}

	summarize("outputs only in LEFT", onlyLeft)
	summarize("outputs only in RIGHT", onlyRight)
}

func scanOutputKind(path string) map[string]string {
	out := map[string]string{}

	streamJSONL(path, func(n map[string]any) {
		k := nodeKVP(n)

		for _, o := range toStrings(n["outputs"]) {
			out[o] = k
		}
	})

	return out
}

func diffByField(leftPath, rightPath string, bw *bufio.Writer) {
	canc := newDumpDiffCanceler(leftPath, rightPath, dumpDiffContentKey)
	rExact := map[string]DiffFieldHashes{}
	rAxis := map[string]DiffFieldHashes{}
	ridx := map[string]DiffFieldHashes{}

	streamJSONL(rightPath, func(n map[string]any) {
		if canc.cancelRight(n) {
			return
		}

		h := nodeFieldHashes(n)

		for _, o := range toStrings(n["outputs"]) {
			setDumpDiffFieldMatch(rExact, rAxis, ridx, o, n, h)
		}
	})

	fieldDiff := make([]int, len(dumpContentFields))
	combo := map[string]int{}
	both := 0

	streamJSONL(leftPath, func(n map[string]any) {
		if canc.cancelLeft(n) {
			both++

			return
		}

		var rh DiffFieldHashes

		found := false

		if rh, found = findDumpDiffFieldMatch(rExact, rAxis, ridx, n); !found {
			return
		}

		both++

		lh := nodeFieldHashes(n)

		var diffs []string

		for i, f := range dumpContentFields {
			if lh[i] != rh[i] {
				fieldDiff[i]++
				diffs = append(diffs, f)
			}
		}

		if len(diffs) > 0 {
			combo[strings.Join(diffs, "+")]++
		}
	})

	throw2(fmt.Fprintf(bw, "=== by-field: %d outputs in both ===\n", both))
	throw2(fmt.Fprintf(bw, "\n[content field -> #nodes where it differs]\n"))

	order := make([]int, len(dumpContentFields))

	for i := range order {
		order[i] = i
	}

	sort.Slice(order, func(a, b int) bool { return fieldDiff[order[a]] > fieldDiff[order[b]] })

	for _, i := range order {
		if fieldDiff[i] == 0 {
			continue
		}

		pct := 0.0

		if both > 0 {
			pct = 100 * float64(fieldDiff[i]) / float64(both)
		}

		throw2(fmt.Fprintf(bw, "  %6d (%5.1f%%)  %s\n", fieldDiff[i], pct, dumpContentFields[i]))
	}

	throw2(fmt.Fprintf(bw, "\n[most common differing-field combinations]\n"))
	writeCountMap(bw, "", combo, 15)
}

func nodeFieldHashes(n map[string]any) [10]uint64 {
	var h DiffFieldHashes

	for i, f := range dumpContentFields {
		h[i] = fnvHash(marshalCompact(n[f]))
	}

	return h
}

var tokenFields = []string{"cmds", "inputs", "tags", "outputs"}

func tokenize(n map[string]any, field string) []string {
	if field == "cmds" {
		return cmdArgTokens(n)
	}

	return toStrings(n[field])
}

type byTokenOpts struct {
	rootsOnly bool
	groupBy   []string
}

func diffByToken(leftPath, rightPath string, bw *bufio.Writer, opts byTokenOpts) {
	canc := newDumpDiffCanceler(leftPath, rightPath, dumpDiffContentKey)
	rExact := map[string]map[string][]string{}
	rAxis := map[string]map[string][]string{}
	ridx := map[string]map[string][]string{}

	streamJSONL(rightPath, func(n map[string]any) {
		if canc.cancelRight(n) {
			return
		}

		rec := map[string][]string{}

		for _, f := range tokenFields {
			rec[f] = tokenize(n, f)
		}

		for _, o := range toStrings(n["outputs"]) {
			setDumpDiffTokenMatch(rExact, rAxis, ridx, o, n, rec)
		}
	})

	var rootSet map[string]bool

	if opts.rootsOnly {
		rootSet, _ = computeRootOutputs(leftPath, rightPath)
	}

	our := map[string]map[string]map[string]int{}
	ref := map[string]map[string]map[string]int{}

	ensure := func(m map[string]map[string]map[string]int, g string) {
		if m[g] == nil {
			m[g] = map[string]map[string]int{}

			for _, f := range tokenFields {
				m[g][f] = map[string]int{}
			}
		}
	}

	groups := []string{}
	paired := 0

	streamJSONL(leftPath, func(n map[string]any) {
		if canc.cancelLeft(n) {
			if opts.rootsOnly && !nodeProducesRoot(n, rootSet) {
				return
			}

			paired++

			return
		}

		rec, ok := findDumpDiffTokenMatch(rExact, rAxis, ridx, n)

		if !ok {
			return
		}

		if opts.rootsOnly && !nodeProducesRoot(n, rootSet) {
			return
		}

		paired++

		g := byTokenGroupKey(n, opts.groupBy)

		if _, seen := our[g]; !seen {
			groups = append(groups, g)
		}

		ensure(our, g)
		ensure(ref, g)

		for _, f := range tokenFields {
			accumMultisetDiff(tokenize(n, f), rec[f], our[g][f], ref[g][f])
		}
	})

	scope := ""

	if opts.rootsOnly {
		scope = " (roots only)"
	}

	throw2(fmt.Fprintf(bw, "=== by-token: %d outputs in both%s ===\n", paired, scope))

	if len(opts.groupBy) == 0 {
		ensure(our, "")
		ensure(ref, "")

		for _, f := range tokenFields {
			writeTokenRanking(bw, f+" tokens only in OURS", our[""][f])
			writeTokenRanking(bw, f+" tokens only in REF", ref[""][f])
		}

		return
	}

	sort.Strings(groups)

	for _, g := range groups {
		throw2(fmt.Fprintf(bw, "\n########## group: %s ##########\n", g))

		for _, f := range tokenFields {
			writeTokenRanking(bw, f+" tokens only in OURS", our[g][f])
			writeTokenRanking(bw, f+" tokens only in REF", ref[g][f])
		}
	}
}

func byTokenGroupKey(n map[string]any, dims []string) string {
	if len(dims) == 0 {
		return ""
	}

	parts := make([]string, 0, len(dims))

	for _, d := range dims {
		switch d {
		case "kind":
			k := nodeKVP(n)

			if k == "" {
				k = "(none)"
			}

			parts = append(parts, "kind="+k)
		case "dir":
			parts = append(parts, "dir="+nodePrimaryDir(n))
		}
	}

	return strings.Join(parts, " ")
}

func nodePrimaryDir(n map[string]any) string {
	outs := toStrings(n["outputs"])

	if len(outs) == 0 {
		return "(no-output)"
	}

	first := outs[0]

	for _, o := range outs[1:] {
		if o < first {
			first = o
		}
	}

	return outputTopDir(first)
}

func nodeProducesRoot(n map[string]any, rootSet map[string]bool) bool {
	for _, o := range toStrings(n["outputs"]) {
		if rootSet[o] {
			return true
		}
	}

	return false
}

func cmdArgTokens(n map[string]any) []string {
	var out []string

	cmds, _ := n["cmds"].([]any)

	for _, c := range cmds {
		cm, _ := c.(map[string]any)

		out = append(out, toStrings(cm["cmd_args"])...)
	}

	return out
}

func accumMultisetDiff(left, right []string, onlyL, onlyR map[string]int) {
	lc, rc := map[string]int{}, map[string]int{}

	for _, t := range left {
		lc[t]++
	}

	for _, t := range right {
		rc[t]++
	}

	for t, c := range lc {
		if d := c - rc[t]; d > 0 {
			onlyL[t]++
		}
	}

	for t, c := range rc {
		if d := c - lc[t]; d > 0 {
			onlyR[t]++
		}
	}
}

func writeTokenRanking(bw *bufio.Writer, title string, m map[string]int) {
	throw2(fmt.Fprintf(bw, "\n[%s]  (token: #nodes, by category)\n", title))

	byCat := map[string]int{}

	for t := range m {
		byCat[tokenCategory(t)] += m[t]
	}

	writeCountMap(bw, "  totals", byCat, 12)
	type kv struct {
		t string
		c int
	}
	var top []kv

	for t, c := range m {
		top = append(top, kv{t, c})
	}

	sort.Slice(top, func(a, b int) bool {
		if top[a].c != top[b].c {
			return top[a].c > top[b].c
		}

		return top[a].t < top[b].t
	})

	for i, e := range top {
		if i >= 25 {
			break
		}

		throw2(fmt.Fprintf(bw, "  %6d  [%s] %s\n", e.c, tokenCategory(e.t), e.t))
	}
}

func tokenCategory(t string) string {
	switch {
	case strings.HasPrefix(t, "-I"):
		return "incl"
	case strings.HasPrefix(t, "-D"):
		return "def"
	case strings.HasPrefix(t, "-L"):
		return "libdir"
	case strings.HasPrefix(t, "-l"):
		return "lib"
	case strings.HasPrefix(t, "-W"):
		return "warn"
	case strings.HasPrefix(t, "-m"):
		return "march"
	case strings.HasPrefix(t, "-f"):
		return "fflag"
	case strings.HasPrefix(t, "${") || strings.Contains(t, "${"):
		return "UNEXPANDED"
	case strings.HasPrefix(t, "-"):
		return "flag"
	case strings.Contains(t, "/") || strings.Contains(t, "$("):
		return "path"
	default:
		return "other"
	}
}

func diffByKind(leftPath, rightPath string, bw *bufio.Writer) {
	canc := newDumpDiffCanceler(leftPath, rightPath, dumpDiffContentKey)
	rExact := map[string]DiffKindRec{}
	rAxis := map[string]DiffKindRec{}
	ridx := map[string]DiffKindRec{}

	streamJSONL(rightPath, func(n map[string]any) {
		if canc.cancelRight(n) {
			return
		}

		rr := DiffKindRec{kind: nodeKVP(n), h: nodeFieldHashes(n)}

		for _, o := range toStrings(n["outputs"]) {
			setDumpDiffKindMatch(rExact, rAxis, ridx, o, n, rr)
		}
	})

	total := map[string]int{}
	diverge := map[string]int{}
	fieldDiff := map[string][]int{}
	combo := map[string]map[string]int{}

	streamJSONL(leftPath, func(n map[string]any) {
		kind := nodeKVP(n)

		if kind == "" {
			kind = "(none)"
		}

		if canc.cancelLeft(n) {
			total[kind]++

			return
		}

		var rr DiffKindRec

		found := false

		if rr, found = findDumpDiffKindMatch(rExact, rAxis, ridx, n); !found {
			return
		}

		total[kind]++

		lh := nodeFieldHashes(n)
		fd := fieldDiff[kind]

		if fd == nil {
			fd = make([]int, len(dumpContentFields))
		}

		var diffs []string

		for i := range dumpContentFields {
			if lh[i] != rr.h[i] {
				fd[i]++
				diffs = append(diffs, dumpContentFields[i])
			}
		}

		fieldDiff[kind] = fd

		if len(diffs) > 0 {
			diverge[kind]++

			if combo[kind] == nil {
				combo[kind] = map[string]int{}
			}

			combo[kind][strings.Join(diffs, "+")]++
		}
	})

	kinds := make([]string, 0, len(total))

	for k := range total {
		kinds = append(kinds, k)
	}

	sort.Slice(kinds, func(a, b int) bool { return total[kinds[a]] > total[kinds[b]] })
	throw2(fmt.Fprintf(bw, "=== by-kind: content divergence per node kind ===\n"))
	throw2(fmt.Fprintf(bw, "%-8s %8s %8s   %s\n", "kind", "paired", "diverge", "top differing fields / combos"))

	for _, k := range kinds {
		fd := fieldDiff[k]

		var parts []string

		for i, f := range dumpContentFields {
			if len(fd) > 0 && fd[i] > 0 {
				parts = append(parts, fmt.Sprintf("%s:%d", f, fd[i]))
			}
		}

		sort.Slice(parts, func(a, b int) bool { return parts[a] > parts[b] })

		topCombo := ""
		best := 0

		for c, n := range combo[k] {
			if n > best {
				best, topCombo = n, c
			}
		}

		throw2(fmt.Fprintf(bw, "%-8s %8d %8d   %s   [top combo: %s ×%d]\n",
			k, total[k], diverge[k], strings.Join(parts, " "), topCombo, best))
	}
}

func computeRootOutputs(leftPath, rightPath string) (map[string]bool, int) {
	canc := newDumpDiffCanceler(leftPath, rightPath, dumpDiffSelfUIDKey)
	rightExact := map[string]string{}
	rightAxis := map[string]string{}
	rightSelf := map[string]string{}

	streamJSONL(rightPath, func(n map[string]any) {
		if canc.cancelRight(n) {
			return
		}

		su := getString(n, "self_uid")

		for _, o := range toStrings(n["outputs"]) {
			setDumpDiffSelfMatch(rightExact, rightAxis, rightSelf, o, n, su)
		}
	})

	divergent := map[string]bool{}
	uidToDivergentOuts := map[string]map[string]bool{}
	uidToDeps := map[string][]string{}

	streamJSONL(leftPath, func(n map[string]any) {
		if canc.cancelLeft(n) {
			return
		}

		su := getString(n, "self_uid")
		uid := getString(n, "uid")
		outs := toStrings(n["outputs"])

		uidToDeps[uid] = toStrings(n["deps"])

		for _, o := range outs {
			rightSU, found := findDumpDiffOutputSelfMatch(rightExact, rightAxis, rightSelf, o, n)

			if !found || rightSU == su {
				continue
			}

			divergent[o] = true

			if uidToDivergentOuts[uid] == nil {
				uidToDivergentOuts[uid] = map[string]bool{}
			}

			uidToDivergentOuts[uid][o] = true
		}
	})

	leafSet := map[string]bool{}

	for uid, outs := range uidToDivergentOuts {
		if len(outs) == 0 {
			continue
		}

		leaf := true

		for _, d := range uidToDeps[uid] {
			if len(uidToDivergentOuts[d]) > 0 {
				leaf = false

				break
			}
		}

		if leaf {
			for out := range outs {
				leafSet[out] = true
			}
		}
	}

	return leafSet, len(divergent)
}

func diffRoots(leftPath, rightPath string, bw *bufio.Writer) {
	leafSet, divergent := computeRootOutputs(leftPath, rightPath)
	leaves := make([]string, 0, len(leafSet))

	for out := range leafSet {
		leaves = append(leaves, out)
	}

	sort.Strings(leaves)

	throw2(fmt.Fprintf(bw, "=== roots: %d leaf-most divergent outputs (of %d divergent) ===\n", len(leaves), divergent))
	throw2(fmt.Fprintf(bw, "(content differs but every dependency child matches the reference — fix these first)\n"))

	for _, o := range leaves {
		throw2(fmt.Fprintf(bw, "%s\n", o))
	}
}

func diffPair(leftPath, rightPath, output string, bw *bufio.Writer) {
	want := normPath(output)
	left, right := findNodePairByOutput(leftPath, rightPath, want)

	if left == nil {
		throwFmt("dump diff --pair: output %q not found in left", output)
	}

	if right == nil {
		throwFmt("dump diff --pair: output %q not found in right", output)
	}

	throw2(fmt.Fprintf(bw, "=== pair diff for %s ===\n", want))

	for _, f := range dumpContentFields {
		if fnvHash(marshalCompact(left[f])) == fnvHash(marshalCompact(right[f])) {
			continue
		}

		throw2(fmt.Fprintf(bw, "\n[field %s differs]\n", f))

		switch f {
		case "cmds":
			writePairCmds(bw, left, right)
		case "inputs", "tags", "outputs":
			writePairTokens(bw, toStrings(left[f]), toStrings(right[f]))
		default:
			throw2(fmt.Fprintf(bw, "  ours: %s\n  ref:  %s\n", marshalCompact(left[f]), marshalCompact(right[f])))
		}
	}
}

func writePairCmds(bw *bufio.Writer, left, right map[string]any) {
	lTok, rTok := cmdArgTokens(left), cmdArgTokens(right)
	onlyL, onlyR := map[string]int{}, map[string]int{}

	accumMultisetDiff(lTok, rTok, onlyL, onlyR)

	if len(onlyL) > 0 || len(onlyR) > 0 {
		writePairTokens(bw, lTok, rTok)

		return
	}

	lc, rc := cmdMaps(left), cmdMaps(right)

	throw2(fmt.Fprintf(bw, "  [cmds structurally differ; cmd_args multiset matches]\n"))

	if len(lc) != len(rc) {
		throw2(fmt.Fprintf(bw, "  cmd count: ours=%d ref=%d\n", len(lc), len(rc)))
	}

	n := len(lc)

	if len(rc) < n {
		n = len(rc)
	}

	for i := 0; i < n; i++ {
		l, r := lc[i], rc[i]

		if lcwd, rcwd := getString(l, "cwd"), getString(r, "cwd"); lcwd != rcwd {
			throw2(fmt.Fprintf(bw, "  cmd[%d] cwd: ours=%s ref=%s\n", i, lcwd, rcwd))
		}

		if lso, rso := getString(l, "stdout"), getString(r, "stdout"); lso != rso {
			throw2(fmt.Fprintf(bw, "  cmd[%d] stdout: ours=%s ref=%s\n", i, lso, rso))
		}

		if le, re := string(marshalCompact(l["env"])), string(marshalCompact(r["env"])); le != re {
			throw2(fmt.Fprintf(bw, "  cmd[%d] env: ours=%s ref=%s\n", i, le, re))
		}

		la, ra := toStrings(l["cmd_args"]), toStrings(r["cmd_args"])

		if strings.Join(la, "\x00") != strings.Join(ra, "\x00") {
			throw2(fmt.Fprintf(bw, "  cmd[%d] arg order:\n    ours: %s\n    ref:  %s\n", i, strings.Join(la, " "), strings.Join(ra, " ")))
		}
	}
}

func cmdMaps(n map[string]any) []map[string]any {
	cmds, _ := n["cmds"].([]any)
	out := make([]map[string]any, 0, len(cmds))

	for _, c := range cmds {
		cm, _ := c.(map[string]any)

		out = append(out, cm)
	}

	return out
}

func writePairTokens(bw *bufio.Writer, left, right []string) {
	onlyL, onlyR := map[string]int{}, map[string]int{}

	accumMultisetDiff(left, right, onlyL, onlyR)
	var lk, rk []string

	for t := range onlyL {
		lk = append(lk, t)
	}

	for t := range onlyR {
		rk = append(rk, t)
	}

	sort.Strings(lk)
	sort.Strings(rk)

	for _, t := range lk {
		throw2(fmt.Fprintf(bw, "  -ours +%s\n", t))
	}

	for _, t := range rk {
		throw2(fmt.Fprintf(bw, "  -ref  +%s\n", t))
	}
}

func findNodePairByOutput(leftPath, rightPath, want string) (map[string]any, map[string]any) {
	leftNodes := findNodesByOutput(leftPath, want)
	rightNodes := findNodesByOutput(rightPath, want)

	if len(leftNodes) == 0 {
		return nil, nil
	}

	if len(rightNodes) == 0 {
		return leftNodes[0], nil
	}

	residLeft, residRight, eqLeft, eqRight := stripContentEqualPairs(leftNodes, rightNodes)

	if len(residLeft) == 0 && len(residRight) == 0 {
		return eqLeft[0], eqRight[0]
	}

	if len(residLeft) > 0 && len(residRight) > 0 {
		leftNodes, rightNodes = residLeft, residRight
	}

	exactDivLeft, exactDivRight, exactAnyLeft, exactAnyRight, exactDivFound, exactAnyFound := findMatchingNodePair(leftNodes, rightNodes, func(left, right map[string]any) bool {
		return dumpDiffNodeMatchKey(left, true) == dumpDiffNodeMatchKey(right, true)
	})

	if exactDivFound {
		return exactDivLeft, exactDivRight
	}

	axisDivLeft, axisDivRight, axisAnyLeft, axisAnyRight, axisDivFound, axisAnyFound := findMatchingNodePair(leftNodes, rightNodes, func(left, right map[string]any) bool {
		return dumpDiffNodeMatchKey(left, false) == dumpDiffNodeMatchKey(right, false)
	})

	if axisDivFound {
		return axisDivLeft, axisDivRight
	}

	if exactAnyFound {
		return exactAnyLeft, exactAnyRight
	}

	if axisAnyFound {
		return axisAnyLeft, axisAnyRight
	}

	return leftNodes[0], rightNodes[0]
}

func stripContentEqualPairs(leftNodes, rightNodes []map[string]any) (residLeft, residRight, eqLeft, eqRight []map[string]any) {
	usedRight := make([]bool, len(rightNodes))

	for _, left := range leftNodes {
		matched := -1

		for j, right := range rightNodes {
			if usedRight[j] {
				continue
			}

			if dumpDiffNodeContentEqual(left, right) {
				matched = j

				break
			}
		}

		if matched < 0 {
			residLeft = append(residLeft, left)

			continue
		}

		usedRight[matched] = true
		eqLeft = append(eqLeft, left)
		eqRight = append(eqRight, rightNodes[matched])
	}

	for j, right := range rightNodes {
		if !usedRight[j] {
			residRight = append(residRight, right)
		}
	}

	return residLeft, residRight, eqLeft, eqRight
}

func findMatchingNodePair(leftNodes, rightNodes []map[string]any, match func(left, right map[string]any) bool) (map[string]any, map[string]any, map[string]any, map[string]any, bool, bool) {
	var firstLeft, firstRight map[string]any

	for _, left := range leftNodes {
		for _, right := range rightNodes {
			if !match(left, right) {
				continue
			}

			if firstLeft == nil {
				firstLeft, firstRight = left, right
			}

			if !dumpDiffNodeContentEqual(left, right) {
				return left, right, firstLeft, firstRight, true, true
			}
		}
	}

	if firstLeft != nil {
		return nil, nil, firstLeft, firstRight, false, true
	}

	return nil, nil, nil, nil, false, false
}

func dumpDiffNodeContentEqual(left, right map[string]any) bool {
	return nodeFieldHashes(left) == nodeFieldHashes(right)
}

type dumpDiffCanceler struct {
	key         func(map[string]any) string
	budgetLeft  map[string]int
	budgetRight map[string]int
}

func newDumpDiffCanceler(leftPath, rightPath string, key func(map[string]any) string) *dumpDiffCanceler {
	left := dumpDiffKeyMultiset(leftPath, key)
	right := dumpDiffKeyMultiset(rightPath, key)
	pairs := make(map[string]int, len(left))

	for k, lc := range left {
		if rc := right[k]; rc > 0 {
			n := lc

			if rc < n {
				n = rc
			}

			pairs[k] = n
		}
	}

	budgetRight := make(map[string]int, len(pairs))

	for k, n := range pairs {
		budgetRight[k] = n
	}

	return &dumpDiffCanceler{key: key, budgetLeft: pairs, budgetRight: budgetRight}
}

func dumpDiffKeyMultiset(path string, key func(map[string]any) string) map[string]int {
	m := map[string]int{}

	streamJSONL(path, func(n map[string]any) {
		m[key(n)]++
	})

	return m
}

func (c *dumpDiffCanceler) cancelLeft(n map[string]any) bool {
	return dumpDiffTakeBudget(c.budgetLeft, c.key(n))
}

func (c *dumpDiffCanceler) cancelRight(n map[string]any) bool {
	return dumpDiffTakeBudget(c.budgetRight, c.key(n))
}

func dumpDiffTakeBudget(m map[string]int, k string) bool {
	if m[k] > 0 {
		m[k]--

		return true
	}

	return false
}

func dumpDiffContentKey(n map[string]any) string {
	h := nodeFieldHashes(n)

	var b [len(h) * 8]byte

	for i, v := range h {
		binary.BigEndian.PutUint64(b[i*8:], v)
	}

	return string(b[:])
}

func dumpDiffSelfUIDKey(n map[string]any) string {
	return getString(n, "self_uid")
}

func findNodesByOutput(path, want string) []map[string]any {
	var found []map[string]any
	streamJSONL(path, func(n map[string]any) {
		for _, o := range toStrings(n["outputs"]) {
			if normPath(o) == want {
				found = append(found, n)

				return
			}
		}
	})

	return found
}

func dumpDiffNodeHostPlatform(n map[string]any) bool {
	host, _ := n["host_platform"].(bool)

	return host
}

func dumpDiffNodeMatchKey(n map[string]any, includeHost bool) string {
	key := nodeKVP(n) + "\x00" + getString(n, "platform")

	if includeHost {
		if dumpDiffNodeHostPlatform(n) {
			key += "\x001"
		} else {
			key += "\x000"
		}
	}

	if dumpDiffNodePicVariant(n) {
		key += "\x00P"
	}

	return key
}

func dumpDiffNodePicVariant(n map[string]any) bool {
	for _, o := range toStrings(n["outputs"]) {
		if strings.HasSuffix(o, ".pic.o") {
			return true
		}
	}

	for _, in := range toStrings(n["inputs"]) {
		if strings.HasSuffix(in, ".pic.o") {
			return true
		}
	}

	for _, t := range cmdArgTokens(n) {
		if strings.HasSuffix(t, ".pic.o") {
			return true
		}
	}

	return false
}

func dumpDiffExactOutputKey(output string, n map[string]any) string {
	return output + "\x00" + dumpDiffNodeMatchKey(n, true)
}

func dumpDiffAxisOutputKey(output string, n map[string]any) string {
	return output + "\x00" + dumpDiffNodeMatchKey(n, false)
}

func setDumpDiffFieldMatch(exact, axis, any map[string]DiffFieldHashes, output string, n map[string]any, h DiffFieldHashes) {
	if _, ok := exact[dumpDiffExactOutputKey(output, n)]; !ok {
		exact[dumpDiffExactOutputKey(output, n)] = h
	}

	if _, ok := axis[dumpDiffAxisOutputKey(output, n)]; !ok {
		axis[dumpDiffAxisOutputKey(output, n)] = h
	}

	if _, ok := any[output]; !ok {
		any[output] = h
	}
}

func findDumpDiffFieldMatch(exact, axis, any map[string]DiffFieldHashes, n map[string]any) (DiffFieldHashes, bool) {
	for _, output := range toStrings(n["outputs"]) {
		if h, ok := exact[dumpDiffExactOutputKey(output, n)]; ok {
			return h, true
		}
	}

	for _, output := range toStrings(n["outputs"]) {
		if h, ok := axis[dumpDiffAxisOutputKey(output, n)]; ok {
			return h, true
		}
	}

	for _, output := range toStrings(n["outputs"]) {
		if h, ok := any[output]; ok {
			return h, true
		}
	}

	return DiffFieldHashes{}, false
}

func setDumpDiffTokenMatch(exact, axis, any map[string]map[string][]string, output string, n map[string]any, rec map[string][]string) {
	if _, ok := exact[dumpDiffExactOutputKey(output, n)]; !ok {
		exact[dumpDiffExactOutputKey(output, n)] = rec
	}

	if _, ok := axis[dumpDiffAxisOutputKey(output, n)]; !ok {
		axis[dumpDiffAxisOutputKey(output, n)] = rec
	}

	if _, ok := any[output]; !ok {
		any[output] = rec
	}
}

func findDumpDiffTokenMatch(exact, axis, any map[string]map[string][]string, n map[string]any) (map[string][]string, bool) {
	for _, output := range toStrings(n["outputs"]) {
		if rec, ok := exact[dumpDiffExactOutputKey(output, n)]; ok {
			return rec, true
		}
	}

	for _, output := range toStrings(n["outputs"]) {
		if rec, ok := axis[dumpDiffAxisOutputKey(output, n)]; ok {
			return rec, true
		}
	}

	for _, output := range toStrings(n["outputs"]) {
		if rec, ok := any[output]; ok {
			return rec, true
		}
	}

	return nil, false
}

func setDumpDiffKindMatch(exact, axis, any map[string]DiffKindRec, output string, n map[string]any, rec DiffKindRec) {
	if _, ok := exact[dumpDiffExactOutputKey(output, n)]; !ok {
		exact[dumpDiffExactOutputKey(output, n)] = rec
	}

	if _, ok := axis[dumpDiffAxisOutputKey(output, n)]; !ok {
		axis[dumpDiffAxisOutputKey(output, n)] = rec
	}

	if _, ok := any[output]; !ok {
		any[output] = rec
	}
}

func findDumpDiffKindMatch(exact, axis, any map[string]DiffKindRec, n map[string]any) (DiffKindRec, bool) {
	for _, output := range toStrings(n["outputs"]) {
		if rec, ok := exact[dumpDiffExactOutputKey(output, n)]; ok {
			return rec, true
		}
	}

	for _, output := range toStrings(n["outputs"]) {
		if rec, ok := axis[dumpDiffAxisOutputKey(output, n)]; ok {
			return rec, true
		}
	}

	for _, output := range toStrings(n["outputs"]) {
		if rec, ok := any[output]; ok {
			return rec, true
		}
	}

	return DiffKindRec{}, false
}

func setDumpDiffSelfMatch(exact, axis, any map[string]string, output string, n map[string]any, selfUID string) {
	if _, ok := exact[dumpDiffExactOutputKey(output, n)]; !ok {
		exact[dumpDiffExactOutputKey(output, n)] = selfUID
	}

	if _, ok := axis[dumpDiffAxisOutputKey(output, n)]; !ok {
		axis[dumpDiffAxisOutputKey(output, n)] = selfUID
	}

	if _, ok := any[output]; !ok {
		any[output] = selfUID
	}
}

func findDumpDiffOutputSelfMatch(exact, axis, any map[string]string, output string, n map[string]any) (string, bool) {
	if selfUID, ok := exact[dumpDiffExactOutputKey(output, n)]; ok {
		return selfUID, true
	}

	if selfUID, ok := axis[dumpDiffAxisOutputKey(output, n)]; ok {
		return selfUID, true
	}

	if selfUID, ok := any[output]; ok {
		return selfUID, true
	}

	return "", false
}

func fnvHash(b []byte) uint64 {
	h := fnv.New64a()

	throw2(h.Write(b))

	return h.Sum64()
}

func outputTopDir(p string) string {
	for _, pre := range []string{"$(B)/", "$(S)/"} {
		if strings.HasPrefix(p, pre) {
			p = p[len(pre):]

			break
		}
	}

	parts := strings.Split(p, "/")

	if len(parts) > 3 {
		parts = parts[:3]
	}

	return strings.Join(parts, "/")
}

func outputExt(p string) string {
	base := p[strings.LastIndex(p, "/")+1:]

	for _, e := range []string{".pic.o", ".global.a", ".pb.cc", ".pb.h", ".o", ".a", ".so", ".cpp", ".c", ".h", ".py", ".pyc", ".bc"} {
		if strings.HasSuffix(base, e) {
			return e
		}
	}

	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[i:]
	}

	return base
}

func writeCountMap(bw *bufio.Writer, prefix string, m map[string]int, top int) {
	type kv struct {
		k string
		c int
	}
	var es []kv

	for k, c := range m {
		es = append(es, kv{k, c})
	}

	sort.Slice(es, func(a, b int) bool {
		if es[a].c != es[b].c {
			return es[a].c > es[b].c
		}

		return es[a].k < es[b].k
	})

	if prefix != "" {
		throw2(fmt.Fprintf(bw, "%s:\n", prefix))
	}

	for i, e := range es {
		if top > 0 && i >= top {
			break
		}

		throw2(fmt.Fprintf(bw, "  %6d  %s\n", e.c, e.k))
	}
}

func writeDiffSection(bw *bufio.Writer, title string, items []string) {
	throw2(fmt.Fprintf(bw, "=== %s (%d) ===\n", title, len(items)))

	for _, s := range items {
		throw2(fmt.Fprintf(bw, "%s\n", s))
	}
}

func setMinus(a, b map[string]bool) []string {
	var out []string

	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}

	sort.Strings(out)

	return out
}

func keysMinus(a, b map[string]map[string]bool) []string {
	var out []string

	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}

	sort.Strings(out)

	return out
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}

	for k := range a {
		if !b[k] {
			return false
		}
	}

	return true
}

func joinSet(m map[string]bool) string {
	keys := make([]string, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return "[" + strings.Join(keys, ",") + "]"
}
