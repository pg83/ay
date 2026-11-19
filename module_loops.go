package main

import (
	"slices"
	"strconv"
	"strings"
)

type moduleLoops struct {
	order map[VFS]int
	edges map[VFS][]VFS
	names map[VFS]string
	next  int
}

func (l *moduleLoops) edge(from, to VFS) {
	if l.edges == nil {
		l.edges = map[VFS][]VFS{}
	}

	l.edges[from] = append(l.edges[from], to)
}

func (l *moduleLoops) finish(dir VFS, name string) {
	if l.order == nil {
		l.order = map[VFS]int{}
		l.names = map[VFS]string{}
	}

	if _, ok := l.order[dir]; !ok {
		l.order[dir] = l.next
		l.names[dir] = name
		l.next++
	}
}

func (l *moduleLoops) plainEdges(from VFS, plain func(VFS) bool) []VFS {
	if !plain(from) {
		return nil
	}

	var out []VFS

	for _, to := range l.edges[from] {
		if plain(to) {
			out = append(out, to)
		}
	}

	return out
}

func (l *moduleLoops) byOrder(a, b VFS) int {
	return l.order[a] - l.order[b]
}

func (l *moduleLoops) badLoops(plain func(VFS) bool) [][]VFS {
	index, low := map[VFS]int{}, map[VFS]int{}
	onStack := map[VFS]bool{}

	var stack []VFS
	var loops [][]VFS

	counter := 0

	var connect func(v VFS)

	connect = func(v VFS) {
		index[v], low[v] = counter, counter
		counter++
		stack = append(stack, v)
		onStack[v] = true

		for _, to := range l.plainEdges(v, plain) {
			if _, seen := index[to]; !seen {
				connect(to)
				low[v] = min(low[v], low[to])
			} else if onStack[to] {
				low[v] = min(low[v], index[to])
			}
		}

		if low[v] != index[v] {
			return
		}

		var scc []VFS

		for {
			w := stack[len(stack)-1]

			stack = stack[:len(stack)-1]
			onStack[w] = false
			scc = append(scc, w)

			if w == v {
				break
			}
		}

		if len(scc) > 1 {
			slices.SortFunc(scc, l.byOrder)

			loops = append(loops, scc)
		}
	}

	nodes := make([]VFS, 0, len(l.order))

	for m := range l.order {
		nodes = append(nodes, m)
	}

	slices.SortFunc(nodes, l.byOrder)

	for _, v := range nodes {
		if _, seen := index[v]; !seen {
			connect(v)
		}
	}

	slices.SortFunc(loops, func(a, b []VFS) int { return l.byOrder(a[0], b[0]) })

	return loops
}

func (l *moduleLoops) removed(loops [][]VFS, plain func(VFS) bool) []VFS {
	gone := map[VFS]bool{}

	for _, loop := range loops {
		for _, m := range loop {
			gone[m] = true
		}
	}

	for changed := true; changed; {
		changed = false

		for from := range l.edges {
			if !gone[from] && slices.ContainsFunc(l.plainEdges(from, plain), func(to VFS) bool { return gone[to] }) {
				gone[from], changed = true, true
			}
		}
	}

	out := make([]VFS, 0, len(gone))

	for m := range gone {
		out = append(out, m)
	}

	slices.SortFunc(out, l.byOrder)

	return out
}

func (l *moduleLoops) reportBadLoops(loops [][]VFS, removed []VFS, onWarn func(Warn)) {
	for i, loop := range loops {
		var nodes []string

		for _, m := range loop {
			nodes = append(nodes, l.names[m], "$S/"+m.relString())
		}

		onWarn(Warn{Kind: WarnMisconfiguration, Where: "At top level", Error: "Loop " + strconv.Itoa(i+1) + " (size: " + strconv.Itoa(len(nodes)) + ", bad): " + strings.Join(nodes, " --> ")})

		reversed := slices.Clone(nodes)

		slices.Reverse(reversed)

		for _, m := range loop {
			onWarn(Warn{Kind: WarnMisconfiguration, Where: "$S/" + m.relString(), Error: "Loop detected: " + strings.Join(reversed, " --> ")})
		}
	}

	for _, m := range removed {
		onWarn(Warn{Kind: WarnMisconfiguration, Where: "At top level", Error: "the module " + l.names[m] + " will not be built due to deprecated loop"})
	}
}

func moduleNodeName(instance ModuleInstance, result *ModuleEmitResult, decl *ModuleStmt) string {
	switch {
	case result.ARPath != nil:
		return ymakeVFS(*result.ARPath)
	case result.LDPath != nil:
		return ymakeVFS(*result.LDPath)
	case instance.Kind == KindLib:
		return "$B/" + dirPrefix(instance.Path) + archiveNameWithPrefixOrName(instance.Platform, instance.Path.relString(), "lib", "")
	}

	return "$B/" + dirPrefix(instance.Path) + programBinaryName(instance, decl)
}

func (ctx *GenCtx) plainModuleDir(dir VFS) bool {
	evaluation := ctx.dirs.get(dir, ctx.target)

	return evaluation != nil && evaluation.declaration != nil && multimoduleShapes[multimoduleOwner(evaluation.declaration)] == nil
}
