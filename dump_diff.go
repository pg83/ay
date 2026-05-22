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

// cmdDumpDiff compares two canonical JSONL graphs (left=ours, right=ref).
// Modes:
//
//	(default)     three lists: self_uids / outputs only on one side, and
//	              outputs in both with a differing self_uid.
//	--summary     group the only-one-side outputs by kind / ext / dir.
//	--by-field    pair nodes by output; count which content fields differ.
//	--by-token    pair nodes by output; rank cmd_args/input tokens that are
//	              systematically only-ours / only-ref, by category.
//	--roots       content-divergent outputs whose dependency children are all
//	              non-divergent — the leaf-most root causes to fix first.
//	--pair OUTPUT field-by-field diff of the single node producing OUTPUT.
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
	case "roots":
		diffRoots(leftPath, rightPath, bw)
	case "pair":
		diffPair(leftPath, rightPath, pairOut, bw)
	default:
		diffSections(leftPath, rightPath, bw)
	}

	return 0
}

// --- default: three-list sections ---

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

// --- #6 summary: only-one-side outputs grouped by kind / ext / dir ---

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

// --- #1 by-field: per-content-field mismatch counts over paired outputs ---

func diffByField(leftPath, rightPath string, bw *bufio.Writer) {
	type fieldHashes [10]uint64 // len(dumpContentFields)
	ridx := map[string]fieldHashes{}
	streamJSONL(rightPath, func(n map[string]any) {
		h := nodeFieldHashes(n)
		for _, o := range toStrings(n["outputs"]) {
			ridx[o] = h
		}
	})

	fieldDiff := make([]int, len(dumpContentFields))
	combo := map[string]int{}
	both := 0
	streamJSONL(leftPath, func(n map[string]any) {
		var rh fieldHashes
		found := false
		for _, o := range toStrings(n["outputs"]) {
			if v, ok := ridx[o]; ok {
				rh, found = v, true
				break
			}
		}
		if !found {
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
	var h [10]uint64
	for i, f := range dumpContentFields {
		h[i] = fnvHash(marshalCompact(n[f]))
	}
	return h
}

// --- #2 by-token: systematic cmd_args / input token diffs ---

func diffByToken(leftPath, rightPath string, bw *bufio.Writer) {
	type rec struct {
		cmds   []string
		inputs []string
	}
	ridx := map[string]rec{}
	streamJSONL(rightPath, func(n map[string]any) {
		r := rec{cmds: cmdArgTokens(n), inputs: toStrings(n["inputs"])}
		for _, o := range toStrings(n["outputs"]) {
			ridx[o] = r
		}
	})

	cmdOur, cmdRef := map[string]int{}, map[string]int{}
	inOur, inRef := map[string]int{}, map[string]int{}
	paired := 0
	streamJSONL(leftPath, func(n map[string]any) {
		var r rec
		found := false
		for _, o := range toStrings(n["outputs"]) {
			if v, ok := ridx[o]; ok {
				r, found = v, true
				break
			}
		}
		if !found {
			return
		}
		paired++
		accumMultisetDiff(cmdArgTokens(n), r.cmds, cmdOur, cmdRef)
		accumMultisetDiff(toStrings(n["inputs"]), r.inputs, inOur, inRef)
	})

	Throw2(fmt.Fprintf(bw, "=== by-token: %d outputs in both ===\n", paired))
	writeTokenRanking(bw, "cmd_args tokens only in OURS", cmdOur)
	writeTokenRanking(bw, "cmd_args tokens only in REF", cmdRef)
	writeTokenRanking(bw, "input paths only in OURS", inOur)
	writeTokenRanking(bw, "input paths only in REF", inRef)
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

// accumMultisetDiff adds (left-right) tokens to onlyL and (right-left) to onlyR.
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

// --- #5 roots: leaf-most content-divergent outputs ---

func diffRoots(leftPath, rightPath string, bw *bufio.Writer) {
	rightSelf := map[string]string{} // output -> self_uid
	streamJSONL(rightPath, func(n map[string]any) {
		su := getString(n, "self_uid")
		for _, o := range toStrings(n["outputs"]) {
			rightSelf[o] = su
		}
	})

	// left graph structure + divergent outputs
	divergent := map[string]bool{}
	uidToOut := map[string]string{}
	uidToDeps := map[string][]string{}
	streamJSONL(leftPath, func(n map[string]any) {
		su := getString(n, "self_uid")
		uid := getString(n, "uid")
		outs := toStrings(n["outputs"])
		uidToDeps[uid] = toStrings(n["deps"])
		if len(outs) > 0 {
			uidToOut[uid] = outs[0]
		}
		for _, o := range outs {
			if rs, ok := rightSelf[o]; ok && rs != su {
				divergent[o] = true
			}
		}
	})

	// a divergent node is a leaf if none of its deps' outputs are divergent
	var leaves []string
	for uid, out := range uidToOut {
		if !divergent[out] {
			continue
		}
		leaf := true
		for _, d := range uidToDeps[uid] {
			if childOut, ok := uidToOut[d]; ok && divergent[childOut] {
				leaf = false
				break
			}
		}
		if leaf {
			leaves = append(leaves, out)
		}
	}
	sort.Strings(leaves)

	Throw2(fmt.Fprintf(bw, "=== roots: %d leaf-most divergent outputs (of %d divergent) ===\n", len(leaves), len(divergent)))
	Throw2(fmt.Fprintf(bw, "(content differs but every dependency child matches the reference — fix these first)\n"))
	for _, o := range leaves {
		Throw2(fmt.Fprintf(bw, "%s\n", o))
	}
}

// --- #4 pair: field-by-field diff of one output's node ---

func diffPair(leftPath, rightPath, output string, bw *bufio.Writer) {
	want := normPath(output)
	left := findNodeByOutput(leftPath, want)
	right := findNodeByOutput(rightPath, want)
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

func findNodeByOutput(path, want string) map[string]any {
	var found map[string]any
	streamJSONL(path, func(n map[string]any) {
		if found != nil {
			return
		}
		for _, o := range toStrings(n["outputs"]) {
			if normPath(o) == want {
				found = n
				return
			}
		}
	})
	return found
}

// --- shared helpers ---

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
