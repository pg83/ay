package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
)

const dumpUIDLen = 22

func cmdDumpNormalize(_ GlobalFlags, args []string) int {
	defer startProfilesFromEnv()()

	var inPath, target, outPath string
	var refGraph, streaming bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--in":
			i++
			inPath = arg(args, i)
		case "--target":
			i++
			target = arg(args, i)
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

	if inPath == "" || target == "" {
		throwFmt("dump normalize: --in and --target are required")
	}

	src := nodeSource{path: inPath}
	workers := 4

	if !streaming {
		src.nodes = loadGraph(inPath)
		workers = min(runtime.NumCPU(), 24)
	}

	contentHash := map[string][32]byte{}
	deps := map[string][]string{}
	fetch := map[string]bool{}
	outputsByUID := map[string][]string{}

	var ldRoots, tsRoots, arRoots []string

	ldPrefix := "$(B)/" + target + "/"
	arInfix := "/" + target + "/"
	tsPrefix := target + "/"

	type p1Result struct {
		uid      string
		deps     []string
		content  [32]byte
		isFetch  bool
		rootKind byte
		outputs  []string
	}

	fanoutNodes(src, workers,
		func(node *rawNode) p1Result {
			uid := node.UID
			kv, _ := node.Kv.(map[string]any)
			p, _ := kv["p"].(string)

			r := p1Result{
				uid:     uid,
				deps:    node.Deps,
				content: sha256.Sum256(marshalCompact(canonContent(node, refGraph))),
				isFetch: p == "FT",
			}

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

			switch p {
			case "LD":
				if strings.HasPrefix(out0, ldPrefix) {
					r.rootKind = 'L'
				}
			case "AR":
				if strings.Contains(out0, arInfix) {
					r.rootKind = 'A'
				}
			case "TS":
				path, _ := kv["path"].(string)

				if strings.HasPrefix(path, tsPrefix) {
					r.rootKind = 'T'
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

			if r.isFetch {
				fetch[r.uid] = true
			}

			switch r.rootKind {
			case 'L':
				ldRoots = append(ldRoots, r.uid)
			case 'A':
				arRoots = append(arRoots, r.uid)
			case 'T':
				tsRoots = append(tsRoots, r.uid)
			}
		})

	if refGraph {
		type stripResult struct {
			uid  string
			deps []string
		}

		fanoutNodes(src, workers,
			func(node *rawNode) stripResult {
				uid := node.UID
				inputSet := make(map[string]struct{})

				for _, in := range canonInputs(node, refGraph) {
					inputSet[in] = struct{}{}
				}

				cmdText := nodeCmdText(node)
				raw := node.Deps
				kept := make([]string, 0, len(raw))

				for _, d := range raw {
					outs := outputsByUID[d]

					if fetch[d] || len(outs) == 0 ||
						depOutputInInputs(outs, inputSet) || depOutputInCmd(outs, cmdText) {
						kept = append(kept, d)
					}
				}

				return stripResult{uid: uid, deps: kept}
			},
			func(r stripResult) {
				deps[r.uid] = r.deps
			})
	}

	roots := []string{}

	if len(ldRoots) > 0 || len(arRoots) > 0 {
		switch {
		case len(ldRoots) == 1:
			roots = append(roots, ldRoots[0])
		case len(ldRoots) > 1:
			throwFmt("dump normalize: %d LD roots for target %q; expected 1", len(ldRoots), target)
		case len(arRoots) == 1:
			roots = append(roots, arRoots[0])
		default:
			throwFmt("dump normalize: %d AR roots for target %q; expected 1", len(arRoots), target)
		}
	}

	roots = append(roots, tsRoots...)
	roots = dedupKeepOrder(roots)

	if len(roots) == 0 {
		throwFmt("dump normalize: no LD/AR/TS root node found for target %q", target)
	}

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
		func(node *rawNode) emitLine {
			uid := node.UID

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
