package main

import (
	"sort"
	"strings"
)

const buildScriptsRoot = "build/scripts"

// scriptDeps maps a build/scripts script's VFS to [self, …transitive import
// closure] (all VFS). Threaded from genCtx to emit sites, which add a build script
// with `append(inputs, scripts[v]...)` so the wrapper and the helpers it imports
// land in one append.
type scriptDeps map[VFS][]VFS

// buildScriptTable parses every build/scripts/*.py and returns, for each script's
// VFS, a slice whose FIRST element is the script itself followed by its transitive
// import closure (the other build/scripts it imports), all as VFS. Emit sites that
// put a build script into a node's inputs use `append(inputs, scripts[v]...)`, so
// the wrapper and the helper scripts it pulls in land in a single append. Edges come
// from real `import`/`from … import` statements (see scriptImports), so a new import
// is picked up automatically — no hand-maintained closure lists. Built once per gen.
//
// Multiple wrappers can share a helper (link_exe and fs_tools both import
// process_command_files), so appending several scripts to one node can duplicate a
// helper; canonInputs dedups, so callers need not.
func buildScriptTable(fs FS) scriptDeps {
	texts := map[string]string{}
	fs.Walk(buildScriptsRoot, func(rel string, isDir bool) {
		if isDir || !strings.HasSuffix(rel, ".py") {
			return
		}
		texts[rel] = string(fs.Read(rel))
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

	table := make(scriptDeps, len(texts))
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
		out[0] = Source(rel)
		for _, d := range deps {
			out = append(out, Source(d))
		}
		table[Source(rel)] = out
	}
	return table
}

// scriptImports parses the top-level module names a Python script imports via
// `import a, b.c as d` and `from x import y` statements. Returns the first
// dotted component of each (the package a flat build/scripts module resolves by),
// with `as` aliases and leading relative-import dots stripped. Only true import
// lines are parsed, so identifiers in code, comments, or strings are never
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

