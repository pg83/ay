package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	json "github.com/goccy/go-json"
)

func cmdDump(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ay dump <normalize|sort|diff|grep> [flags]")
		return 2
	}

	switch args[0] {
	case "normalize":
		return cmdDumpNormalize(args[1:])
	case "sort":
		return cmdDumpSort(args[1:])
	case "diff":
		return cmdDumpDiff(args[1:])
	case "grep":
		return cmdDumpGrep(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown dump subcommand: %s\n", args[0])
		return 2
	}
}

var versionedResourceRe = regexp.MustCompile(`\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)`)

func normPath(s string) string {
	if !strings.Contains(s, "$(") {
		return s
	}

	s = strings.ReplaceAll(s, "$(BUILD_ROOT)", "$(B)")
	s = strings.ReplaceAll(s, "$(SOURCE_ROOT)", "$(S)")

	if strings.Contains(s, "CLANG-") || strings.Contains(s, "LLD_ROOT-") || strings.Contains(s, "YMAKE_PYTHON3-") {
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

func normSortedStrings(v any) []string {
	out := toStrings(v)
	for i := range out {
		out[i] = normPath(out[i])
	}
	sort.Strings(out)
	return out
}

func normStringsKeepOrder(v any) []string {
	out := toStrings(v)
	for i := range out {
		out[i] = normPath(out[i])
	}
	return out
}

func orVal(v, def any) any {
	if v == nil {
		return def
	}
	return v
}

func nodeProgramKind(node map[string]any) string {
	kv, _ := node["kv"].(map[string]any)
	p, _ := kv["p"].(string)
	return p
}

var ldOwnScriptRels = func() map[string]bool {
	m := map[string]bool{"build/scripts/link_dyn_lib.py": true}
	for _, v := range ldScriptInputs {
		m[v.Rel()] = true
	}
	return m
}()

func arLDInputKept(s, kind string) bool {
	if isHeaderSource(s) {
		return false
	}
	if strings.HasSuffix(s, ".o") || strings.HasSuffix(s, ".a") ||
		strings.HasSuffix(s, ".pyplugin") || strings.HasSuffix(s, ".exports") {
		return true
	}
	rel, ok := strings.CutPrefix(s, "$(S)/")
	if !ok {
		return false
	}
	if kind == "AR" {
		return rel == "build/scripts/link_lib.py"
	}
	return ldOwnScriptRels[rel]
}

func filterARLDInputs(in []string, kind string) []string {
	out := in[:0]
	for _, s := range in {
		if arLDInputKept(s, kind) {
			out = append(out, s)
		}
	}
	return out
}

// cpScriptRels are CP-node auxiliary scripts (the python copy tooling). They
// legitimately appear in CP nodes' inputs; upstream additionally splatters them
// into the inputs of unrelated nodes that transitively depend on a CP product
// (CC compiles consuming a COPY_FILE-generated header pick up these scripts as
// inputs of the CP cascade). We don't model that splatter — and don't want to,
// since the scripts have no effect on the consumer's compile. Filter them out
// of non-CP nodes' inputs so the comparison is fair.
var cpScriptRels = map[string]struct{}{
	"build/scripts/fs_tools.py":             {},
	"build/scripts/process_command_files.py": {},
}

func filterNonCPCascadeScripts(in []string) []string {
	out := in[:0]
	for _, s := range in {
		rel, ok := strings.CutPrefix(s, "$(S)/")
		if ok {
			if _, drop := cpScriptRels[rel]; drop {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func getString(node map[string]any, key string) string {
	s, _ := node[key].(string)
	return s
}

func canonContent(node map[string]any) map[string]any {
	inputs := normSortedStrings(node["inputs"])

	kind := nodeProgramKind(node)
	if kind == "AR" || kind == "LD" {
		inputs = filterARLDInputs(inputs, kind)
	}

	if kind != "CP" {
		inputs = filterNonCPCascadeScripts(inputs)
	}
	canon := map[string]any{
		"cmds":              normRec(orVal(node["cmds"], []any{})),
		"env":               normRec(orVal(node["env"], map[string]any{})),
		"inputs":            inputs,
		"kv":                normRec(orVal(node["kv"], map[string]any{})),
		"outputs":           normStringsKeepOrder(node["outputs"]),
		"platform":          normPath(getString(node, "platform")),
		"requirements":      normRec(orVal(node["requirements"], map[string]any{})),
		"sandboxing":        true,
		"target_properties": normRec(orVal(node["target_properties"], map[string]any{})),
		"tags":              normSortedStrings(node["tags"]),
	}

	if b, ok := node["host_platform"].(bool); ok && b {
		canon["host_platform"] = true
	}

	return canon
}

func marshalCompact(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	Throw(enc.Encode(v))

	b := buf.Bytes()
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	return b
}

func streamGraphFanout[R any](path string, workers int, process func(map[string]any) R, collect func(R)) {
	f := Throw2(os.Open(path))
	defer func() { Throw(f.Close()) }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	dec.UseNumber()
	seekToGraph(dec, path)

	nodes := make(chan map[string]any, workers*2)
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
		node := map[string]any{}
		Throw(dec.Decode(&node))
		nodes <- node
	}
	close(nodes)
	wg.Wait()
	close(results)
	<-done
}

func streamJSONL(path string, fn func(map[string]any)) {
	f := Throw2(os.Open(path))
	defer func() { Throw(f.Close()) }()

	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			n := map[string]any{}
			Throw(json.Unmarshal([]byte(line), &n))
			fn(n)
		}
		if err == io.EOF {
			break
		}
		Throw(err)
	}
}

var dumpContentFields = []string{
	"cmds", "env", "inputs", "kv", "outputs",
	"platform", "requirements", "tags", "target_properties", "host_platform",
}

func nodeKVP(n map[string]any) string {
	kv, _ := n["kv"].(map[string]any)
	p, _ := kv["p"].(string)
	return p
}

func seekToGraph(dec *json.Decoder, path string) {
	tok := Throw2(dec.Token())
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		ThrowFmt("dump: %s: expected top-level JSON object", path)
	}

	for dec.More() {
		key, ok := Throw2(dec.Token()).(string)
		if !ok {
			ThrowFmt("dump: %s: expected object key", path)
		}

		if key == "graph" {
			open := Throw2(dec.Token())
			if d, ok := open.(json.Delim); !ok || d != '[' {
				ThrowFmt("dump: %s: graph is not an array", path)
			}
			return
		}

		var skip json.RawMessage
		Throw(dec.Decode(&skip))
	}

	ThrowFmt("dump: %s: no \"graph\" key found", path)
}
