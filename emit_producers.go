package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	prodCopyFile = iota
	prodConfigureFile
	prodAntlrRun
	prodAntlr4Grammar
	prodBuildInfo
	prodDecimalMD5
	prodBuildMn
	prodSplitCodegen
	prodBaseCodegen
	prodRunProgram
	prodRunPython
	prodArchiveAsm
	prodSrc
	prodEnum
	prodLj21
	prodArchive
	prodCheckConfigH
	prodCythonAll
	prodSwigAll
	prodJoinSrcs
	prodSproto
	prodLlvmBc
	prodGoCgoCopy
	prodGoCgo1
)

type ProducerPos struct {
	kind  int
	index int
	outs  []VFS
	ins   []VFS
}

func runInputBuildCandidate(modulePath, rel string) VFS {
	if v, ok := moduleRootedVFS(modulePath, rel); ok {
		if v.isBuild() {
			return v
		}

		return 0
	}

	return buildJoinClean(modulePath, rel)
}

func rootedBuildCandidate(modulePath, rel string) VFS {
	if v, ok := moduleRootedVFS(modulePath, rel); ok && v.isBuild() {
		return v
	}

	return 0
}

func relUnderDir(rel, dir string) bool {
	return len(rel) > len(dir) && rel[len(dir)] == '/' && rel[:len(dir)] == dir
}

func (e *EmitContext) requireProducedInput(kind, token string, vfs VFS) VFS {
	module := e.instance.Path.relString()

	if vfs.isBuild() && e.codegen.lookup(vfs) == nil && relUnderDir(vfs.relString(), module) {
		e.ctx.onWarn(Warn{Kind: WarnMissingProducer, Message: fmt.Sprintf("%s: %s %q resolves to build file %s that no declared macro produces", module, kind, token, vfs.string())})
	}

	return vfs
}

func (e *EmitContext) srcPositionOuts(tok STR) []VFS {
	d := e.d

	switch srcExtClassOf(tok.any()) {
	case srcExtProto:
		rel := protoSourceRelPath(e.ctx.fs, e.instance, d, tok.string())
		base := strings.TrimSuffix(rel, ".proto")

		if d.unit.Tag == unitTagPy3Proto {
			outs := []VFS{build(base, "__intpy3___pb2.py")}

			if d.grpc {
				outs = append(outs, build(base, "__intpy3___pb2_grpc.py"))
			}

			return outs
		}

		outs := []VFS{build(base, ".pb.h"), build(base, ".pb.cc")}

		if !protoTransitiveHeadersEnabled(d) {
			outs = append(outs, build(base, ".deps.pb.h"))
		}

		if d.grpc {
			outs = append(outs, build(base, ".grpc.pb.cc"), build(base, ".grpc.pb.h"))
		}

		for _, plugin := range d.cppProtoPlugins {
			for _, suffix := range plugin.OutputSuffixes {
				outs = append(outs, build(base, suffix))
			}
		}

		return outs
	case srcExtEv, srcExtCfgProto:
		rel := protoSourceRelPath(e.ctx.fs, e.instance, d, tok.string())

		return []VFS{build(rel, ".pb.h"), build(rel, ".pb.cc")}
	case srcExtGztProto:
		if d.unit.Tag == unitTagPy3Proto {
			return nil
		}

		return []VFS{build(e.instance.Path.relString(), "/", e.gztGenProtoName(tok.string()))}
	}

	return nil
}

func srcInsCandidate(module string, tok ANY) []VFS {
	if v := runInputBuildCandidate(module, tok.string()); v != 0 {
		return []VFS{v}
	}

	return nil
}

func (e *EmitContext) srcPositionIns(tok STR) []VFS {
	module := e.instance.Path.relString()
	ins := srcInsCandidate(module, tok.any())

	switch srcExtClassOf(tok.any()) {
	case srcExtProto, srcExtEv:
		rel := protoSourceRelPath(e.ctx.fs, e.instance, e.d, tok.string())

		if !e.ctx.fs.isFile(srcRootRel, rel) {
			return ins
		}

		outputRoot := protoCPPOutRoot(e.d)

		protoEachDirectImportName(e.ctx.parsers, rel, func(name string) {
			clean := name

			if name == "" || !pathIsClean(name) {
				clean = filepath.ToSlash(filepath.Clean(name))
			}

			ins = append(ins, build(clean))

			if outputRoot != "" {
				ins = append(ins, build(protoOutputRel(outputRoot, clean)))
			}
		})
	}

	return ins
}

