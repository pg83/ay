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

// cmdDumpNormalize is a streaming, two-pass Go port of dev/normalize.py.
// It does not reproduce normalize.py byte-for-byte; it reproduces its
// SEMANTICS, so that two semantically-equal graphs (OUR vs REF) emit an
// identical set of canonical JSONL node lines.
//
// Pass 1 streams every node, canonicalizes its content (minus deps/identity),
// and keeps only metadata in memory: uid→content-hash, uid→deps, the FETCH
// set, and root candidates. Node bodies are not retained.
//
// Between passes: BFS the closure from the target root(s) over deps (FETCH
// and dangling edges excluded), then assign new uids bottom-up — a Merkle
// fold new_uid(n) = base64url(sha256(content_hash(n) ⧺ sorted(child new uids))).
//
// Pass 2 re-streams the file, and for each closure node emits one canonical
// JSONL line with deps rewritten to new uids and uid=self_uid=new_uid. Output
// is UNSORTED; pipe through `ay dump sort` for the canonical total order.
func cmdDumpNormalize(args []string) int {
	defer startProfilesFromEnv()()

	var inPath, target, outPath string

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
		default:
			ThrowFmt("dump normalize: unknown argument %q", args[i])
		}
	}

	if inPath == "" || target == "" {
		ThrowFmt("dump normalize: --in and --target are required")
	}

	// --- Pass 1: content hashes, adjacency, FETCH set, root candidates ---
	contentHash := map[string][32]byte{}
	deps := map[string][]string{}
	fetch := map[string]bool{}

	var ldRoots, tsRoots []string
	type arCand struct {
		uid  string
		host bool
	}
	var arRoots []arCand

	ldPrefix := "$(B)/" + target + "/"
	arInfix := "/" + target + "/"
	tsPrefix := target + "/"

	streamGraph(inPath, func(node map[string]any) {
		uid := getString(node, "uid")
		deps[uid] = toStrings(node["deps"])
		contentHash[uid] = sha256.Sum256(marshalCompact(canonContent(node)))

		kv, _ := node["kv"].(map[string]any)
		p, _ := kv["p"].(string)
		if p == "FETCH" {
			fetch[uid] = true
		}

		outs := toStrings(node["outputs"])
		out0 := ""
		if len(outs) > 0 {
			out0 = normPath(outs[0])
		}

		switch p {
		case "LD":
			if strings.HasPrefix(out0, ldPrefix) {
				ldRoots = append(ldRoots, uid)
			}
		case "AR":
			if strings.Contains(out0, arInfix) {
				host, _ := node["host_platform"].(bool)
				arRoots = append(arRoots, arCand{uid: uid, host: host})
			}
		case "TS":
			path, _ := kv["path"].(string)
			if strings.HasPrefix(path, tsPrefix) {
				tsRoots = append(tsRoots, uid)
			}
		}
	})

	// --- Resolve roots (mirror normalize.py::_find_roots) ---
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

	// --- Closure: BFS over deps, FETCH + dangling edges excluded ---
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

	// --- Bottom-up re-uid (post-order Merkle fold) ---
	newUID := reuidClosure(roots, closure, fetch, deps, contentHash)

	// --- Pass 2: re-stream, emit canonical JSONL for closure nodes ---
	var out io.Writer
	if outPath == "" || outPath == "-" {
		out = os.Stdout
	} else {
		f := Throw2(os.Create(outPath))
		defer func() { Throw(f.Close()) }()
		out = f
	}
	bw := bufio.NewWriterSize(out, 1<<20)

	emitted := 0
	streamGraph(inPath, func(node map[string]any) {
		uid := getString(node, "uid")
		if !closure[uid] {
			return
		}

		canon := canonContent(node)
		canon["deps"] = rewriteDeps(deps[uid], closure, fetch, newUID)
		nu := newUID[uid]
		canon["uid"] = nu
		canon["self_uid"] = nu

		Throw2(bw.Write(marshalCompact(canon)))
		Throw(bw.WriteByte('\n'))
		emitted++
	})
	Throw(bw.Flush())

	return 0
}

// rewriteDeps maps a node's raw deps to canonical tokens: closure children
// become their new uid, dangling edges keep the old uid, FETCH edges drop.
// Result is sorted (canonical, independent of original uid assignment).
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

// reuidClosure computes new uids for every closure node via iterative
// post-order DFS (children finished before parents). new_uid folds the
// node's content hash with the sorted dep tokens (child new uids; dangling
// edges keep old uid). Cycles: a back-edge child not yet finished keeps its
// old uid in the fold.
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
			tokens = append(tokens, d) // dangling or cycle back-edge
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
