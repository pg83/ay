package main

import (
	"slices"
	"strings"
)

type isolatedNode struct {
	children map[string]*isolatedNode
	project  string
	shadowed bool
}

type IsolatedProjects struct {
	root    isolatedNode
	outside BitSet
}

func (ctx *GenCtx) loadIsolatedProjects(env Environment) *IsolatedProjects {
	files := strings.Fields(env.expand(env.string(env_ISOLATED_PROJECTS)))

	if len(files) == 0 {
		return nil
	}

	var projects []string

	for _, file := range files {
		if !ctx.fs.isFile(srcRootRel, file) {
			abs := ctx.fs.root() + "/" + file

			ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: "At top level", Error: "Error while reading blacklist file " + file + `: (Error 2: No such file or directory) util/system/file.cpp:936: can't open "` + abs + `" with mode RdOnly|Seq (0x00000028)`})

			continue
		}

		for _, line := range strings.FieldsFunc(string(concatChunks(ctx.fs.read(file))), func(r rune) bool { return r == '\n' || r == '\r' }) {
			if hash := strings.IndexByte(line, '#'); hash >= 0 {
				line = line[:hash]
			}

			if path, ok := normalizeBlacklistPath(strings.TrimSpace(line)); ok {
				projects = append(projects, path)
			}
		}
	}

	slices.Sort(projects)

	var kept []string

	for _, project := range slices.Compact(projects) {
		if n := len(kept); n == 0 || !pathIsPrefixOf(kept[n-1], project) {
			kept = append(kept, project)
		}
	}

	p := &IsolatedProjects{}

	for i, project := range kept {
		node := &p.root

		for _, c := range strings.Split(project, "/") {
			if node.children == nil {
				node.children = map[string]*isolatedNode{}
			}

			if node.children[c] == nil {
				node.children[c] = &isolatedNode{}
			}

			node = node.children[c]
		}

		node.project = project
		node.shadowed = i+1 < len(kept) && strings.HasPrefix(kept[i+1], project) && kept[i+1][len(project)] < '/'
	}

	return p
}

func (p *IsolatedProjects) projectOf(path string) string {
	for node, rest := &p.root, path; rest != ""; {
		c, tail, _ := strings.Cut(rest, "/")

		if node = node.children[c]; node == nil {
			return ""
		}

		if node.project != "" {
			if node.shadowed && tail != "" {
				return ""
			}

			return node.project
		}

		rest = tail
	}

	return ""
}

func (p *IsolatedProjects) enters(from, to string) string {
	if project := p.projectOf(to); project != "" && !pathIsPrefixOf(project, from) {
		return project
	}

	return ""
}

func isolatedProjectError(project, from, to string) string {
	return "The project depends on [[alt1]]isolated project[[rst]] [[imp]]" + project + "[[rst]]\n\n    " + from + " -> " + to
}

func (ctx *GenCtx) checkModuleIsolation(r *ModuleEmitResult, d *ModuleData) {
	p := ctx.isolated
	name, from := "", ""

	report := func(project, to string) {
		ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(r.transitiveDir()), Error: isolatedProjectError(project, name, to)})
	}

	enters := func(to string) string {
		if name == "" {
			if name = r.transitiveName(); r.LDPath != nil {
				name = ymakeVFS(*r.LDPath)
			}

			from = name[len("$B/"):]
		}

		return p.enters(from, to)
	}

	for _, peer := range r.testPeers {
		if !peer.inIsolated {
			continue
		}

		peerName := peer.transitiveName()

		if project := enters(peerName[len("$B/"):]); project != "" {
			report(project, peerName)
		}

		if project := enters(peer.transitiveDir()); project != "" {
			report(project, "$S/"+peer.transitiveDir())
		}
	}

	for _, data := range d.test.data {
		if rel, ok := strings.CutPrefix(data, "arcadia/"); ok {
			if project := enters(strings.TrimRight(rel, "/")); project != "" {
				report(project, "$S/"+strings.TrimRight(rel, "/"))
			}
		}
	}
}

func (ctx *GenCtx) isolatedIncludes(v VFS, children []VFS) {
	if ctx.whereModule == 0 {
		return
	}

	p := ctx.isolated

	for _, child := range children {
		if p.outside.has(uint32(child)) {
			continue
		}

		project := p.projectOf(child.relString())

		if project == "" {
			p.outside.add(uint32(child))

			continue
		}

		if !pathIsPrefixOf(project, v.relString()) {
			ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(ctx.whereModule.relString()), Error: isolatedProjectError(project, ymakeVFS(v), ymakeVFS(child))})
		}
	}
}

func (p *IsolatedProjects) contains(dir string) bool {
	for node, rest := &p.root, dir; rest != ""; {
		c, tail, _ := strings.Cut(rest, "/")

		if node = node.children[c]; node == nil {
			return false
		}

		if node.project != "" {
			return true
		}

		rest = tail
	}

	return false
}
