package main

import (
	"path/filepath"
	"strconv"
)

const (
	prodCopyFile = iota
	prodConfigureFile
	prodAntlrRun
	prodAntlr4Grammar
	prodBuildInfo
	prodDecimalMD5
	prodSplitCodegen
	prodBaseCodegen
	prodRunProgram
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

func rootedBuildCandidate(modulePath, rel string) VFS {
	if v := moduleRootedVFS(modulePath, rel); v != nil && v.isBuild() {
		return *v
	}

	return 0
}

func (e *EmitContext) producerPositions() []ProducerPos {
	d := e.d
	module := e.instance.Path.rel()

	n := len(d.copyFiles) + len(d.configureFiles) + len(d.antlrRuns) + len(d.antlr4Grammars) +
		len(d.decimalMD5) + len(d.splitCodegens) + len(d.baseCodegens) + len(d.runPrograms) + len(d.runPython)

	if d.createBuildInfoFor != nil {
		n++
	}

	if n == 0 {
		return nil
	}

	total := 2*len(d.copyFiles) + 2*len(d.configureFiles) + 2*len(d.baseCodegens)

	if d.createBuildInfoFor != nil {
		total++
	}

	for _, run := range d.antlrRuns {
		total += len(run.OUTFiles) + len(run.OUTNoAutoFiles) + len(run.INFiles)
	}

	for _, stmt := range d.decimalMD5 {
		total += 1 + len(stmt.Opts)
	}

	for _, sc := range d.splitCodegens {
		total += sc.OutNum + 2
	}

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

	push := func(kind, index, outStart, outEnd int) {
		positions = append(positions, ProducerPos{
			kind:  kind,
			index: index,
			outs:  backing[outStart:outEnd],
			ins:   backing[outEnd:len(backing)],
		})
	}

	runOuts := func(outFiles, outNoAuto []STR, stdout *STR) {
		for _, f := range outFiles {
			backing = append(backing, copyFileOutputVFS(module, f.string()))
		}

		for _, f := range outNoAuto {
			backing = append(backing, copyFileOutputVFS(module, f.string()))
		}

		if stdout != nil {
			backing = append(backing, copyFileOutputVFS(module, stdout.string()))
		}
	}

	for i, entry := range d.copyFiles {
		start := len(backing)

		backing = append(backing, copyFileOutputVFS(module, entry.Dst))

		outEnd := len(backing)

		if v := rootedBuildCandidate(module, entry.Src); v != 0 {
			backing = append(backing, v)
		}

		push(prodCopyFile, i, start, outEnd)
	}

	for i, cf := range d.configureFiles {
		start := len(backing)

		backing = append(backing, copyFileOutputVFS(module, cf.Dst))

		outEnd := len(backing)

		if v := rootedBuildCandidate(module, cf.Src); v != 0 {
			backing = append(backing, v)
		}

		push(prodConfigureFile, i, start, outEnd)
	}

	for i, run := range d.antlrRuns {
		start := len(backing)

		runOuts(run.OUTFiles, run.OUTNoAutoFiles, nil)

		outEnd := len(backing)

		for _, f := range run.INFiles {
			if v := rootedBuildCandidate(module, f.string()); v != 0 {
				backing = append(backing, v)
			}
		}

		push(prodAntlrRun, i, start, outEnd)
	}

	for i := range d.antlr4Grammars {
		push(prodAntlr4Grammar, i, len(backing), len(backing))
	}

	if d.createBuildInfoFor != nil {
		start := len(backing)

		backing = append(backing, build(module, "/", d.createBuildInfoFor.string()))
		push(prodBuildInfo, 0, start, len(backing))
	}

	for i, stmt := range d.decimalMD5 {
		start := len(backing)

		backing = append(backing, copyFileOutputVFS(module, stmt.File))

		outEnd := len(backing)

		for _, opt := range stmt.Opts {
			if v := rootedBuildCandidate(module, opt.string()); v != 0 {
				backing = append(backing, v)
			}
		}

		push(prodDecimalMD5, i, start, outEnd)
	}

	for i, sc := range d.splitCodegens {
		start := len(backing)
		prefix := sc.Prefix.string()

		for part := 0; part < sc.OutNum; part++ {
			backing = append(backing, build(module, "/", prefix, ".", strconv.Itoa(part), ".cpp"))
		}

		backing = append(backing, build(module, "/", prefix, ".cpp"), build(module, "/", prefix, ".h"))
		push(prodSplitCodegen, i, start, len(backing))
	}

	for i, bc := range d.baseCodegens {
		start := len(backing)
		prefix := bc.Prefix.string()

		backing = append(backing, build(module, "/", prefix, ".cpp"), build(module, "/", prefix, ".h"))
		push(prodBaseCodegen, i, start, len(backing))
	}

	runIns := func(inFiles []STR) {
		for _, f := range inFiles {
			if v := runInputBuildCandidate(module, f.string()); v != 0 {
				backing = append(backing, v)
			}
		}
	}

	for i, rp := range d.runPrograms {
		start := len(backing)

		runOuts(rp.OUTFiles, rp.OUTNoAutoFiles, rp.StdoutFile)

		outEnd := len(backing)

		runIns(rp.INFiles)
		push(prodRunProgram, i, start, outEnd)
	}

	for i, rp := range d.runPython {
		start := len(backing)

		runOuts(rp.OUTFiles, rp.OUTNoAutoFiles, rp.StdoutFile)

		outEnd := len(backing)

		runIns(rp.INFiles)
		push(prodRunPython, i, start, outEnd)
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
		case prodCopyFile:
			e.emitCopyFileStmt(e.d.copyFiles[pos.index])
		case prodConfigureFile:
			e.emitExplicitCF(e.d.configureFiles[pos.index])
		case prodAntlrRun:
			e.emitAntlrRunStmt(e.d.antlrRuns[pos.index])
		case prodAntlr4Grammar:
			e.emitAntlr4GrammarStmt(e.d.antlr4Grammars[pos.index])
		case prodBuildInfo:
			e.emitBuildInfoStmt()
		case prodDecimalMD5:
			e.emitDecimalMD5Stmt(e.d.decimalMD5[pos.index])
		case prodSplitCodegen:
			e.emitSplitCodegenStmt(e.d.splitCodegens[pos.index])
		case prodBaseCodegen:
			e.emitBaseCodegen(e.d.baseCodegens[pos.index])
		case prodRunProgram:
			e.emitRunProgramStmt(e.d.runPrograms[pos.index])
		case prodRunPython:
			e.emitRunPythonStmt(e.d.runPython[pos.index])
		}
	}
}
