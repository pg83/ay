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

	// goccy/go-json is a drop-in encoding/json replacement (~2-3x faster
	// decode); its Delim/Token/RawMessage are aliases of encoding/json's,
	// and it sorts map keys on marshal, so canonicalization stays
	// deterministic. Used only by the dump streaming path.
	json "github.com/goccy/go-json"
)

// cmdDump routes `ay dump <normalize|sort>`. The dump family operates on
// build-graph JSON / canonical JSONL streams for L4 acceptance: `normalize`
// canonicalizes a raw graph into per-node JSONL (semantically equivalent to
// dev/normalize.py); `sort` is a generic external-merge line sorter.
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

// versionedResourceRe collapses sandbox-versioned resource roots
// ($(CLANG-243881345) → $(CLANG)) so OUR and REF compare uniformly.
// Mirrors dev/normalize.py's load-time regex.
var versionedResourceRe = regexp.MustCompile(`\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)`)

// normPath applies the non-semantic textual canonicalizations
// dev/normalize.py runs over the raw bytes pre-parse, but per-string so the
// streaming decoder never holds the whole file: $(BUILD_ROOT)→$(B),
// $(SOURCE_ROOT)→$(S), and versioned-resource collapse.
func normPath(s string) string {
	if !strings.Contains(s, "$(") {
		return s
	}

	s = strings.ReplaceAll(s, "$(BUILD_ROOT)", "$(B)")
	s = strings.ReplaceAll(s, "$(SOURCE_ROOT)", "$(S)")

	// The versioned-resource collapse is rare; the regex is ~11% of the
	// run if applied to every "$(" string. Gate it on a cheap substring
	// check for an actual "$(NAME-" before paying for the regex.
	if strings.Contains(s, "CLANG-") || strings.Contains(s, "LLD_ROOT-") || strings.Contains(s, "YMAKE_PYTHON3-") {
		s = versionedResourceRe.ReplaceAllStringFunc(s, func(m string) string {
			return "$(" + versionedResourceRe.FindStringSubmatch(m)[1] + ")"
		})
	}

	return s
}

// normRec recursively applies normPath to every string leaf of a decoded
// JSON value (maps, slices, strings), mutating in place.
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

// nodeProgramKind returns the node's kv.p tag ("AR", "LD", "CC", ...) or "".
func nodeProgramKind(node map[string]any) string {
	kv, _ := node["kv"].(map[string]any)
	p, _ := kv["p"].(string)
	return p
}

// ldOwnScriptRels is the set of build scripts an LD/dynlib node emits itself —
// its link wrapper plus the helper scripts that wrapper drives. Built from the
// emission constants (composeLDInputs's ldScriptInputs + the dynlib
// link_dyn_lib.py) so it cannot drift from what we emit.
var ldOwnScriptRels = func() map[string]bool {
	m := map[string]bool{"build/scripts/link_dyn_lib.py": true}
	for _, v := range ldScriptInputs {
		m[v.Rel] = true
	}
	return m
}()

// arLDInputKept reports whether s belongs in a `kind` (AR/LD) node's inputs:
// an object/archive it bundles (.o/.a), the ar plugin (.pyplugin), a linker
// version script (.exports), or the node's OWN command script — link_lib.py
// for AR, the link wrappers/helpers for LD. Never a header. Everything else
// is the members' transitive source/header closure plus codegen wrapper
// scripts (cpp_proto_wrapper.py, ...) that ride in through it — none of which
// the ar/link command reads.
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

// filterARLDInputs keeps only a `kind` node's real inputs (see arLDInputKept),
// dropping the member source/header closure + transitive codegen wrappers.
func filterARLDInputs(in []string, kind string) []string {
	out := in[:0]
	for _, s := range in {
		if arLDInputKept(s, kind) {
			out = append(out, s)
		}
	}
	return out
}

func getString(node map[string]any, key string) string {
	s, _ := node[key].(string)
	return s
}

// canonContent builds the canonical node content used as the self_uid hash
// input: every kept field EXCEPT deps/uid/self_uid (those are folded /
// assigned during re-uid). Drops stats_uid, cache, foreign_deps; omits
// host_platform when false; forces sandboxing=true; sorts inputs/tags.
// Mirrors dev/normalize.py::_strip_and_canonicalize (minus identity/deps).
func canonContent(node map[string]any) map[string]any {
	inputs := normSortedStrings(node["inputs"])
	// AR/LD nodes consume only the objects/archives the command bundles plus
	// the node's OWN scripts (its link wrapper + helpers). Upstream also lists
	// every member CC's transitive source+header closure AND the codegen
	// wrapper scripts that ride in through it (cpp_proto_wrapper.py, ...) —
	// none of which the ar/link command reads. Keep only the real inputs in
	// BOTH graphs; drop the rest.
	if kind := nodeProgramKind(node); kind == "AR" || kind == "LD" {
		inputs = filterARLDInputs(inputs, kind)
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

// marshalCompact emits compact JSON with sorted map keys (encoding/json
// sorts map keys) and no HTML escaping, no trailing newline. Both OUR and
// REF run through this identical path, so the byte form is shared.
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

// streamGraphFanout parses the "graph" array of a raw build-graph JSON file
// and fans nodes out to `workers` parallel processors. A single decoder
// goroutine feeds a node channel; each worker runs process() (CPU-heavy
// canonicalization / hashing / marshaling) and sends its result to a single
// collector running collect(), which may therefore mutate shared state
// without locks. Results arrive in completion order — callers must not
// depend on input order (the dump pipeline sorts downstream).
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

// streamJSONL calls fn once per line of a JSONL graph (the normalize output),
// decoding each line into a node map. Single goroutine; fn runs sequentially.
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

// dumpContentFields are the canonical node fields that feed self_uid (deps,
// uid, self_uid, sandboxing excluded); the diff analyses compare these.
var dumpContentFields = []string{
	"cmds", "env", "inputs", "kv", "outputs",
	"platform", "requirements", "tags", "target_properties", "host_platform",
}

// nodeKVP returns kv["p"] (node kind: CC/AR/LD/PB/...), "" if absent.
func nodeKVP(n map[string]any) string {
	kv, _ := n["kv"].(map[string]any)
	p, _ := kv["p"].(string)
	return p
}

// seekToGraph advances dec to the first element of the top-level "graph"
// array, skipping any other top-level keys (conf, inputs, result).
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
