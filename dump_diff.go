package main

import (
	"bufio"
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

func cmdDumpDiff(args []string) int {
	var leftPath, rightPath, outPath, mode, pairOut string

	setMode := func(m string) {
		if mode != "" {
			ThrowFmt("dump diff: modes --%s and --%s are mutually exclusive", mode, m)
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
			setMode("roots")
		case "--pair":
			setMode("pair")
			i++
			pairOut = arg(args, i)
		default:
			ThrowFmt("dump diff: unknown argument %q", args[i])
		}
	}

	if leftPath == "" || rightPath == "" {
		ThrowFmt("dump diff: --left and --right are required")
	}

	var w io.Writer = os.Stdout

	if outPath != "" && outPath != "-" {
		f := Throw2(os.Create(outPath))

		defer func() { Throw(f.Close()) }()

		w = f
	}

	bw := bufio.NewWriterSize(w, 1<<20)

	defer func() { Throw(bw.Flush()) }()

	switch mode {
	case "summary":
		diffSummary(leftPath, rightPath, bw)
	case "by-field":
		diffByField(leftPath, rightPath, bw)
	case "by-token":
		diffByToken(leftPath, rightPath, bw)
	case "by-kind":
		diffByKind(leftPath, rightPath, bw)
	case "roots":
		diffRoots(leftPath, rightPath, bw)
	case "pair":
		diffPair(leftPath, rightPath, pairOut, bw)
	default:
		diffSections(leftPath, rightPath, bw)
	}

	return 0
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

	Throw2(fmt.Fprintf(bw, "=== outputs in both with mismatched self_uid (%d) ===\n", len(mismatched)))

	for _, out := range mismatched {
		Throw2(fmt.Fprintf(bw, "%s  left=%s right=%s\n", out, joinSet(leftOut[out]), joinSet(rightOut[out])))
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
		Throw2(fmt.Fprintf(bw, "=== %s (%d) ===\n", title, len(only)))
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
	rExact := map[string]DiffFieldHashes{}
	rAxis := map[string]DiffFieldHashes{}
	ridx := map[string]DiffFieldHashes{}
	streamJSONL(rightPath, func(n map[string]any) {
		h := nodeFieldHashes(n)

		for _, o := range toStrings(n["outputs"]) {
			setDumpDiffFieldMatch(rExact, rAxis, ridx, o, n, h)
		}
	})

	fieldDiff := make([]int, len(dumpContentFields))
	combo := map[string]int{}
	both := 0
	streamJSONL(leftPath, func(n map[string]any) {
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

	Throw2(fmt.Fprintf(bw, "=== by-field: %d outputs in both ===\n", both))
	Throw2(fmt.Fprintf(bw, "\n[content field -> #nodes where it differs]\n"))
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

		Throw2(fmt.Fprintf(bw, "  %6d (%5.1f%%)  %s\n", fieldDiff[i], pct, dumpContentFields[i]))
	}

	Throw2(fmt.Fprintf(bw, "\n[most common differing-field combinations]\n"))
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

func diffByToken(leftPath, rightPath string, bw *bufio.Writer) {
	rExact := map[string]map[string][]string{}
	rAxis := map[string]map[string][]string{}
	ridx := map[string]map[string][]string{}
	streamJSONL(rightPath, func(n map[string]any) {
		rec := map[string][]string{}

		for _, f := range tokenFields {
			rec[f] = tokenize(n, f)
		}

		for _, o := range toStrings(n["outputs"]) {
			setDumpDiffTokenMatch(rExact, rAxis, ridx, o, n, rec)
		}
	})
	our := map[string]map[string]int{}
	ref := map[string]map[string]int{}

	for _, f := range tokenFields {
		our[f], ref[f] = map[string]int{}, map[string]int{}
	}

	paired := 0
	streamJSONL(leftPath, func(n map[string]any) {
		rec, ok := findDumpDiffTokenMatch(rExact, rAxis, ridx, n)

		if !ok {
			return
		}

		paired++

		for _, f := range tokenFields {
			accumMultisetDiff(tokenize(n, f), rec[f], our[f], ref[f])
		}
	})

	Throw2(fmt.Fprintf(bw, "=== by-token: %d outputs in both ===\n", paired))

	for _, f := range tokenFields {
		writeTokenRanking(bw, f+" tokens only in OURS", our[f])
		writeTokenRanking(bw, f+" tokens only in REF", ref[f])
	}
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
	Throw2(fmt.Fprintf(bw, "\n[%s]  (token: #nodes, by category)\n", title))
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

		Throw2(fmt.Fprintf(bw, "  %6d  [%s] %s\n", e.c, tokenCategory(e.t), e.t))
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
	rExact := map[string]DiffKindRec{}
	rAxis := map[string]DiffKindRec{}
	ridx := map[string]DiffKindRec{}
	streamJSONL(rightPath, func(n map[string]any) {
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
		var rr DiffKindRec
		found := false

		if rr, found = findDumpDiffKindMatch(rExact, rAxis, ridx, n); !found {
			return
		}

		kind := nodeKVP(n)

		if kind == "" {
			kind = "(none)"
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
	Throw2(fmt.Fprintf(bw, "=== by-kind: content divergence per node kind ===\n"))
	Throw2(fmt.Fprintf(bw, "%-8s %8s %8s   %s\n", "kind", "paired", "diverge", "top differing fields / combos"))

	for _, k := range kinds {
		fd := fieldDiff[k]
		var parts []string

		for i, f := range dumpContentFields {
			if fd[i] > 0 {
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

		Throw2(fmt.Fprintf(bw, "%-8s %8d %8d   %s   [top combo: %s ×%d]\n",
			k, total[k], diverge[k], strings.Join(parts, " "), topCombo, best))
	}
}

func diffRoots(leftPath, rightPath string, bw *bufio.Writer) {
	rightExact := map[string]string{}
	rightAxis := map[string]string{}
	rightSelf := map[string]string{}
	streamJSONL(rightPath, func(n map[string]any) {
		su := getString(n, "self_uid")

		for _, o := range toStrings(n["outputs"]) {
			setDumpDiffSelfMatch(rightExact, rightAxis, rightSelf, o, n, su)
		}
	})
	divergent := map[string]bool{}
	uidToDivergentOuts := map[string]map[string]bool{}
	uidToDeps := map[string][]string{}
	streamJSONL(leftPath, func(n map[string]any) {
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

	leaves := make([]string, 0, len(leafSet))

	for out := range leafSet {
		leaves = append(leaves, out)
	}

	sort.Strings(leaves)

	Throw2(fmt.Fprintf(bw, "=== roots: %d leaf-most divergent outputs (of %d divergent) ===\n", len(leaves), len(divergent)))
	Throw2(fmt.Fprintf(bw, "(content differs but every dependency child matches the reference — fix these first)\n"))

	for _, o := range leaves {
		Throw2(fmt.Fprintf(bw, "%s\n", o))
	}
}

func diffPair(leftPath, rightPath, output string, bw *bufio.Writer) {
	want := normPath(output)
	left, right := findNodePairByOutput(leftPath, rightPath, want)

	if left == nil {
		ThrowFmt("dump diff --pair: output %q not found in left", output)
	}

	if right == nil {
		ThrowFmt("dump diff --pair: output %q not found in right", output)
	}

	Throw2(fmt.Fprintf(bw, "=== pair diff for %s ===\n", want))

	for _, f := range dumpContentFields {
		if fnvHash(marshalCompact(left[f])) == fnvHash(marshalCompact(right[f])) {
			continue
		}

		Throw2(fmt.Fprintf(bw, "\n[field %s differs]\n", f))

		switch f {
		case "cmds":
			writePairTokens(bw, cmdArgTokens(left), cmdArgTokens(right))
		case "inputs", "tags", "outputs":
			writePairTokens(bw, toStrings(left[f]), toStrings(right[f]))
		default:
			Throw2(fmt.Fprintf(bw, "  ours: %s\n  ref:  %s\n", marshalCompact(left[f]), marshalCompact(right[f])))
		}
	}
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
		Throw2(fmt.Fprintf(bw, "  -ours +%s\n", t))
	}

	for _, t := range rk {
		Throw2(fmt.Fprintf(bw, "  -ref  +%s\n", t))
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

	return key
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
	Throw2(h.Write(b))
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
		Throw2(fmt.Fprintf(bw, "%s:\n", prefix))
	}

	for i, e := range es {
		if top > 0 && i >= top {
			break
		}

		Throw2(fmt.Fprintf(bw, "  %6d  %s\n", e.c, e.k))
	}
}

func writeDiffSection(bw *bufio.Writer, title string, items []string) {
	Throw2(fmt.Fprintf(bw, "=== %s (%d) ===\n", title, len(items)))

	for _, s := range items {
		Throw2(fmt.Fprintf(bw, "%s\n", s))
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
