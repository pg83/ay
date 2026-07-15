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

func (e *EmitContext) prodVFSTake(mark int) []VFS {
	n := len(e.prodVFS)

	if n == mark {
		return nil
	}

	return e.prodVFS[mark:n:n]
}

func (e *EmitContext) srcPosition(tok STR) ([]VFS, []VFS) {
	d := e.d
	src := tok.string()
	class := srcExtClassOf(tok.any())
	outsMark := len(e.prodVFS)
	var protoRelID STR
	protoRel := ""

	switch class {
	case srcExtProto, srcExtEv, srcExtCfgProto:
		protoRelID = e.protoSourceRel(src)
		protoRel = protoRelID.string()
	}

	switch class {
	case srcExtProto:
		base := strings.TrimSuffix(protoRel, ".proto")

		if d.unit.Tag == unitTagPy3Proto {
			e.prodVFS = append(e.prodVFS, build(base, "__intpy3___pb2.py"))

			if d.grpc {
				e.prodVFS = append(e.prodVFS, build(base, "__intpy3___pb2_grpc.py"))
			}
		} else {
			e.prodVFS = append(e.prodVFS, build(base, ".pb.h"), build(base, ".pb.cc"))

			if !protoTransitiveHeadersEnabled(d) {
				e.prodVFS = append(e.prodVFS, build(base, ".deps.pb.h"))
			}

			if d.grpc {
				e.prodVFS = append(e.prodVFS, build(base, ".grpc.pb.cc"), build(base, ".grpc.pb.h"))
			}

			for _, plugin := range d.cppProtoPlugins {
				for _, suffix := range plugin.OutputSuffixes {
					e.prodVFS = append(e.prodVFS, build(base, suffix))
				}
			}
		}
	case srcExtEv, srcExtCfgProto:
		e.prodVFS = append(e.prodVFS, build(protoRel, ".pb.h"), build(protoRel, ".pb.cc"))
	case srcExtGztProto:
		if d.unit.Tag != unitTagPy3Proto {
			e.prodVFS = append(e.prodVFS, build(e.instance.Path.relString(), "/", e.gztGenProtoName(src)))
		}
	}

	outs := e.prodVFSTake(outsMark)
	module := e.instance.Path.relString()
	insMark := len(e.prodVFS)

	if v := runInputBuildCandidate(module, src); v != 0 {
		e.prodVFS = append(e.prodVFS, v)
	}

	switch class {
	case srcExtProto, srcExtEv:
		if e.ctx.fs.isFile(srcRootRel, protoRel) {
			outputRoot := protoCPPOutRoot(e.d)
			outputRootClean := outputRoot != "" && pathIsClean(outputRoot)

			for _, directive := range e.ctx.parsers.sourceParsedBuckets(protoRelID.source(), nil).bucket(parsedIncludesLocal) {
				nameID := directive.target.str()
				name := ""

				if nameID != 0 {
					name = nameID.string()
				} else {
					name = directive.target.string()
				}

				if nameID != 0 && name != "" && pathIsClean(name) {
					e.prodVFS = append(e.prodVFS, nameID.build())

					if outputRootClean {
						e.prodVFS = append(e.prodVFS, internV(outputRoot, "/", name).build())
					} else if outputRoot != "" {
						e.prodVFS = append(e.prodVFS, build(protoOutputRel(outputRoot, name)))
					}

					continue
				}

				clean := filepath.ToSlash(filepath.Clean(name))
				e.prodVFS = append(e.prodVFS, build(clean))

				if outputRoot != "" {
					e.prodVFS = append(e.prodVFS, build(protoOutputRel(outputRoot, clean)))
				}
			}
		}
	}

	return outs, e.prodVFSTake(insMark)
}

