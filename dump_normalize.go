package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"sort"
	"strings"
)

const dumpUIDLen = 22

func cmdDumpNormalize(args []string) int {
	defer startProfilesFromEnv()()

	var inPath, target, outPath string
	var stripDeps bool

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
		case "--strip-unreferenced-deps":
			// Drop build-order-only dep edges: an edge u->d is removed when none of
			// d's outputs is among u's inputs (the files u's action reads). Intended
			// for the UPSTREAM graph only, to discount ymake TJSONVisitor induced
			// NodeDeps (e.g. ydbd's 350 .pb.h/.gen.h edges — never link inputs) that
			// our generator does not model. Our graph is normalized WITHOUT this flag
			// so any superfluous dep we emit still surfaces in the diff.
			stripDeps = true
		default:
			ThrowFmt("dump normalize: unknown argument %q", args[i])
		}
	}

	if inPath == "" || target == "" {
		ThrowFmt("dump normalize: --in and --target are required")
	}

	const workers = 4

	contentHash := map[string][32]byte{}
	deps := map[string][]string{}
	fetch := map[string]bool{}
	// outputsByUID (populated only under --strip-unreferenced-deps) maps each
	// node to its output paths so the strip pass can resolve, for an edge u->d,
	// what d produces and check it against u's inputs. Compact (1-3 strings/node).
	outputsByUID := map[string][]string{}

	var ldRoots, tsRoots []string
	type arCand struct {
		uid  string
		host bool
	}
	var arRoots []arCand

	ldPrefix := "$(B)/" + target + "/"
	arInfix := "/" + target + "/"
	tsPrefix := target + "/"

	type p1Result struct {
		uid      string
		deps     []string
		content  [32]byte
		isFetch  bool
		rootKind byte
		arHost   bool
		outputs  []string
	}

	streamGraphFanout(inPath, workers,
		func(node map[string]any) p1Result {
			uid := getString(node, "uid")
			kv, _ := node["kv"].(map[string]any)
			p, _ := kv["p"].(string)

			r := p1Result{
				uid:     uid,
				deps:    toStrings(node["deps"]),
				content: sha256.Sum256(marshalCompact(canonContent(node))),
				isFetch: p == "FETCH",
			}

			outs := toStrings(node["outputs"])
			out0 := ""
			if len(outs) > 0 {
				out0 = normPath(outs[0])
			}
			if stripDeps {
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
					r.arHost, _ = node["host_platform"].(bool)
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
			if stripDeps {
				outputsByUID[r.uid] = r.outputs
			}
			if r.isFetch {
				fetch[r.uid] = true
			}
			switch r.rootKind {
			case 'L':
				ldRoots = append(ldRoots, r.uid)
			case 'A':
				arRoots = append(arRoots, arCand{uid: r.uid, host: r.arHost})
			case 'T':
				tsRoots = append(tsRoots, r.uid)
			}
		})

	// Optional strip pass (upstream only): drop dep edges u->d where none of d's
	// outputs is among u's inputs. The induced codegen .pb.h/.gen.h producers
	// ymake hangs off a link node for cache-key Merkle folding are never link
	// inputs, so they go; the real .o/.a (which ARE inputs, even though link
	// commands pass them via a response file rather than naming them in cmd_args)
	// stay. Runs before the Merkle re-uid. Reads inputs transiently; only the
	// compact outputsByUID lives across the pass.
	if stripDeps {
		type stripResult struct {
			uid  string
			deps []string
		}
		streamGraphFanout(inPath, workers,
			func(node map[string]any) stripResult {
				uid := getString(node, "uid")
				inputSet := make(map[string]struct{})
				for _, in := range canonInputs(node) {
					inputSet[in] = struct{}{}
				}
				raw := toStrings(node["deps"])
				kept := make([]string, 0, len(raw))
				for _, d := range raw {
					outs := outputsByUID[d]
					if fetch[d] || len(outs) == 0 || depOutputInInputs(outs, inputSet) {
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
			ThrowFmt("dump normalize: %d LD roots for target %q; expected 1", len(ldRoots), target)
		case len(arRoots) == 1:
			roots = append(roots, arRoots[0].uid)
		default:
			var nonHost []string
			for _, c := range arRoots {
				if !c.host {
					nonHost = append(nonHost, c.uid)
				}
			}
			if len(nonHost) == 1 {
				roots = append(roots, nonHost[0])
			} else {
				ThrowFmt("dump normalize: %d AR roots for target %q; expected 1", len(arRoots), target)
			}
		}
	}
	roots = append(roots, tsRoots...)
	roots = dedupKeepOrder(roots)
	if len(roots) == 0 {
		ThrowFmt("dump normalize: no LD/AR/TS root node found for target %q", target)
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
		f := Throw2(os.Create(outPath))
		defer func() { Throw(f.Close()) }()
		out = f
	}
	bw := bufio.NewWriterSize(out, 1<<20)

	streamGraphFanout(inPath, workers,
		func(node map[string]any) []byte {
			uid := getString(node, "uid")
			if !closure[uid] {
				return nil
			}

			canon := canonContent(node)
			canon["deps"] = rewriteDeps(deps[uid], closure, fetch, newUID)

			canon["uid"] = newUID[uid]
			ch := contentHash[uid]
			canon["self_uid"] = base64.RawURLEncoding.EncodeToString(ch[:])[:dumpUIDLen]

			return append(marshalCompact(canon), '\n')
		},
		func(line []byte) {
			if line != nil {
				Throw2(bw.Write(line))
			}
		})
	Throw(bw.Flush())

	return 0
}

// depOutputInInputs reports whether any of a dep's outputs is among the
// consuming node's inputs (the files its action reads).
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

func dedupKeepOrder(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func arg(args []string, i int) string {
	if i >= len(args) {
		ThrowFmt("dump: missing value for flag %q", args[i-1])
	}
	return args[i]
}
