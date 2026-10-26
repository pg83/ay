package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"

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

func getString(node map[string]any, key string) string {
	s, _ := node[key].(string)

	return s
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

func arg(args []string, i int) string {
	if i >= len(args) {
		throwFmt("dump: missing value for flag %q", args[i-1])
	}

	return args[i]
}
