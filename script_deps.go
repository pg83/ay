package main

import (
	"sort"
	"strings"
)

const buildScriptsRoot = "build/scripts"

type ScriptDeps map[VFS][]VFS

func buildScriptTable(fs FS) ScriptDeps {
	texts := map[string]string{}

	fs.walk(buildScriptsRoot, func(rel string, isDir bool) bool {
		if isDir {
			return true
		}

		if strings.HasSuffix(rel, ".py") {
			texts[rel] = string(fs.read(rel))
		}

		return false
	})

	byStem := make(map[string]string, len(texts))

	for rel := range texts {
		base := rel[strings.LastIndexByte(rel, '/')+1:]

		byStem[strings.TrimSuffix(base, ".py")] = rel
	}

	direct := make(map[string]map[string]bool, len(texts))

	for rel, txt := range texts {
		deps := map[string]bool{}

		for _, mod := range scriptImports(txt) {
			if target, ok := byStem[mod]; ok && target != rel {
				deps[target] = true
			}
		}

		direct[rel] = deps
	}

	table := make(ScriptDeps, len(texts))

	for rel := range texts {
		seen := map[string]bool{}
		stack := make([]string, 0, len(direct[rel]))

		for d := range direct[rel] {
			stack = append(stack, d)
		}

		for len(stack) > 0 {
			d := stack[len(stack)-1]

			stack = stack[:len(stack)-1]

			if d == rel || seen[d] {
				continue
			}

			seen[d] = true

			for d2 := range direct[d] {
				if !seen[d2] {
					stack = append(stack, d2)
				}
			}
		}

		deps := make([]string, 0, len(seen))

		for d := range seen {
			deps = append(deps, d)
		}

		sort.Strings(deps)

		out := make([]VFS, 1, 1+len(deps))

		out[0] = source(rel)

		for _, d := range deps {
			out = append(out, source(d))
		}

		table[source(rel)] = out
	}

	return table
}

func scriptImports(txt string) []string {
	var out []string

	for _, line := range strings.Split(txt, "\n") {
		l := strings.TrimSpace(line)

		var mods string

		switch {
		case strings.HasPrefix(l, "import "):
			mods = l[len("import "):]
		case strings.HasPrefix(l, "from "):
			rest := l[len("from "):]

			if i := strings.Index(rest, " import"); i >= 0 {
				rest = rest[:i]
			}

			mods = rest
		default:
			continue
		}

		for _, part := range strings.Split(mods, ",") {
			p := strings.TrimSpace(part)

			if k := strings.Index(p, " as "); k >= 0 {
				p = strings.TrimSpace(p[:k])
			}

			p = strings.TrimLeft(p, ".")

			if d := strings.IndexByte(p, '.'); d >= 0 {
				p = p[:d]
			}

			if p != "" {
				out = append(out, p)
			}
		}
	}

	return out
}
