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
	// Dedup adjacent: a node may list the same input more than once — e.g. several
	// wrapper scripts pulling in a shared helper (link_exe and fs_tools both import
	// process_command_files) — but the canonical form is a set.
	w := 0
	for i := range out {
		if i == 0 || out[i] != out[w-1] {
			out[w] = out[i]
			w++
		}
	}
	return out[:w]
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
	// The vcs-stamp header is a real LD input (included by the generated
	// __vcs_version__.c the link compiles) present in both graphs, but the link
	// command never names it — whitelist it so reference-graph pruning keeps it.
	m[ldSvnversionHVFS.Rel()] = true
	return m
}()

// nodeCmdText flattens a node's command arguments into one NUL-joined, normPath'd
// string for whole-path substring testing (used by the dep strip to detect a dep
// output the command names directly, e.g. a TS run_test --context path that is not
// listed among the node's inputs).
func nodeCmdText(node map[string]any) string {
	cmds, _ := node["cmds"].([]any)
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

// nodeCmdBasenames returns the set of file basenames named by a node's command
// arguments. Each cmd_arg token contributes its basename (the part after the last
// '/'); a trailing ':' (the resource archiver's "path:" syntax) is stripped.
// The match is on whole basenames, not substrings, so a source input (foo.c) is
// not spuriously matched against the object token that embeds its name (foo.c.o).
func nodeCmdBasenames(node map[string]any) map[string]struct{} {
	cmds, _ := node["cmds"].([]any)
	set := map[string]struct{}{}
	for _, c := range cmds {
		m, _ := c.(map[string]any)
		for _, a := range toStrings(m["cmd_args"]) {
			b := strings.TrimRight(baseName(normPath(a)), ":")
			set[b] = struct{}{}
		}
	}
	return set
}

// cmdLiteralBasenames are bare-word command arguments that collide with a real
// input's basename but do NOT name that file: "gnu" is the llvm-ar archive-format
// selector (link_lib.py ... LLVM_AR gnu $(B) ...), not the magic-database source
// contrib/libs/libmagic/magic/Magdir/gnu. An input whose basename is one of these
// is never kept via the command-name rule.
var cmdLiteralBasenames = map[string]bool{"gnu": true}

// arLDInputKept decides whether a link/archive input survives reference-graph
// pruning. Keep it if it is a real build artifact / known script (the white
// list), OR if the command names its file — its basename matches a whole basename
// token of the command (cmdBases). ymake propagates the full transitive
// header/source set onto AR/LD nodes as inputs; those are never named by the
// link/ar command (the .o/.a go through a response file, headers/protos aren't
// passed at all), so they fall away — while genuinely-consumed inputs the command
// DOES name (vcs svnversion.h, llvm-link .bc, exports.symlist, the archiver tool,
// archived .pyc) are kept.
func arLDInputKept(s, kind string, cmdBases map[string]struct{}) bool {
	if strings.HasSuffix(s, ".o") || strings.HasSuffix(s, ".a") ||
		strings.HasSuffix(s, ".pyplugin") || strings.HasSuffix(s, ".exports") {
		return true
	}
	if rel, ok := strings.CutPrefix(s, "$(S)/"); ok {
		if kind == "AR" && rel == "build/scripts/link_lib.py" {
			return true
		}
		if kind == "LD" && ldOwnScriptRels[rel] {
			return true
		}
	}
	b := baseName(s)
	if cmdLiteralBasenames[b] {
		return false
	}
	_, ok := cmdBases[b]
	return ok
}

func baseName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func filterARLDInputs(in []string, kind string, cmdBases map[string]struct{}) []string {
	out := in[:0]
	for _, s := range in {
		if arLDInputKept(s, kind, cmdBases) {
			out = append(out, s)
		}
	}
	return out
}

func getString(node map[string]any, key string) string {
	s, _ := node[key].(string)
	return s
}

// canonInputs returns a node's inputs after canonical filtering. filterARLDInputs
// (AR/LD input pruning) is applied ONLY to the upstream reference graph
// (refGraph) — it discounts inputs ymake lists on link/archive nodes that our
// generator does not (or should not) model. Our graph is normalized faithfully so
// that any genuine over- or under-emission surfaces as a diff, and so the filter
// can be tightened to drop only what is really superfluous.
func canonInputs(node map[string]any, refGraph bool) []string {
	inputs := normSortedStrings(node["inputs"])
	kind := nodeProgramKind(node)
	if refGraph && (kind == "AR" || kind == "LD") {
		inputs = filterARLDInputs(inputs, kind, nodeCmdBasenames(node))
	}
	return inputs
}

func canonContent(node map[string]any, refGraph bool) map[string]any {
	inputs := canonInputs(node, refGraph)

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
