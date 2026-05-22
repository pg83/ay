package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	json "github.com/goccy/go-json"
)

// cmdDumpDiff compares two canonical JSONL graphs (left/right, as emitted by
// `ay dump normalize`) and reports, as sorted lists:
//  1. self_uid present only in left / only in right
//  2. outputs present only in left / only in right
//  3. outputs present in both but whose producing self_uid set differs
//
// Single-pass streaming per side; only compact self_uid/output indexes are
// held in memory (not node bodies).
func cmdDumpDiff(args []string) int {
	var leftPath, rightPath, outPath string

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
		default:
			ThrowFmt("dump diff: unknown argument %q", args[i])
		}
	}

	if leftPath == "" || rightPath == "" {
		ThrowFmt("dump diff: --left and --right are required")
	}

	leftSelf, leftOut := scanDiffIndex(leftPath)
	rightSelf, rightOut := scanDiffIndex(rightPath)

	var w io.Writer
	if outPath == "" || outPath == "-" {
		w = os.Stdout
	} else {
		f := Throw2(os.Create(outPath))
		defer func() { Throw(f.Close()) }()
		w = f
	}
	bw := bufio.NewWriterSize(w, 1<<20)
	defer func() { Throw(bw.Flush()) }()

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

	return 0
}

// scanDiffIndex streams a JSONL graph, returning the set of self_uids and a
// map output→set of producing self_uids.
func scanDiffIndex(path string) (map[string]bool, map[string]map[string]bool) {
	f := Throw2(os.Open(path))
	defer func() { Throw(f.Close()) }()

	selfUIDs := map[string]bool{}
	outToUIDs := map[string]map[string]bool{}

	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			var n struct {
				SelfUID string   `json:"self_uid"`
				Outputs []string `json:"outputs"`
			}
			Throw(json.Unmarshal([]byte(line), &n))
			selfUIDs[n.SelfUID] = true
			for _, out := range n.Outputs {
				if outToUIDs[out] == nil {
					outToUIDs[out] = map[string]bool{}
				}
				outToUIDs[out][n.SelfUID] = true
			}
		}
		if err == io.EOF {
			break
		}
		Throw(err)
	}

	return selfUIDs, outToUIDs
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
