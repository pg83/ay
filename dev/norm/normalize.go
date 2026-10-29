package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	json "github.com/goccy/go-json"
)

const usage = `usage: normalize --in <graph.json> [--out <file>] [--streaming|--auto-streaming] [--ref-graph]
  the closure of the graph's result roots as canonical JSON lines (unsorted)
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) (code int) {
	defer func() {
		if rec := recover(); rec != nil {
			f, ok := rec.(failure)

			if !ok {
				panic(rec)
			}

			fmt.Fprintf(os.Stderr, "normalize: %v\n", f.err)

			code = 1
		}
	}()

	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(os.Stderr, usage)

		return 2
	}

	return normalize(args)
}

type failure struct {
	err error
}

func throw(err error) {
	if err != nil {
		panic(failure{err})
	}
}

func throw2[T any](val T, err error) T {
	throw(err)

	return val
}

func throwFmt(format string, args ...any) {
	panic(failure{fmt.Errorf(format, args...)})
}

var (
	versionedResourceRe = regexp.MustCompile(`\$\(([A-Z][A-Z0-9_]*)-(?:sbr:)?[0-9]+\)`)
	resourceMountFoldRe = regexp.MustCompile(`\$\(B\)/resources/([A-Z_][A-Z0-9_]*)`)
)

func normPath(s string) string {
	if !strings.Contains(s, "$(") {
		return s
	}

	s = strings.ReplaceAll(s, "$(BUILD_ROOT)", "$(B)")
	s = strings.ReplaceAll(s, "$(SOURCE_ROOT)", "$(S)")

	if strings.Contains(s, "$(B)/resources/") {
		s = strings.ReplaceAll(s, "$(B)/resources/CLANG20/", "$(CLANG)/")
		s = resourceMountFoldRe.ReplaceAllString(s, "$($1)")
	}

	s = strings.ReplaceAll(s, "$(B)/vcs.json", "$(VCS)/vcs.json")

	if strings.Contains(s, "-") {
		s = versionedResourceRe.ReplaceAllStringFunc(s, func(m string) string {
			return "$(" + versionedResourceRe.FindStringSubmatch(m)[1] + ")"
		})
	}

	return s
}

func warmEncoder() {
	marshalCompact(map[string]any{
		"any":     []any{"s", json.Number("1"), true, nil, float64(1), map[string]any{}, []any{}, []string{"s"}},
		"strings": []string{"s"},
	})
}

func marshalCompact(v any) []byte {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)

	enc.SetEscapeHTML(false)
	throw(enc.Encode(v))

	b := buf.Bytes()

	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}

	return b
}

func seekToGraph(dec *json.Decoder, path string) {
	tok := throw2(dec.Token())

	if d, ok := tok.(json.Delim); !ok || d != '{' {
		throwFmt("%s: expected top-level JSON object", path)
	}

	for dec.More() {
		key, ok := throw2(dec.Token()).(string)

		if !ok {
			throwFmt("%s: expected object key", path)
		}

		if key == "graph" {
			open := throw2(dec.Token())

			if d, ok := open.(json.Delim); !ok || d != '[' {
				throwFmt("%s: graph is not an array", path)
			}

			return
		}

		var skip json.RawMessage
		throw(dec.Decode(&skip))
	}

	throwFmt("%s: no \"graph\" key found", path)
}

func arg(args []string, i int) string {
	if i >= len(args) {
		throwFmt("missing value for flag %q", args[i-1])
	}

	return args[i]
}

var (
	refLinkKinds      = map[string]bool{"LD": true, "UD": true, "PK": true, "GO": true, "JV": true}
	refLinkPrunedExts = map[string]bool{"h": true, "hpp": true, "inc": true, "pch": true, "cc": true, "cpp": true}
)

func normRec(v any) any {
	switch t := v.(type) {
	case string:
		return normPath(t)
	case []any:
		for i := range t {
			t[i] = normRec(t[i])
		}

		return t
	case map[string]any:
		for k := range t {
			t[k] = normRec(t[k])
		}

		return t
	default:
		return v
	}
}

func normSortedStrings(in []string) []string {
	out := make([]string, len(in))

	for i := range in {
		out[i] = normPath(in[i])
	}

	sort.Strings(out)

	w := 0

	for i := range out {
		if i == 0 || out[i] != out[w-1] {
			out[w] = out[i]
			w++
		}
	}

	return out[:w]
}

func normStringsKeepOrder(in []string) []string {
	out := make([]string, len(in))

	for i := range in {
		out[i] = normPath(in[i])
	}

	return out
}

func orVal(v, def any) any {
	if v == nil {
		return def
	}

	return v
}

type refLinkInputs struct {
	kept   []string
	pruned []string
}

func refLinkInputPruned(path string) bool {
	if !strings.HasPrefix(path, "$(B)/") {
		return false
	}

	base := path[strings.LastIndexByte(path, '/')+1:]
	dot := strings.LastIndexByte(base, '.')

	return dot > 0 && refLinkPrunedExts[base[dot+1:]]
}

func splitRefLinkInputs(node *RawNode, inputs []string) refLinkInputs {
	kv, _ := node.Kv.(map[string]any)
	p, _ := kv["p"].(string)

	if !refLinkKinds[p] {
		return refLinkInputs{kept: inputs}
	}

	link := refLinkInputs{kept: inputs[:0:0]}

	for _, input := range inputs {
		if refLinkInputPruned(input) {
			link.pruned = append(link.pruned, input)
		} else {
			link.kept = append(link.kept, input)
		}
	}

	return link
}

func pruneRefLinkDeps(deps []string, link refLinkInputs, outputs map[string][]string) []string {
	kept := make(map[string]bool, len(link.kept))

	for _, input := range link.kept {
		kept[input] = true
	}

	pruned := make(map[string]bool, len(link.pruned))

	for _, input := range link.pruned {
		pruned[input] = true
	}

	return slices.DeleteFunc(deps, func(dep string) bool {
		hit := false

		for _, out := range outputs[dep] {
			if kept[out] {
				return false
			}

			hit = hit || pruned[out]
		}

		return hit
	})
}

func canonContent(node *RawNode, refGraph bool) (map[string]any, refLinkInputs) {
	inputs := normSortedStrings(node.Inputs)

	var link refLinkInputs

	if refGraph {
		link = splitRefLinkInputs(node, inputs)
		inputs = link.kept
	}

	canon := map[string]any{
		"cmds":         normRec(orVal(node.Cmds, []any{})),
		"env":          normRec(orVal(node.Env, map[string]any{})),
		"inputs":       inputs,
		"kv":           normRec(orVal(node.Kv, map[string]any{})),
		"outputs":      normStringsKeepOrder(node.Outputs),
		"platform":     normPath(node.Platform),
		"requirements": normRec(orVal(node.Requirements, map[string]any{})),
	}

	return canon, link
}

type NodeID string

func (k *NodeID) UnmarshalJSON(b []byte) error {
	return k.unmarshalJSON(b)
}

func (k *NodeID) unmarshalJSON(b []byte) error {
	if len(b) >= 2 && b[0] == '"' {
		*k = NodeID(b[1 : len(b)-1])
	} else {
		*k = NodeID(b)
	}

	return nil
}

func idStrs(ids []NodeID) []string {
	out := make([]string, len(ids))

	for i, id := range ids {
		out[i] = string(id)
	}

	return out
}

type RawNode struct {
	UID          NodeID   `json:"uid"`
	Deps         []NodeID `json:"deps"`
	Inputs       []string `json:"inputs"`
	Outputs      []string `json:"outputs"`
	Platform     string   `json:"platform"`
	Cmds         any      `json:"cmds"`
	Env          any      `json:"env"`
	Kv           any      `json:"kv"`
	Requirements any      `json:"requirements"`
}

func graphSize(file string) int64 {
	return throw2(os.Stat(file)).Size()
}

func loadGraph(path string) ([]*RawNode, []NodeID) {
	f := throw2(os.Open(path))

	defer func() { throw(f.Close()) }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))

	dec.UseNumber()
	seekToGraph(dec, path)

	nodes := []*RawNode{}

	for dec.More() {
		node := &RawNode{}

		throw(dec.Decode(node))
		nodes = append(nodes, node)
	}

	return nodes, readGraphResult(dec, path)
}

type NodeSource struct {
	path   string
	nodes  []*RawNode
	result []NodeID
}

func fanoutNodes[R any](src NodeSource, workers int, process func(*RawNode) R, collect func(R)) []NodeID {
	if src.nodes != nil {
		sliceFanout(src.nodes, workers, process, collect)

		return src.result
	}

	return streamGraphFanout(src.path, workers, process, collect)
}

func sliceFanout[R any](nodes []*RawNode, workers int, process func(*RawNode) R, collect func(R)) {
	in := make(chan *RawNode, workers*2)
	results := make(chan R, workers*2)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for n := range in {
				results <- process(n)
			}
		}()
	}

	done := make(chan struct{})

	go func() {
		for r := range results {
			collect(r)
		}

		close(done)
	}()

	for _, n := range nodes {
		in <- n
	}

	close(in)
	wg.Wait()
	close(results)
	<-done
}

func streamGraphFanout[R any](path string, workers int, process func(*RawNode) R, collect func(R)) []NodeID {
	f := throw2(os.Open(path))

	defer func() { throw(f.Close()) }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))

	dec.UseNumber()
	seekToGraph(dec, path)

	nodes := make(chan *RawNode, workers*2)
	results := make(chan R, workers*2)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for n := range nodes {
				results <- process(n)
			}
		}()
	}

	done := make(chan struct{})

	go func() {
		for r := range results {
			collect(r)
		}

		close(done)
	}()

	for dec.More() {
		node := &RawNode{}

		throw(dec.Decode(node))
		nodes <- node
	}

	close(nodes)
	wg.Wait()
	close(results)
	<-done

	return readGraphResult(dec, path)
}

func readGraphResult(dec *json.Decoder, path string) []NodeID {
	throw2(dec.Token())

	var result []NodeID

	for dec.More() {
		key, ok := throw2(dec.Token()).(string)

		if !ok {
			throwFmt("%s: expected object key", path)
		}

		if key == "result" {
			throw(dec.Decode(&result))

			continue
		}

		var skip json.RawMessage
		throw(dec.Decode(&skip))
	}

	return result
}

const (
	dumpUIDLen          = 22
	streamingGraphBytes = 1 << 30
)

func normalize(args []string) int {
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
			throwFmt("unknown argument %q", args[i])
		}
	}

	if inPath == "" {
		throwFmt("--in is required")
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

	warmEncoder()

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
		throwFmt("graph has no result roots")
	}

	roots := make([]string, 0, len(result))

	for _, uid := range result {
		root := string(uid)

		if _, present := contentHash[root]; !present {
			throwFmt("result root %q is absent from graph", root)
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
