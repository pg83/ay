package main

import (
	"sort"
	"strings"
)

const buildScriptsRoot = "build/scripts"

// scriptDeps maps a build/scripts script's VFS to [self, …transitive import
// closure]. Emit sites use `append(inputs, scripts[v]...)`, so the wrapper and its
// imported helpers land in one append.
type ScriptDeps map[VFS][]VFS

// buildScriptTable parses every build/scripts/*.py and returns, per script VFS, a
// slice of [self, …transitive import closure]. Edges come from real
// `import`/`from … import` statements (see scriptImports), so a new import is
// picked up automatically. Built once per gen.
//
// Multiple wrappers can share a helper, so appending can duplicate one;
// canonInputs dedups, so callers need not.
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

// scriptImports parses the top-level module names a Python script imports via
// `import a, b.c as d` and `from x import y`. Returns the first dotted component of
// each, with `as` aliases and leading relative-import dots stripped. Only true
// import lines are parsed, so identifiers in code/comments/strings are never
// mistaken for imports.
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