func (e *EmitContext) srcInsCandidate(tok ANY) []VFS {
	if v := runInputBuildCandidate(e.instance.Path.relString(), tok.string()); v != 0 {
		e.prodVFS = append(e.prodVFS, v)

		n := len(e.prodVFS)

		return e.prodVFS[n-1 : n : n]
	}

	return nil
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

	goModule := isGoModuleType(d.moduleStmt.Name)

	backing := e.prodBacking[:0]
	positions := e.prodPos[:0]

	defer func() {
		e.prodBacking = backing[:0]
		e.prodPos = retainMaxLen(e.prodPos, positions)
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

	for _, meta := range d.srcs {
		src := meta.Source

		if !isCodegenProducingSrcID(src) {
			continue
		}

		srcs = append(srcs, meta)
	}

	srcCount := len(srcs)

	for i := 0; i < srcCount; i++ {
		meta := srcs[i]
		src := meta.Source

		if srcExtClassOf(src) == srcExtGztProto && d.unit.Tag != unitTagPy3Proto {
			childMeta := meta

			childMeta.Source = internStr(e.gztGenProtoName(src.string())).any()
			srcs = append(srcs, childMeta)
		}
	}

	for i, m := range srcs {
		outs, ins := e.srcPosition(m.Source.str())

		positions = append(positions, ProducerPos{
			kind:  prodSrc,
			index: i,
			outs:  outs,
			ins:   ins,
		})
	}

	if goModule {
		for i, srcTok := range goModuleCgoCFiles(d) {
			outsMark := len(e.prodVFS)

			e.prodVFS = append(e.prodVFS, build(module, "/", srcTok.string()))

			positions = append(positions, ProducerPos{
				kind:  prodGoCgoCopy,
				index: i,
				outs:  e.prodVFSTake(outsMark),
				ins:   e.srcInsCandidate(srcTok),
			})
		}

		if len(d.cgoSrcs) > 0 {
			outsMark := len(e.prodVFS)

			for _, f := range d.cgoSrcs {
				base := strings.TrimSuffix(f.string(), ".go")

				e.prodVFS = append(e.prodVFS, build(module, "/", base, ".cgo1.go"), build(module, "/", base, ".cgo2.c"))
			}

			e.prodVFS = append(e.prodVFS,
				build(module, "/_cgo_export.h"),
				build(module, "/_cgo_export.c"),
				build(module, "/_cgo_gotypes.go"),
				build(module, "/_cgo_main.c"))

			outs := e.prodVFSTake(outsMark)
			insMark := len(e.prodVFS)

			for _, f := range d.cgoSrcs {
				e.prodVFS = append(e.prodVFS, source(module, "/", f.string()))
			}

			positions = append(positions, ProducerPos{kind: prodGoCgo1, index: 0, outs: outs, ins: e.prodVFSTake(insMark)})
		}
	}

	for i, srcTok := range d.ymapsSprotoSrcs {
		protoRelPath := e.protoSourceRelPath(srcTok.string())
		sprotoMark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, build(strings.TrimSuffix(protoRelPath, ".proto"), ".sproto.h"))

		positions = append(positions, ProducerPos{
			kind:  prodSproto,
			index: i,
			outs:  e.prodVFSTake(sprotoMark),
			ins:   e.srcInsCandidate(srcTok),
		})
	}

	if !isProgramModuleType(d.moduleStmt.Name) {
		for i, stmt := range d.llvmBc {
			outsMark := len(e.prodVFS)

			e.prodVFS = append(e.prodVFS,
				build(module, "/", stmt.Name, "_optimized", stmt.Suffix, ".bc"),
				build(module, "/", stmt.Name, "_merged", stmt.Suffix, ".bc"))

			outs := e.prodVFSTake(outsMark)
			insMark := len(e.prodVFS)

			for _, src := range stmt.Sources {
				if v := runInputBuildCandidate(module, src); v != 0 {
					e.prodVFS = append(e.prodVFS, v)
				}
			}

			positions = append(positions, ProducerPos{
				kind:  prodLlvmBc,
				index: i,
				outs:  outs,
				ins:   e.prodVFSTake(insMark),
			})
		}
	}

	for i := range d.enumSrcs {
		stmt := d.enumSrcs[i]
		enDir, enSep, enBase := e.enumSerializedBaseParts(stmt)
		outsMark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, build(enDir, enSep, enBase, "_serialized.cpp"))

		if stmt.Variant == "with_header" {
			e.prodVFS = append(e.prodVFS, build(enDir, enSep, enBase, "_serialized.h"))
		}

		outs := e.prodVFSTake(outsMark)
		insMark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, build(e.enumHeaderSourceInput(stmt.Header, d.srcDirs).relString()))

		positions = append(positions, ProducerPos{
			kind:  prodEnum,
			index: i,
			outs:  outs,
			ins:   e.prodVFSTake(insMark),
		})
	}

	if d.lj21 != nil {
		outsMark := len(e.prodVFS)

		for _, lua := range d.lj21.Luas {
			e.prodVFS = append(e.prodVFS, build(module, "/", strings.TrimSuffix(lua, ".lua"), ".raw"))
		}

		positions = append(positions, ProducerPos{kind: prodLj21, outs: e.prodVFSTake(outsMark)})
	}

	for i, a := range d.archives {
		outsMark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, build(module, "/", a.Name))

		outs := e.prodVFSTake(outsMark)
		insMark := len(e.prodVFS)

		for _, f := range a.Files {
			e.prodVFS = append(e.prodVFS, copyFileOutputVFS(module, f))
		}

		positions = append(positions, ProducerPos{
			kind:  prodArchive,
			index: i,
			outs:  outs,
			ins:   e.prodVFSTake(insMark),
		})
	}

	for i, conf := range d.checkConfigHeaders {
		outsMark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, checkConfigHGeneratedVFS(module, conf))

		positions = append(positions, ProducerPos{
			kind:  prodCheckConfigH,
			index: i,
			outs:  e.prodVFSTake(outsMark),
		})
	}

	if hasCython {
		positions = append(positions, ProducerPos{kind: prodCythonAll})
	}

	if len(d.swigC) > 0 {
		positions = append(positions, ProducerPos{kind: prodSwigAll})
	}

	for i, js := range d.joinSrcs {
		outsMark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, build(module, "/", js.OutputName))

		outs := e.prodVFSTake(outsMark)
		insMark := len(e.prodVFS)

		for _, src := range js.Sources {
			if v := runInputBuildCandidate(module, src.string()); v != 0 {
				e.prodVFS = append(e.prodVFS, v)
			}
		}

		positions = append(positions, ProducerPos{
			kind:  prodJoinSrcs,
			index: i,
			outs:  outs,
			ins:   e.prodVFSTake(insMark),
		})
	}

	return positions, srcs
}

func scheduleProducers(dst []int, m *IdValueMap, positions []ProducerPos, modulePath string) []int {
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

	order := dst[:0]

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

	order := scheduleProducers(e.prodOrder[:0], &e.ctx.prodOuts, positions, e.instance.Path.relString())

	e.prodOrder = order[:0]

	for _, pi := range order {
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
