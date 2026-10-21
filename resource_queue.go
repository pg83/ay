package main

import (
	"slices"
	"strings"
)

type resourceInputKey struct {
	path string
	seq  int
}

type resourceQueueBatch struct {
	entries []ResourceEntry
	order   int
}

type resourceActionState struct {
	order   map[int]int
	sources map[resourceInputKey]VFS
}

type resourceQueueAction struct {
	seq      int
	inputs   []string
	outputs  []VFS
	entries  []ResourceEntry
	deferred bool
}

func (e *EmitContext) resourceActionOrder() map[int]int {
	instance := e.instance

	instance.Demand = demandNone

	makefile := source(instance.Path.relString(), "/ya.make")
	info, _ := e.codegen.sourceModules.get(makefile.rel())

	for ; info != nil; info = info.Next {
		if info.Instance == instance && info.ResourceActions != nil {
			e.resourceSources = info.ResourceActions.sources

			return info.ResourceActions.order
		}
	}

	return e.scheduleResourceActions()
}

func (e *EmitContext) scheduleResourceActions() map[int]int {
	if len(e.d.fromSandboxes)+len(e.d.runPrograms)+len(e.d.runPython) == 0 {
		return nil
	}

	module := e.instance.Path.relString()

	var actions []*resourceQueueAction

	outputs := make(map[VFS]bool)

	addProducer := func(seq int, ins []ANY, outs, noauto []ANY, stdout *ANY) {
		if seq == 0 {
			return
		}

		a := &resourceQueueAction{seq: seq}

		for _, in := range ins {
			a.inputs = append(a.inputs, in.string())
		}

		for _, names := range [][]ANY{outs, noauto} {
			for _, name := range names {
				out := copyFileOutputVFS(module, name.string())

				a.outputs = append(a.outputs, out)
				outputs[out] = true
			}
		}

		if stdout != nil {
			out := copyFileOutputVFS(module, stdout.string())

			a.outputs = append(a.outputs, out)
			outputs[out] = true
		}

		actions = append(actions, a)
	}

	for _, stmt := range e.d.fromSandboxes {
		addProducer(stmt.DeclSeq, nil, stmt.OUTFiles, stmt.OUTNoAutoFiles, nil)
	}

	for _, stmt := range e.d.runPrograms {
		addProducer(stmt.DeclSeq, stmt.INFiles, stmt.OUTFiles, stmt.OUTNoAutoFiles, stmt.StdoutFile)
	}

	for _, stmt := range e.d.runPython {
		ins := append([]ANY{stmt.ScriptPath}, stmt.INFiles...)

		addProducer(stmt.DeclSeq, ins, stmt.OUTFiles, stmt.OUTNoAutoFiles, stmt.StdoutFile)
	}

	for entries := e.resources; len(entries) > 0; {
		n := 1

		for n < len(entries) && !entries[n-1].EndsBatch {
			n++
		}

		a := &resourceQueueAction{seq: entries[0].DeclSeq, entries: entries[:n]}

		entries = entries[n:]
		actions = append(actions, a)
	}

	slices.SortStableFunc(actions, func(a, b *resourceQueueAction) int { return a.seq - b.seq })

	if e.codegen.resourceReady == nil {
		e.codegen.resourceReady = make(map[VFS]bool)
	}

	ready := e.codegen.resourceReady

	inputReady := func(path string, seq int, remember bool) bool {
		key := resourceInputKey{path, seq}

		if e.resourceSources[key] != 0 {
			return true
		}

		output := resourceOutputVFS(module, path)

		if !outputs[output] || ready[output] {
			return true
		}

		src := resolveSourceVFS(e.ctx, e.instance, path, e.d.srcDirs)

		if src.isSource() && e.ctx.fs.isFile(srcRootRel, src.relString()) {
			if remember {
				if e.resourceSources == nil {
					e.resourceSources = make(map[resourceInputKey]VFS)
				}

				key.path = strings.Clone(path)
				e.resourceSources[key] = src
			}

			return true
		}

		return false
	}

	order := make(map[int]int)
	rank := 0

	var firstPending *resourceQueueAction

	for head := 0; head < len(actions); head++ {
		a := actions[head]

		if a == firstPending {
			throwFmt("gen: %s: resource action inputs never become ready", module)
		}

		available := true

		for _, in := range a.inputs {
			if slices.Contains(a.outputs, resourceOutputVFS(module, in)) {
				continue
			}

			if !inputReady(in, 0, false) {
				available = false

				break
			}
		}

		if available {
			for _, entry := range a.entries {
				path := entry.Path

				if path == "-" {
					path = entry.SrcPath

					if path == "" {
						continue
					}
				}

				if !inputReady(path, entry.DeclSeq, true) {
					available = false

					break
				}
			}
		}

		if !available {
			if firstPending == nil {
				firstPending = a
			}

			a.deferred = true
			actions = append(actions, a)

			continue
		}

		firstPending = nil

		for _, output := range a.outputs {
			ready[output] = true
		}

		if len(a.entries) > 0 {
			if a.deferred {
				order[a.entries[0].DeclSeq] = rank
			}

			rank++
		}
	}

	return order
}
