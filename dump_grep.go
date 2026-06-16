package main

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strings"

	json "github.com/goccy/go-json"
)

func cmdDumpGrep(_ GlobalFlags, args []string) int {
	var inPath string
	raw, substr, useRegex := false, false, false
	var keys []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--in":
			i++
			inPath = arg(args, i)
		case args[i] == "--raw":
			raw = true
		case args[i] == "--substr":
			substr = true
		case args[i] == "--regex":
			useRegex = true
		case strings.HasPrefix(args[i], "--"):
			throwFmt("dump grep: unknown argument %q", args[i])
		default:
			keys = append(keys, args[i])
		}
	}

	if substr && useRegex {
		throwFmt("dump grep: --substr and --regex are mutually exclusive")
	}

	if inPath == "" {
		throwFmt("dump grep: --in is required")
	}

	if len(keys) == 0 {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 1<<20), 1<<26)

		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				keys = append(keys, line)
			}
		}

		throw(sc.Err())
	}

	if len(keys) == 0 {
		throwFmt("dump grep: no keys given (positional args or stdin)")
	}

	var matchStr func(string) bool

	switch {
	case useRegex:
		res := make([]*regexp.Regexp, len(keys))

		for i, k := range keys {
			res[i] = throw2(regexp.Compile(k))
		}

		matchStr = func(s string) bool {
			for _, re := range res {
				if re.MatchString(s) {
					return true
				}
			}

			return false
		}
	case substr:
		want := make([]string, len(keys))

		for i, k := range keys {
			want[i] = strings.TrimSpace(k)
		}

		matchStr = func(s string) bool {
			for _, k := range want {
				if strings.Contains(s, k) {
					return true
				}
			}

			return false
		}
	default:
		want := make(map[string]bool, len(keys))

		for _, k := range keys {
			want[normPath(strings.TrimSpace(k))] = true
		}

		matchStr = func(s string) bool { return want[s] }
	}

	bw := bufio.NewWriterSize(os.Stdout, 1<<20)

	defer func() { throw(bw.Flush()) }()

	exact := !substr && !useRegex
	emit := func(node map[string]any) {
		hit := false

		if exact {
			if matchStr(getString(node, "self_uid")) {
				hit = true
			}

			for _, o := range toStrings(node["outputs"]) {
				if hit {
					break
				}

				if matchStr(normPath(o)) {
					hit = true
				}
			}
		} else {
			hit = matchStr(string(marshalCompact(node)))
		}

		if !hit {
			return
		}

		throw2(bw.Write(throw2(json.MarshalIndent(node, "", "  "))))
		throw(bw.WriteByte('\n'))
	}

	if raw {
		f := throw2(os.Open(inPath))

		defer func() { throw(f.Close()) }()

		dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
		dec.UseNumber()
		seekToGraph(dec, inPath)

		for dec.More() {
			node := map[string]any{}
			throw(dec.Decode(&node))
			emit(node)
		}

		return 0
	}

	f := throw2(os.Open(inPath))

	defer func() { throw(f.Close()) }()

	r := bufio.NewReaderSize(f, 1<<20)

	for {
		line, err := r.ReadString('\n')

		if len(line) > 0 {
			node := map[string]any{}
			throw(json.Unmarshal([]byte(line), &node))
			emit(node)
		}

		if err == io.EOF {
			break
		}

		throw(err)
	}

	return 0
}