func (e *EmitContext) producerPositions(hasCython bool) ([]ProducerPos, []SrcMeta) {
	d := e.d
	module := e.instance.Path.relString()

	n := len(d.copyFiles) + len(d.configureFiles) + len(d.antlrRuns) + len(d.antlr4Grammars) +
		len(d.decimalMD5) + len(d.splitCodegens) + len(d.baseCodegens) + len(d.runPrograms) + len(d.runPython) +
		len(d.archiveAsm) + len(d.srcs) + len(d.ymapsSprotoSrcs) + len(d.llvmBc) + len(d.enumSrcs) +
		len(d.archives) + len(d.checkConfigHeaders) + len(d.joinSrcs)

	if d.createBuildInfoFor != nil {
		n++
	}

	if d.lj21 != nil || hasCython || len(d.swigC) > 0 {
		n += 3
	}

	if n == 0 {
		return nil, nil
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

	goModule := isGoModuleType(d.moduleStmt.Name)

	if goModule {
		cgoC := goModuleCgoCFiles(d)

		n += len(cgoC)
		total += 2 * len(cgoC)

		if len(d.cgoSrcs) > 0 {
			n++
			total += 3*len(d.cgoSrcs) + 4
		}
	}

	backing := e.prodBacking[:0]
	positions := e.prodPos[:0]

	defer func() {
		e.prodBacking = backing[:0]
		e.prodPos = positions
	}()

	push := func(kind, index, outStart, outEnd int) {
		positions = append(positions, ProducerPos{
			kind:  kind,
			index: index,
			outs:  backing[outStart:outEnd],
			ins:   backing[outEnd:len(backing)],
		})
	}

	runOuts := func(outFiles, outNoAuto []ANY, stdout *ANY) {
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

	for i, stmt := range d.buildMns {
		start := len(backing)

		backing = append(backing, build(module, "/mn.", stmt.Name, ".cpp"), build(module, "/MN_External_", stmt.Name, ".rodata"))
		push(prodBuildMn, i, start, len(backing))
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
		prefix := filepath.Base(bc.Prefix.string())

		backing = append(backing, build(module, "/", prefix, ".cpp"), build(module, "/", prefix, ".h"))
		push(prodBaseCodegen, i, start, len(backing))
	}

	runIns := func(inFiles []ANY) {
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

	if len(d.archiveAsm) > 0 {
		push(prodArchiveAsm, 0, len(backing), len(backing))
	}

	srcs := e.prodSrcs[:0]

	defer func() { e.prodSrcs = srcs }()

	var gztChildren []SrcMeta

	for _, src := range d.srcs {
		if !isCodegenProducingSrcID(src) {
			continue
		}

		srcs = append(srcs, d.srcMetaOf(src))

		if srcExtClassOf(src) == srcExtGztProto && d.unit.Tag != unitTagPy3Proto {
			childMeta := d.srcMetaOf(src)

			childMeta.Source = internStr(e.gztGenProtoName(src.string())).any()
			gztChildren = append(gztChildren, childMeta)
		}
	}

	srcs = append(srcs, gztChildren...)

	for i, m := range srcs {
		positions = append(positions, ProducerPos{
			kind:  prodSrc,
			index: i,
			outs:  e.srcPositionOuts(m.Source.str()),
			ins:   e.srcPositionIns(m.Source.str()),
		})
	}

	if goModule {
		for i, srcTok := range goModuleCgoCFiles(d) {
			positions = append(positions, ProducerPos{
				kind:  prodGoCgoCopy,
				index: i,
				outs:  []VFS{build(module, "/", srcTok.string())},
				ins:   srcInsCandidate(module, srcTok),
			})
		}

		if len(d.cgoSrcs) > 0 {
			outs := make([]VFS, 0, 2*len(d.cgoSrcs)+4)
			ins := make([]VFS, 0, len(d.cgoSrcs))

			for _, f := range d.cgoSrcs {
				base := strings.TrimSuffix(f.string(), ".go")

				outs = append(outs, build(module, "/", base, ".cgo1.go"), build(module, "/", base, ".cgo2.c"))
				ins = append(ins, source(module, "/", f.string()))
			}

			outs = append(outs,
				build(module, "/_cgo_export.h"),
				build(module, "/_cgo_export.c"),
				build(module, "/_cgo_gotypes.go"),
				build(module, "/_cgo_main.c"))

			positions = append(positions, ProducerPos{kind: prodGoCgo1, index: 0, outs: outs, ins: ins})
		}
	}

	for i, srcTok := range d.ymapsSprotoSrcs {
		protoRelPath := protoSourceRelPath(e.ctx.fs, e.instance, d, srcTok.string())

		positions = append(positions, ProducerPos{
			kind:  prodSproto,
			index: i,
			outs:  []VFS{build(strings.TrimSuffix(protoRelPath, ".proto"), ".sproto.h")},
			ins:   srcInsCandidate(module, srcTok),
		})
	}

	if !isProgramModuleType(d.moduleStmt.Name) {
		for i, stmt := range d.llvmBc {
			ins := make([]VFS, 0, len(stmt.Sources))

			for _, src := range stmt.Sources {
				if v := runInputBuildCandidate(module, src); v != 0 {
					ins = append(ins, v)
				}
			}

			positions = append(positions, ProducerPos{
				kind:  prodLlvmBc,
				index: i,
				outs: []VFS{
					build(module, "/", stmt.Name, "_optimized", stmt.Suffix, ".bc"),
					build(module, "/", stmt.Name, "_merged", stmt.Suffix, ".bc"),
				},
				ins: ins,
			})
		}
	}

	for i := range d.enumSrcs {
		stmt := d.enumSrcs[i]
		enDir, enSep, enBase := e.enumSerializedBaseParts(stmt)
		outs := []VFS{build(enDir, enSep, enBase, "_serialized.cpp")}

		if stmt.Variant == "with_header" {
			outs = append(outs, build(enDir, enSep, enBase, "_serialized.h"))
		}

		positions = append(positions, ProducerPos{
			kind:  prodEnum,
			index: i,
			outs:  outs,
			ins:   []VFS{build(e.enumHeaderSourceInput(stmt.Header, d.srcDirs).relString())},
		})
	}

	if d.lj21 != nil {
		outs := make([]VFS, 0, len(d.lj21.Luas))

		for _, lua := range d.lj21.Luas {
			outs = append(outs, build(module, "/", strings.TrimSuffix(lua, ".lua"), ".raw"))
		}

		positions = append(positions, ProducerPos{kind: prodLj21, outs: outs})
	}

	for i, a := range d.archives {
		ins := make([]VFS, 0, len(a.Files))

		for _, f := range a.Files {
			ins = append(ins, copyFileOutputVFS(module, f))
		}

		positions = append(positions, ProducerPos{
			kind:  prodArchive,
			index: i,
			outs:  []VFS{build(module, "/", a.Name)},
			ins:   ins,
		})
	}

	for i, conf := range d.checkConfigHeaders {
		positions = append(positions, ProducerPos{
			kind:  prodCheckConfigH,
			index: i,
			outs:  []VFS{checkConfigHGeneratedVFS(module, conf)},
		})
	}

	if hasCython {
		positions = append(positions, ProducerPos{kind: prodCythonAll})
	}

	if len(d.swigC) > 0 {
		positions = append(positions, ProducerPos{kind: prodSwigAll})
	}

	for i, js := range d.joinSrcs {
		ins := make([]VFS, 0, len(js.Sources))

		for _, src := range js.Sources {
			if v := runInputBuildCandidate(module, src.string()); v != 0 {
				ins = append(ins, v)
			}
		}

		positions = append(positions, ProducerPos{
			kind:  prodJoinSrcs,
			index: i,
			outs:  []VFS{build(module, "/", js.OutputName)},
			ins:   ins,
		})
	}

	return positions, srcs
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

func (e *EmitContext) emitDeclaredProducers(cythonPlans []CythonStmtPlan) {
	positions, srcs := e.producerPositions(len(cythonPlans) > 0)

	if len(positions) == 0 {
		return
	}

	for _, pi := range scheduleProducers(&e.ctx.prodOuts, positions, e.instance.Path.relString()) {
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
		case prodBuildMn:
			e.emitBuildMnStmt(e.d.buildMns[pos.index])
		case prodSplitCodegen:
			e.emitSplitCodegenStmt(e.d.splitCodegens[pos.index])
		case prodBaseCodegen:
			e.emitBaseCodegen(e.d.baseCodegens[pos.index])
		case prodRunProgram:
			e.emitRunProgramStmt(e.d.runPrograms[pos.index])
		case prodRunPython:
			e.emitRunPythonStmt(e.d.runPython[pos.index])
		case prodArchiveAsm:
			e.emitArchiveAsmForAR()
		case prodSrc:
			e.emitOneSource(srcs[pos.index])
		case prodGoCgoCopy:
			e.emitGoCgoCopyStmt(goModuleCgoCFiles(e.d)[pos.index])
		case prodGoCgo1:
			e.emitGoCgo1Stmt()
		case prodEnum:
			e.emitEnumSrcStmt(e.d.enumSrcs[pos.index])
		case prodLj21:
			e.emitLuaJit21()
		case prodArchive:
			e.emitArchiveStmt(e.d.archives[pos.index])
		case prodCheckConfigH:
			e.emitCheckConfigHStmt(e.d.checkConfigHeaders[pos.index])
		case prodCythonAll:
			e.emitCythonCppPlanned(cythonPlans)
		case prodSwigAll:
			e.emitSwigC()
		case prodJoinSrcs:
			e.emitJoinSrcsStmt(e.d.joinSrcs[pos.index])
		case prodSproto:
			e.emitYmapsSprotoStmt(e.d.ymapsSprotoSrcs[pos.index])
		case prodLlvmBc:
			e.emitLlvmBcStmt(e.d.llvmBc[pos.index])
		}
	}
}
