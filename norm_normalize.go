//go:build aynorm

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
)

const (
	dumpUIDLen          = 22
	streamingGraphBytes = 1 << 30
)

func normCmdNormalize(args []string) int {
	var inPath, outPath string
	var streaming, autoStreaming, refGraph bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--in":
			i++
			inPath = arg(args, i)
		case "--out":
			i++
			outPath = arg(args, i)
		case "--ref-graph":
			refGraph = true
		case "--streaming":

			streaming = true
		case "--auto-streaming":

			autoStreaming = true
		default:
			throwFmt("normalize: unknown argument %q", args[i])
		}
	}

	if inPath == "" {
		throwFmt("normalize: --in is required")
	}

	if autoStreaming && graphSize(inPath) > streamingGraphBytes {
		streaming = true
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
	outputs := map[string][]string{}
	pruned := map[string]refLinkInputs{}

	type p1Result struct {
		uid     string
		deps    []string
		outputs []string
		content [32]byte
		isFetch bool
		link    refLinkInputs
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

			canon, link := canonContent(node, refGraph)

			r.content = sha256.Sum256(marshalCompact(canon))

			if refGraph {
				r.outputs = normStringsKeepOrder(node.Outputs)
				r.link = link
			}

			outs := node.Outputs
			out0 := ""

			if len(outs) > 0 {
				out0 = normPath(outs[0])
			}

			if out0 == "$(VCS)/vcs.json" {
				r.isFetch = true
			}

			return r
		},
		func(r p1Result) {
			contentHash[r.uid] = r.content
			deps[r.uid] = r.deps

			if r.isFetch {
				fetch[r.uid] = true
			}

			if r.outputs != nil {
				outputs[r.uid] = r.outputs
			}

			if len(r.link.pruned) > 0 {
				pruned[r.uid] = r.link
			}
		})

	rawDeps := make(map[string][]string, len(pruned))

	for uid, link := range pruned {
		rawDeps[uid] = deps[uid]
		deps[uid] = pruneRefLinkDeps(slices.Clone(deps[uid]), link, outputs)
	}

	if len(result) == 0 {
		throwFmt("normalize: graph has no result roots")
	}

	roots := make([]string, 0, len(result))

	for _, uid := range result {
		root := string(uid)

		if _, present := contentHash[root]; !present {
			throwFmt("normalize: result root %q is absent from graph", root)
		}

		roots = append(roots, root)
	}

	seenRoot := map[string]bool{}

	roots = slices.DeleteFunc(roots, func(root string) bool {
		if seenRoot[root] {
			return true
		}

		seenRoot[root] = true

		return false
	})

	if len(rawDeps) > 0 {
		roots = dropOrphanedRoots(roots, deps, rawDeps)
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
		func(node *RawNode) emitLine {
			uid := string(node.UID)

			if !closure[uid] {
				return emitLine{}
			}

			canon, _ := canonContent(node, refGraph)

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

func dropOrphanedRoots(roots []string, deps, rawDeps map[string][]string) []string {
	inner := map[string]bool{}
	queue := []string{}

	for _, root := range roots {
		if raw, ok := rawDeps[root]; ok {
			queue = append(queue, raw...)
		} else {
			queue = append(queue, deps[root]...)
		}
	}

	for len(queue) > 0 {
		uid := queue[len(queue)-1]

		queue = queue[:len(queue)-1]

		if inner[uid] {
			continue
		}

		inner[uid] = true

		if raw, ok := rawDeps[uid]; ok {
			queue = append(queue, raw...)
		} else {
			queue = append(queue, deps[uid]...)
		}
	}

	reached := map[string]bool{}

	for _, root := range roots {
		if !inner[root] {
			queue = append(queue, root)
		}
	}

	for len(queue) > 0 {
		uid := queue[len(queue)-1]

		queue = queue[:len(queue)-1]

		if reached[uid] {
			continue
		}

		reached[uid] = true
		queue = append(queue, deps[uid]...)
	}

	return slices.DeleteFunc(roots, func(root string) bool { return !reached[root] })
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

	return slices.Compact(out)
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
		tokens = slices.Compact(tokens)

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
