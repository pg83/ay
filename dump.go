package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// cmdDump routes `ay dump <normalize|sort>`. The dump family operates on
// build-graph JSON / canonical JSONL streams for L4 acceptance: `normalize`
// canonicalizes a raw graph into per-node JSONL (semantically equivalent to
// dev/normalize.py); `sort` is a generic external-merge line sorter.
func cmdDump(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ay dump <normalize|sort> [flags]")
		return 2
	}

	switch args[0] {
	case "normalize":
		return cmdDumpNormalize(args[1:])
	case "sort":
		return cmdDumpSort(args[1:])
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
	s = versionedResourceRe.ReplaceAllStringFunc(s, func(m string) string {
		return "$(" + versionedResourceRe.FindStringSubmatch(m)[1] + ")"
	})

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
	canon := map[string]any{
		"cmds":              normRec(orVal(node["cmds"], []any{})),
		"env":               normRec(orVal(node["env"], map[string]any{})),
		"inputs":            normSortedStrings(node["inputs"]),
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

// streamGraph parses the "graph" array of a raw build-graph JSON file and
// calls process once per node. Parsing and processing run in two goroutines
// joined by a channel: the decode loop (CPU-heavy JSON parse) overlaps with
// per-node canonicalization/hashing. process is invoked sequentially by a
// single worker, so it may mutate shared state without locks.
func streamGraph(path string, process func(map[string]any)) {
	f := Throw2(os.Open(path))
	defer func() { Throw(f.Close()) }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	dec.UseNumber()
	seekToGraph(dec, path)

	ch := make(chan map[string]any, 256)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for node := range ch {
			process(node)
		}
	}()

	for dec.More() {
		node := map[string]any{}
		Throw(dec.Decode(&node))
		ch <- node
	}
	close(ch)
	wg.Wait()
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
