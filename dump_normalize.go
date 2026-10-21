package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
)

const dumpUIDLen = 22

type NormalizeTarget struct {
	name     string
	ldPrefix string
	arInfix  string
	tsPrefix string
	ld       []string
	ar       []string
	ts       []string
}

type NormalizeRoot struct {
	target int
	kind   byte
}

type NormalizeEnumFilter struct {
	uid     string
	inputs  []string
	removed []string
	command string
}

func cmdDumpNormalize(_ GlobalFlags, args []string) int {
	var inPath, outPath string
	var targets []*NormalizeTarget
	var refGraph, streaming bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--in":
			i++
			inPath = arg(args, i)
		case "--target":
			i++

			name := arg(args, i)

			targets = append(targets, &NormalizeTarget{
				name:     name,
				ldPrefix: "$(B)/" + name + "/",
				arInfix:  "/" + name + "/",
				tsPrefix: name + "/",
			})
		case "--out":
			i++
			outPath = arg(args, i)
		case "--ref-graph":

			refGraph = true
		case "--streaming":

			streaming = true
		default:
			throwFmt("dump normalize: unknown argument %q", args[i])
		}
	}

	if inPath == "" || len(targets) == 0 {
		throwFmt("dump normalize: --in and --target are required")
	}

	src := NodeSource{path: inPath}
	workers := 4

	if !streaming {
		src.nodes, src.result = loadGraph(inPath)
		workers = min(runtime.NumCPU(), 24)
	}

	contentHash := map[string][32]byte{}
	deps := map[string][]string{}
	fetch := map[string]bool{}
	outputsByUID := map[string][]string{}
	targetOut0 := map[string]string{}
	arOut0 := map[string]string{}

	var enumFilters []NormalizeEnumFilter

	type p1Result struct {
		uid        string
		deps       []string
		content    [32]byte
		isFetch    bool
		roots      []NormalizeRoot
		targetOut0 string
		arOut0     string
		outputs    []string
		enumFilter *NormalizeEnumFilter
	}

	result := fanoutNodes(src, workers,
		func(node *RawNode) p1Result {
			uid := string(node.UID)
			kv, _ := node.Kv.(map[string]any)
			p, _ := kv["p"].(string)

			r := p1Result{
				uid:     uid,
				deps:    idStrs(node.Deps),
				isFetch: p == "FT",
			}

			var canon map[string]any

			if refGraph && p == "EN" {
				canon = canonContent(node, false)

				rawInputs := canon["inputs"].([]string)
				inputs := filterEnumParserInputs(slices.Clone(rawInputs), node)

				canon["inputs"] = inputs
				var removed []string

				if len(inputs) != len(rawInputs) {
					for _, in := range rawInputs {
						if !slices.Contains(inputs, in) {
							removed = append(removed, in)
						}
					}
				}

				if len(removed) > 0 {
					r.enumFilter = &NormalizeEnumFilter{
						uid:     uid,
						inputs:  slices.Clone(inputs),
						removed: removed,
						command: nodeCmdText(node),
					}
				}
			} else {
				canon = canonContent(node, refGraph)
			}

			r.content = sha256.Sum256(marshalCompact(canon))

			outs := node.Outputs
			out0 := ""

			if len(outs) > 0 {
				out0 = normPath(outs[0])
			}

			if out0 == "$(VCS)/vcs.json" {
				r.isFetch = true
			}

			if refGraph {
				r.outputs = make([]string, 0, len(outs))

				for _, o := range outs {
					r.outputs = append(r.outputs, normPath(o))
				}
			}

			tsPath, _ := kv["path"].(string)

			for t, target := range targets {
				underTarget := strings.HasPrefix(out0, target.ldPrefix)

				if underTarget {
					r.targetOut0 = out0
				}

				switch {
				case p == "LD" && underTarget:
					r.roots = append(r.roots, NormalizeRoot{target: t, kind: 'L'})
				case p == "AR" && strings.Contains(out0, target.arInfix):
					r.roots = append(r.roots, NormalizeRoot{target: t, kind: 'A'})
					r.arOut0 = out0
				case p == "TS" && strings.HasPrefix(tsPath, target.tsPrefix):
					r.roots = append(r.roots, NormalizeRoot{target: t, kind: 'T'})
				}
			}

			return r
		},
		func(r p1Result) {
			contentHash[r.uid] = r.content
			deps[r.uid] = r.deps

			if refGraph {
				outputsByUID[r.uid] = r.outputs
			}

			if r.enumFilter != nil {
				enumFilters = append(enumFilters, *r.enumFilter)
			}

			if r.isFetch {
				fetch[r.uid] = true
			}

			if r.targetOut0 != "" {
				targetOut0[r.uid] = r.targetOut0
			}

			for _, root := range r.roots {
				target := targets[root.target]

				switch root.kind {
				case 'L':
					target.ld = append(target.ld, r.uid)
				case 'A':
					target.ar = append(target.ar, r.uid)
					arOut0[r.uid] = r.arOut0
				case 'T':
					target.ts = append(target.ts, r.uid)
				}
			}
		})

	for _, filter := range enumFilters {
		inputs := make(map[string]struct{}, len(filter.inputs))
		removed := make(map[string]struct{}, len(filter.removed))

		for _, in := range filter.inputs {
			inputs[in] = struct{}{}
		}

		for _, in := range filter.removed {
			removed[in] = struct{}{}
		}

		raw := deps[filter.uid]
		kept := raw[:0]

		for _, d := range raw {
			outs := outputsByUID[d]

			if fetch[d] || !depOutputInInputs(outs, removed) ||
				depOutputInInputs(outs, inputs) || depOutputInCmd(outs, filter.command) {
				kept = append(kept, d)
			}
		}

		deps[filter.uid] = kept
	}

	roots := []string{}

	for _, target := range targets {
		n := len(roots)

		switch {
		case len(target.ld) > 0:
			roots = append(roots, target.ld...)
		case len(target.ar) == 1:
			roots = append(roots, target.ar[0])
		case len(target.ar) > 1:
			for _, uid := range target.ar {
				if strings.HasPrefix(arOut0[uid], target.ldPrefix) {
					roots = append(roots, uid)
				}
			}

			if len(roots) == n {
				throwFmt("dump normalize: %d AR roots for target %q, none under %s", len(target.ar), target.name, target.ldPrefix)
			}
		}

		roots = append(roots, target.ts...)

		if len(roots) == n {
			for _, uid := range result {
				if strings.HasPrefix(targetOut0[string(uid)], target.ldPrefix) {
					roots = append(roots, string(uid))
				}
			}
		}

		if len(roots) == n {
			fmt.Fprintf(os.Stderr, "\x1b[33mdump normalize: no LD/AR/TS root node found for target %q; it contributes no nodes\x1b[0m\n", target.name)
		}
	}

	roots = dedupKeepOrder(roots)

	closure := map[string]bool{}
	queue := append([]string(nil), roots...)

	for len(queue) > 0 {
		uid := queue[0]

		queue = queue[1:]

		if closure[uid] || fetch[uid] {
			continue
		}

		if _, present := contentHash[uid]; !present {
			continue
		}

		closure[uid] = true

		for _, d := range deps[uid] {
			if !fetch[d] && !closure[d] {
				queue = append(queue, d)
			}
		}
	}

	newUID := reuidClosure(roots, closure, fetch, deps, contentHash)

	var out io.Writer

	if outPath == "" || outPath == "-" {
		out = os.Stdout
	} else {
		f := throw2(os.Create(outPath))

		defer func() { throw(f.Close()) }()

		out = f
	}

	bw := bufio.NewWriterSize(out, 1<<20)

	type emitLine struct {
		uid  string
		line []byte
	}

	seen := map[string]bool{}

	fanoutNodes(src, workers,
		func(node *RawNode) emitLine {
			uid := string(node.UID)

			if !closure[uid] {
				return emitLine{}
			}

			canon := canonContent(node, refGraph)

			canon["deps"] = rewriteDeps(deps[uid], closure, fetch, newUID)

			nu := newUID[uid]

			canon["uid"] = nu

			ch := contentHash[uid]

			canon["self_uid"] = base64.RawURLEncoding.EncodeToString(ch[:])[:dumpUIDLen]

			return emitLine{uid: nu, line: append(marshalCompact(canon), '\n')}
		},
		func(e emitLine) {
			if e.line != nil && !seen[e.uid] {
				seen[e.uid] = true
				throw2(bw.Write(e.line))
			}
		})

	throw(bw.Flush())

	return 0
}

