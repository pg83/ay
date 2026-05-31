package main

import (
	"sort"
	"strings"
)

const buildScriptsRoot = "build/scripts"

// scriptDepClosure maps a build/scripts Python script (by its $(S)-relative path)
// to the sorted set of OTHER build/scripts scripts it transitively imports. A
// wrapper script the command names directly (link_exe.py, fs_tools.py, …) drags in
// the helper modules it imports as genuine action inputs, even though the command
// line never names those helpers (link_exe imports process_command_files,
// thinlto_cache, process_whole_archive_option; fs_tools imports
// process_command_files; …). Edges come from parsing real `import`/`from … import`
// statements — NOT arbitrary textual occurrences, which would wrongly pick up a
// local variable named `wrapper`, the word `error` in a comment, or a script name
// printed in a usage string.
type scriptDepClosure map[string][]string

func buildScriptDepClosure(fs FS) scriptDepClosure {
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

	closure := make(scriptDepClosure, len(texts))
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
		out := make([]string, 0, len(seen))
		for d := range seen {
			out = append(out, d)
		}
		sort.Strings(out)
		closure[rel] = out
	}
	return closure
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

// expandNodeScriptClosure adds, to a node that lists a build/scripts script as an
// input, that script's transitive helper closure (see scriptDepClosure). Idempotent
// and additive: scripts already present are not duplicated. Mirrors ymake attaching
// a wrapper's imported helpers as inputs of the action that runs the wrapper.
//
// Applied per-node at emit time (by the emitters' Emit), NOT as a post-pass over
// the finished graph: the streaming build path hands each node to the executor as
// soon as its deps resolve, so a node's inputs must be complete by the time it is
// emitted. Both the streaming executor path and the buffered -G dump path run this,
// so they produce identical node content.
func expandNodeScriptClosure(n *Node, closure scriptDepClosure) {
	if len(closure) == 0 || n == nil {
		return
	}
	present := make(map[string]struct{}, len(n.Inputs))
	var seeds []string
	for _, in := range n.Inputs {
		if !in.IsSource() {
			continue
		}
		rel := in.Rel()
		present[rel] = struct{}{}
		if _, ok := closure[rel]; ok {
			seeds = append(seeds, rel)
		}
	}
	for _, seed := range seeds {
		for _, dep := range closure[seed] {
			if _, ok := present[dep]; ok {
				continue
			}
			present[dep] = struct{}{}
			n.Inputs = append(n.Inputs, Source(dep))
		}
	}
}
