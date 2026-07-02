package main

import (
	"path/filepath"
)

const (
	prodRunProgram = iota
	prodRunPython
)

type ProducerPos struct {
	kind  int
	index int
	outs  []VFS
	ins   []VFS
}

func runInputBuildCandidate(modulePath, rel string) VFS {
	if v := moduleRootedVFS(modulePath, rel); v != nil {
		if v.isBuild() {
			return *v
		}

		return 0
	}

	return build(filepath.ToSlash(filepath.Clean(modulePath + "/" + rel)))
}

func (e *EmitContext) producerPositions() []ProducerPos {
	d := e.d
	module := e.instance.Path.rel()
	n := len(d.runPrograms) + len(d.runPython)

	if n == 0 {
		return nil
	}

	total := 0

	for _, rp := range d.runPrograms {
		total += len(rp.OUTFiles) + len(rp.OUTNoAutoFiles) + len(rp.INFiles)

		if rp.StdoutFile != nil {
			total++
		}
	}

	for _, rp := range d.runPython {
		total += len(rp.OUTFiles) + len(rp.OUTNoAutoFiles) + len(rp.INFiles)

		if rp.StdoutFile != nil {
			total++
		}
	}

	backing := make([]VFS, 0, total)
	positions := make([]ProducerPos, 0, n)

	appendPos := func(kind, index int, outFiles, outNoAuto []STR, stdout *STR, inFiles []STR) {
		start := len(backing)

		for _, f := range outFiles {
			backing = append(backing, copyFileOutputVFS(module, f.string()))
		}

		for _, f := range outNoAuto {
			backing = append(backing, copyFileOutputVFS(module, f.string()))
		}

		if stdout != nil {
			backing = append(backing, copyFileOutputVFS(module, stdout.string()))
		}

		outEnd := len(backing)

		for _, f := range inFiles {
			if v := runInputBuildCandidate(module, f.string()); v != 0 {
				backing = append(backing, v)
			}
		}

		positions = append(positions, ProducerPos{
			kind:  kind,
			index: index,
			outs:  backing[start:outEnd],
			ins:   backing[outEnd:len(backing)],
		})
	}

	for i, rp := range d.runPrograms {
		appendPos(prodRunProgram, i, rp.OUTFiles, rp.OUTNoAutoFiles, rp.StdoutFile, rp.INFiles)
	}

	for i, rp := range d.runPython {
		appendPos(prodRunPython, i, rp.OUTFiles, rp.OUTNoAutoFiles, rp.StdoutFile, rp.INFiles)
	}

	return positions
}

func scheduleProducers(m *IdValueMap, positions []ProducerPos, modulePath string) []int {
	m.reset(vfsBound())

	for i, p := range positions {
		for _, out := range p.outs {
			m.put(out, int32(i))
		}
	}

	type edge struct {
		from int32
		to   int32
	}

	var edges []edge

	indeg := make([]int32, len(positions))

	for i, p := range positions {
		for _, in := range p.ins {
			from, ok := m.get(in)

			if !ok || from == int32(i) {
				continue
			}

			edges = append(edges, edge{from: from, to: int32(i)})
			indeg[i]++
		}
	}

	order := make([]int, 0, len(positions))

	if len(edges) == 0 {
		for i := range positions {
			order = append(order, i)
		}

		return order
	}

	done := make([]bool, len(positions))

	for len(order) < len(positions) {
		picked := -1

		for i := range positions {
			if !done[i] && indeg[i] == 0 {
				picked = i

				break
			}
		}

		if picked < 0 {
			throwFmt("gen: %s declares a dependency cycle among generating macros", modulePath)
		}

		done[picked] = true
		order = append(order, picked)

		for _, ed := range edges {
			if ed.from == int32(picked) {
				indeg[ed.to]--
			}
		}
	}

	return order
}

func (e *EmitContext) emitDeclaredProducers() {
	positions := e.producerPositions()

	if len(positions) == 0 {
		return
	}

	for _, pi := range scheduleProducers(&e.ctx.prodOuts, positions, e.instance.Path.rel()) {
		pos := positions[pi]

		switch pos.kind {
		case prodRunProgram:
			e.emitRunProgramStmt(e.d.runPrograms[pos.index])
		case prodRunPython:
			e.emitRunPythonStmt(e.d.runPython[pos.index])
		}
	}
}
