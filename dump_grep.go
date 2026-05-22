package main

import (
	"bufio"
	"io"
	"os"
	"strings"

	json "github.com/goccy/go-json"
)

// cmdDumpGrep prints, pretty-formatted, every node of a graph whose self_uid
// or one of whose outputs matches a requested key. Keys come from positional
// args, or stdin (one per line) when none are given — so `ay dump diff`
// output can be piped straight in. Default input is JSONL (normalize output);
// --raw streams a raw build-graph JSON. Output paths are matched modulo the
// $(BUILD_ROOT)/$(B) canonicalization.
func cmdDumpGrep(args []string) int {
	var inPath string
	raw := false
	var keys []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--in":
			i++
			inPath = arg(args, i)
		case args[i] == "--raw":
			raw = true
		case strings.HasPrefix(args[i], "--"):
			ThrowFmt("dump grep: unknown argument %q", args[i])
		default:
			keys = append(keys, args[i])
		}
	}

	if inPath == "" {
		ThrowFmt("dump grep: --in is required")
	}

	if len(keys) == 0 {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				keys = append(keys, line)
			}
		}
		Throw(sc.Err())
	}
	if len(keys) == 0 {
		ThrowFmt("dump grep: no keys given (positional args or stdin)")
	}

	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[normPath(strings.TrimSpace(k))] = true
	}

	bw := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer func() { Throw(bw.Flush()) }()

	emit := func(node map[string]any) {
		hit := false
		if su, _ := node["self_uid"].(string); want[su] {
			hit = true
		}
		if !hit {
			for _, o := range toStrings(node["outputs"]) {
				if want[normPath(o)] {
					hit = true
					break
				}
			}
		}
		if !hit {
			return
		}
		Throw2(bw.Write(Throw2(json.MarshalIndent(node, "", "  "))))
		Throw(bw.WriteByte('\n'))
	}

	if raw {
		f := Throw2(os.Open(inPath))
		defer func() { Throw(f.Close()) }()
		dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
		dec.UseNumber()
		seekToGraph(dec, inPath)
		for dec.More() {
			node := map[string]any{}
			Throw(dec.Decode(&node))
			emit(node)
		}
		return 0
	}

	f := Throw2(os.Open(inPath))
	defer func() { Throw(f.Close()) }()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			node := map[string]any{}
			Throw(json.Unmarshal([]byte(line), &node))
			emit(node)
		}
		if err == io.EOF {
			break
		}
		Throw(err)
	}

	return 0
}
