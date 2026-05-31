package main

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strings"

	json "github.com/goccy/go-json"
)

func cmdDumpGrep(args []string) int {
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
			ThrowFmt("dump grep: unknown argument %q", args[i])
		default:
			keys = append(keys, args[i])
		}
	}

	if substr && useRegex {
		ThrowFmt("dump grep: --substr and --regex are mutually exclusive")
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

	var matchStr func(string) bool

	switch {
	case useRegex:
		res := make([]*regexp.Regexp, len(keys))

		for i, k := range keys {
			res[i] = Throw2(regexp.Compile(k))
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

	defer func() { Throw(bw.Flush()) }()

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