func depOutputInInputs(depOutputs []string, inputSet map[string]struct{}) bool {
	for _, o := range depOutputs {
		if o == "" {
			continue
		}

		if _, ok := inputSet[o]; ok {
			return true
		}
	}

	return false
}

func depOutputInCmd(depOutputs []string, cmdText string) bool {
	for _, o := range depOutputs {
		if o == "" {
			continue
		}

		if strings.Contains(cmdText, o) {
			return true
		}
	}

	return false
}

func rewriteDeps(raw []string, closure, fetch map[string]bool, newUID map[string]string) []string {
	out := make([]string, 0, len(raw))

	for _, d := range raw {
		if fetch[d] {
			continue
		}

		if closure[d] {
			out = append(out, newUID[d])
		} else {
			out = append(out, d)
		}
	}

	sort.Strings(out)

	return out
}

func reuidClosure(
	roots []string,
	closure, fetch map[string]bool,
	deps map[string][]string,
	contentHash map[string][32]byte,
) map[string]string {
	newUID := make(map[string]string, len(closure))

	const (
		onStack = 1
		done    = 2
	)

	state := make(map[string]int, len(closure))

	closureChildren := func(uid string) []string {
		var ch []string

		for _, d := range deps[uid] {
			if closure[d] && !fetch[d] {
				ch = append(ch, d)
			}
		}

		sort.Strings(ch)

		return ch
	}

	finish := func(uid string) {
		tokens := make([]string, 0, len(deps[uid]))

		for _, d := range deps[uid] {
			if fetch[d] {
				continue
			}

			if closure[d] {
				if nu, ok := newUID[d]; ok {
					tokens = append(tokens, nu)

					continue
				}
			}

			tokens = append(tokens, d)
		}

		sort.Strings(tokens)

		h := sha256.New()
		ch := contentHash[uid]

		h.Write(ch[:])
		h.Write([]byte{0})
		h.Write([]byte(strings.Join(tokens, "\x00")))
		newUID[uid] = base64.RawURLEncoding.EncodeToString(h.Sum(nil))[:dumpUIDLen]
		state[uid] = done
	}

	type frame struct {
		uid      string
		idx      int
		children []string
	}

	for _, r := range roots {
		if !closure[r] || state[r] == done {
			continue
		}

		stack := []frame{{uid: r, children: closureChildren(r)}}

		state[r] = onStack

		for len(stack) > 0 {
			top := &stack[len(stack)-1]

			if top.idx < len(top.children) {
				child := top.children[top.idx]

				top.idx++

				if state[child] == 0 {
					state[child] = onStack
					stack = append(stack, frame{uid: child, children: closureChildren(child)})
				}

				continue
			}

			finish(top.uid)
			stack = stack[:len(stack)-1]
		}
	}

	return newUID
}

func arg(args []string, i int) string {
	if i >= len(args) {
		throwFmt("dump: missing value for flag %q", args[i-1])
	}

	return args[i]
}
