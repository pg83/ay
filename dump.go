package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	json "github.com/goccy/go-json"
)

var (
	versionedResourceRe = regexp.MustCompile(`\$\(([A-Z][A-Z0-9_]*)-(?:sbr:)?[0-9]+\)`)
	resourceMountFoldRe = regexp.MustCompile(`\$\(B\)/resources/([A-Z_][A-Z0-9_]*)`)
)

var dumpContentFields = []string{
	"cmds", "env", "inputs", "kv", "outputs",
	"platform", "requirements",
}

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

func toStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))

	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}

	return out
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

func nodeProgramKind(node *RawNode) string {
	kv, _ := node.Kv.(map[string]any)
	p, _ := kv["p"].(string)

	return p
}

func nodeCmdText(node *RawNode) string {
	cmds, _ := node.Cmds.([]any)

	var b strings.Builder

	for _, c := range cmds {
		m, _ := c.(map[string]any)

		for _, a := range toStrings(m["cmd_args"]) {
			b.WriteString(normPath(a))
			b.WriteByte('\x00')
		}
	}

	return b.String()
}

func filterEnumParserInputs(in []string, node *RawNode) []string {
	cmds, _ := node.Cmds.([]any)

	if len(cmds) != 1 {
		return in
	}

	m, _ := cmds[0].(map[string]any)
	args := toStrings(m["cmd_args"])

	if len(args) != 6 && (len(args) != 8 || args[6] != "--header") {
		return in
	}

	tool := normPath(args[0])

	if !strings.HasSuffix(tool, "/tools/enum_parser/enum_parser/enum_parser") || args[2] != "--include-path" || args[4] != "--output" {
		return in
	}

	header := normPath(args[1])
	out := in[:0]

	for _, s := range in {
		if s == tool || s == header {
			out = append(out, s)
		}
	}

	return out
}

func getString(node map[string]any, key string) string {
	s, _ := node[key].(string)

	return s
}

func canonInputs(node *RawNode, refGraph bool) []string {
	inputs := normSortedStrings(node.Inputs)

	if refGraph && nodeProgramKind(node) == "EN" {
		return filterEnumParserInputs(inputs, node)
	}

	return inputs
}

func canonContent(node *RawNode, refGraph bool) map[string]any {
	inputs := canonInputs(node, refGraph)

	canon := map[string]any{
		"cmds":         normRec(orVal(node.Cmds, []any{})),
		"env":          normRec(orVal(node.Env, map[string]any{})),
		"inputs":       inputs,
		"kv":           normRec(orVal(node.Kv, map[string]any{})),
		"outputs":      normStringsKeepOrder(node.Outputs),
		"platform":     normPath(node.Platform),
		"requirements": normRec(orVal(node.Requirements, map[string]any{})),
	}

	return canon
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

func streamJSONL(path string, fn func(map[string]any)) {
	f := throw2(os.Open(path))

	defer func() { throw(f.Close()) }()

	r := bufio.NewReaderSize(f, 1<<20)

	for {
		line, err := r.ReadString('\n')

		if len(line) > 0 {
			n := map[string]any{}

			throw(json.Unmarshal([]byte(line), &n))
			fn(n)
		}

		if err == io.EOF {
			break
		}

		throw(err)
	}
}

func nodeKVP(n map[string]any) string {
	kv, _ := n["kv"].(map[string]any)
	p, _ := kv["p"].(string)

	return p
}

func seekToGraph(dec *json.Decoder, path string) {
	tok := throw2(dec.Token())

	if d, ok := tok.(json.Delim); !ok || d != '{' {
		throwFmt("dump: %s: expected top-level JSON object", path)
	}

	for dec.More() {
		key, ok := throw2(dec.Token()).(string)

		if !ok {
			throwFmt("dump: %s: expected object key", path)
		}

		if key == "graph" {
			open := throw2(dec.Token())

			if d, ok := open.(json.Delim); !ok || d != '[' {
				throwFmt("dump: %s: graph is not an array", path)
			}

			return
		}

		var skip json.RawMessage
		throw(dec.Decode(&skip))
	}

	throwFmt("dump: %s: no \"graph\" key found", path)
}

func readGraphResult(dec *json.Decoder, path string) []NodeID {
	throw2(dec.Token())

	var result []NodeID

	for dec.More() {
		key, ok := throw2(dec.Token()).(string)

		if !ok {
			throwFmt("dump: %s: expected object key", path)
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
