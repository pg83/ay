package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	prodCuda = iota
	prodImplib
	prodCopyFile
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
	prodDocsCopy
	prodGoCodegen
	prodLlvmCompile
	prodLuaCompile
	prodUserverProto
	prodListProto
	prodFromArchive
	prodPchFile
	prodProto2Fbs
	prodPireInline
)

type ProducerPos struct {
	kind  int
	index int
	outs  []VFS
	ins   []VFS
}

func runInputBuildCandidate(modulePath, rel string) VFS {
	v := moduleInputCandidateVFS(modulePath, rel)

	if v.isBuild() {
		return v
	}

	return 0
}

func rootedBuildCandidate(modulePath, rel string) VFS {
	if v, ok := moduleRootedVFS(modulePath, rel); ok && v.isBuild() {
		return v
	}

	return 0
}

// Plan source-resolved inputs before local producers have been registered.
// A physical source (or an explicit root) keeps its namespace. Missing names
// retain a build candidate for declaration ordering; emission still selects
// generated files only from CodegenRegistry's registered producer facts.
func (e *EmitContext) sourceInputForScheduling(name string) VFS {
	input := resolveSourceVFS(e.ctx, e.instance, name, e.d.srcDirs)

	if input.isSource() && !e.ctx.fs.isFile(srcRootRel, input.relString()) {
		return moduleInputCandidateVFS(e.instance.Path.relString(), name)
	}

	return input
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

func (e *EmitContext) srcPosition(meta SrcMeta) ([]VFS, []VFS) {
	if srcExtClassOf(meta.Source) == srcExtGettext {
		input := resolveSourceVFS(e.ctx, e.instance, meta.Source.string(), e.d.srcDirs)
		mark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, e.gettextOutput(input))

		outputs := e.prodVFSTake(mark)

		mark = len(e.prodVFS)

		if meta.Source.vfs() == 0 && input.isSource() && !e.ctx.fs.isFilePathClean(input.relString()) {
			e.prodVFS = append(e.prodVFS, input.rel().build())
		}

		e.prodVFS = append(e.prodVFS, input)

		return outputs, e.prodVFSTake(mark)
	}

	d := e.d
	tok := meta.Source.str()
	src := tok.string()
	class := srcExtClassOf(tok.any())

	if extIsProto(meta.Source.string()) {
		src = meta.Source.string()
		class = srcExtProto
	}

	outsMark := len(e.prodVFS)

	var protoRelID STR
	var protoInput VFS

	protoRel := ""

	switch class {
	case srcExtProto:
		protoInput = resolveSourceVFS(e.ctx, e.instance, src, d.srcDirs)
		protoRelID = e.protoSourceRel(protoInput.string())
		protoRel = protoRelID.string()
	case srcExtEv, srcExtCfgProto:
		protoRelID = e.protoSourceRel(src)
		protoRel = protoRelID.string()
	}

	switch class {
	case srcExtRos:
		if d.ros != nil {
			e.prodVFS = append(e.prodVFS, e.rosOutputPaths(src, meta.RosZeroCopy)...)
		}
	case srcExtProto:
		base := strings.TrimSuffix(protoRel, ".proto")

		if isPythonProtoModule(d) {
			for _, suffix := range pyProtoSuffixes(d) {
				e.prodVFS = append(e.prodVFS, build(base, "__int", protoPythonVersion(d), "__", suffix))
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
		if !isPythonProtoModule(d) {
			e.prodVFS = append(e.prodVFS, e.gztGenProto(src))
		}
	}

	outs := e.prodVFSTake(outsMark)
	module := e.instance.Path.relString()
	insMark := len(e.prodVFS)

	if class == srcExtProto {
		if _, rooted := moduleRootedVFS(module, src); !rooted && protoInput.isSource() && !e.ctx.fs.isFile(srcRootRel, protoInput.relString()) {
			protoInput = protoInput.rel().build()
		}

		e.prodVFS = append(e.prodVFS, protoInput)
	} else if v := runInputBuildCandidate(module, src); v != 0 {
		e.prodVFS = append(e.prodVFS, v)
	}

	switch class {
	case srcExtProto, srcExtEv:
		var directives []IncludeDirective

		if class != srcExtProto || protoInput.isSource() {
			directives = e.ctx.parsers.sourceParsedBuckets(protoRelID.source(), nil).bucket(parsedIncludesLocal)
		}

		if len(directives) > 0 {
			outputRoot := protoCPPOutRoot(e.d)
			outputRootClean := outputRoot != "" && pathIsClean(outputRoot)

			for _, directive := range directives {
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

	n := len(d.cudaSrcs) + len(d.implibs) + len(d.copyFiles) + len(d.configureFiles) + len(d.antlrRuns) + len(d.antlr4Grammars) +
		len(d.decimalMD5) + len(d.buildMns) + len(d.splitCodegens) + len(d.baseCodegens) + len(d.runPrograms) + len(d.runPython) +
		len(d.archiveAsm) + len(d.srcs) + len(d.ymapsSprotoSrcs) + len(d.llvmBc) + len(d.enumSrcs) +
		len(d.archives) + len(d.checkConfigHeaders) + len(d.joinSrcs) + len(d.goCodegens) + len(d.cgoSrcs) +
		len(d.llvmCompile) + len(d.luaCompile) + len(d.userverProtos) + len(d.listProtos) + len(d.fromArchives) + len(d.pchFiles) + len(d.proto2Fbs) + len(d.pireInline)

	if d.docs != nil {
		n += len(d.docs.copies)
	}

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

	if d.docs != nil {
		for i, entry := range d.docs.copies {
			start := len(backing)
			dst := buildJoinClean(module, entry.namespace)

			for _, file := range entry.files {
				backing = append(backing, buildJoinClean(dst.relString(), file))
			}

			end := len(backing)

			for _, file := range entry.files {
				backing = append(backing, docsSource(strings.TrimRight(entry.from, "/")+"/"+file))
			}

			push(prodDocsCopy, i, start, end)
		}
	}

	for i, stmt := range d.cudaSrcs {
		start := len(backing)

		for _, src := range stmt.Sources {
			backing = append(backing, build(module, "/", cudaSourceBase(src.string()), ".cudafe1.cpp"))
		}

		outEnd := len(backing)

		for _, src := range stmt.Sources {
			if v := runInputBuildCandidate(module, src.string()); v != 0 {
				backing = append(backing, v)
			}
		}

		push(prodCuda, i, start, outEnd)
	}

	for i, entry := range d.implibs {
		start := len(backing)

		backing = append(backing, implibOutputs(module, entry.Path)...)

		outEnd := len(backing)

		if entry.FromBundle {
			if v := runInputBuildCandidate(module, entry.Path); v != 0 {
				backing = append(backing, v)
			}
		}

		push(prodImplib, i, start, outEnd)
	}

	for i, entry := range d.copyFiles {
		start := len(backing)

		backing = append(backing, copyFileOutputVFS(module, entry.Dst))

		outEnd := len(backing)

		backing = append(backing, e.sourceInputForScheduling(entry.Src))

		push(prodCopyFile, i, start, outEnd)
	}

	for i, entry := range d.fromArchives {
		start := len(backing)

		runOuts(entry.OUTFiles, entry.OUTNoAutoFiles, nil)

		outEnd := len(backing)

		if v := runInputBuildCandidate(module, entry.Source.string()); v != 0 {
			backing = append(backing, v)
		}

		push(prodFromArchive, i, start, outEnd)
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
			backing = append(backing, copyFileOutputVFS(module, prefix+"."+strconv.Itoa(part)+".cpp"))
		}

		backing = append(backing, copyFileOutputVFS(module, filepath.Base(prefix)+".cpp"), copyFileOutputVFS(module, filepath.Base(prefix)+".h"))
		outEnd := len(backing)
		backing = append(backing, e.sourceInputForScheduling(prefix+".in"))
		push(prodSplitCodegen, i, start, outEnd)
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

			if !vfsHasPrefix(f.string()) {
				backing = append(backing, buildClean(f.string()))
			}
		}
	}

	for i, lp := range d.listProtos {
		start := len(backing)
		out := copyFileOutputVFS(module, lp.Output.string())

		backing = append(backing, out, listProtoHeader(out))

		outEnd := len(backing)

		runIns(lp.Files)
		push(prodListProto, i, start, outEnd)
	}

	for i, stmt := range d.proto2Fbs {
		output := copyFileOutputVFS(module, strings.TrimSuffix(stmt.Source.string(), filepath.Ext(stmt.Source.string()))+".fbs").rel().build()

		positions = append(positions, ProducerPos{kind: prodProto2Fbs, index: i, outs: []VFS{output}, ins: e.srcInsCandidate(stmt.Source)})
	}

	for i, token := range d.pireInline {
		input := e.moduleSourceVFS(token)
		output, _ := composeCCPaths(e.instance, e.moduleSourceRel(token), input, ModuleCCInputs{}, "")
		start := len(backing)

		backing = append(backing, output, input)
		push(prodPireInline, i, start, start+1)
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
		start := len(backing)

		for _, archive := range d.archiveAsm {
			for _, file := range archive.Files {
				backing = append(backing, moduleInputCandidateVFS(module, file))
			}
		}

		push(prodArchiveAsm, 0, start, start)
	}

	srcs := e.prodSrcs[:0]

	defer func() { e.prodSrcs = srcs }()

	for _, meta := range d.srcs {
		src := meta.Source

		if !isCodegenProducingSrcID(src) {
			continue
		}

		if meta.Prio == stmtPrioSrcs {
			meta.SourceQueueRank = 1
		}

		srcs = append(srcs, meta)
	}

	srcCount := len(srcs)

	for i := 0; i < srcCount; i++ {
		meta := srcs[i]
		src := meta.Source

		if srcExtClassOf(src) == srcExtGztProto && !isPythonProtoModule(d) {
			childMeta := meta

			childMeta.Source = e.gztGenProto(src.string()).any()
			if childMeta.SourceQueueRank != 0 {
				childMeta.SourceQueueRank++
			}
			srcs = append(srcs, childMeta)
		}
	}

	for i, st := range d.goCodegens {
		outsMark := len(e.prodVFS)

		for _, name := range st.outputNames() {
			e.prodVFS = append(e.prodVFS, build(module, "/", name))
		}

		positions = append(positions, ProducerPos{kind: prodGoCodegen, index: i, outs: e.prodVFSTake(outsMark)})
	}

	for i, batch := range d.userverProtos {
		start := len(e.prodVFS)

		for _, file := range batch.files {
			e.prodVFS = append(e.prodVFS, batch.outputs(file.path)...)
		}

		outs := e.prodVFSTake(start)

		start = len(e.prodVFS)

		if batch.descriptor != "" {
			e.prodVFS = append(e.prodVFS, copyFileOutputVFS(module, batch.descriptor))
		}

		for _, file := range batch.files {
			e.prodVFS = append(e.prodVFS, build(file.path))
		}

		positions = append(positions, ProducerPos{kind: prodUserverProto, index: i, outs: outs, ins: e.prodVFSTake(start)})
	}

	for i, m := range srcs {
		outs, ins := e.srcPosition(m)

		positions = append(positions, ProducerPos{
			kind:  prodSrc,
			index: i,
			outs:  outs,
			ins:   ins,
		})
	}

	if goModule {
		for i, srcTok := range goModuleCgoExports(d) {
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
				e.prodVFS = append(e.prodVFS, e.goCgoOut(f, ".cgo1.go"), e.goCgoOut(f, ".cgo2.c"))
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

	for i, stmt := range d.pchFiles {
		positions = append(positions, ProducerPos{
			kind: prodPchFile, index: i,
			outs: []VFS{copyFileOutputVFS(module, stmt.Output.string())},
			ins:  e.srcInsCandidate(stmt.Header),
		})
	}

	for i, stmt := range d.llvmCompile {
		outputs := []VFS{copyFileOutputVFS(module, stmt.Output)}

		if stmt.Kind == tokBpfStatic {
			outputs = append(outputs, copyFileOutputVFS(module, stmt.Source+".bpf.o"))
		}

		positions = append(positions, ProducerPos{
			kind: prodLlvmCompile, index: i,
			outs: outputs,
			ins:  e.srcInsCandidate(internStr(stmt.Source).any()),
		})
	}

	for i, stmt := range d.luaCompile {
		positions = append(positions, ProducerPos{
			kind: prodLuaCompile, index: i,
			outs: []VFS{copyFileOutputVFS(module, stmt.Source+".o")},
			ins:  e.srcInsCandidate(internStr(stmt.Source).any()),
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

		if stmt.Variant != "noexcept_header" {
			e.prodVFS = append(e.prodVFS, build(enDir, enSep, enBase, "_serialized.cpp"))
		}

		if stmt.Variant == "with_header" || stmt.Variant == "noexcept_header" {
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

		e.prodVFS = append(e.prodVFS, copyFileOutputVFS(module, a.Name))

		outs := e.prodVFSTake(outsMark)
		insMark := len(e.prodVFS)

		for _, f := range a.Files {
			e.prodVFS = append(e.prodVFS, moduleInputCandidateVFS(module, f))
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
		outsMark := len(e.prodVFS)

		for _, stmt := range d.swigC {
			prefix := swigOutputPrefix(stmt.Src, stmt.Module)

			e.prodVFS = append(e.prodVFS, build(module, "/", prefix, ".swg", cythonOutExt(stmt.CMode)), build(module, "/", prefix, ".py"))
		}

		positions = append(positions, ProducerPos{kind: prodSwigAll, outs: e.prodVFSTake(outsMark)})
	}

	for i, js := range d.joinSrcs {
		outsMark := len(e.prodVFS)

		e.prodVFS = append(e.prodVFS, copyFileOutputVFS(module, js.OutputName))

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
	if e.peers != nil {
		env := Environment{s: &e.frame.ifEnv}

		for _, decl := range e.peers.ResourceGlobals {
			env.setVFS(internEnvSTR(decl.GlobalVar), decl.Value)
		}
	}

	positions, srcs := e.producerPositions(len(cythonPlans) > 0)

	if len(positions) == 0 {
		return
	}

	order := scheduleProducers(e.prodOrder[:0], &e.ctx.prodOuts, positions, e.instance.Path.relString())

	e.prodOrder = order[:0]

	for _, pi := range order {
		pos := positions[pi]

		switch pos.kind {
		case prodPireInline:
			e.emitPireInline(pos.ins[0], pos.outs[0])
		case prodProto2Fbs:
			e.emitProto2Fbs(e.d.proto2Fbs[pos.index], pos.outs[0])
		case prodFromArchive:
			e.emitFromArchive(e.d.fromArchives[pos.index])
		case prodDocsCopy:
			e.emitDocsCopy(e.d.docs.copies[pos.index])
		case prodCuda:
			e.emitCudaSrcs(e.d.cudaSrcs[pos.index])
		case prodImplib:
			e.emitImplib(e.d.implibs[pos.index])
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
			meta := srcs[pos.index]

			if srcExtClassOf(meta.Source) == srcExtRos {
				e.emitRosSource(meta, pos.outs)
			} else if srcExtClassOf(meta.Source) == srcExtGettext {
				input := pos.ins[len(pos.ins)-1]

				if len(pos.ins) > 1 {
					info := e.codegen.lookup(pos.ins[0])

					if info == nil {
						throwFmt("gen: %s: no source or producer for %s", e.instance.Path.relString(), input.string())
					}

					input = info.OutputPath
				}

				e.emitGettextSource(input, pos.outs[0])
			} else {
				e.emitOneSource(meta)
			}
		case prodGoCgoCopy:
			e.emitGoCgoCopyStmt(goModuleCgoExports(e.d)[pos.index])
		case prodGoCgo1:
			e.emitGoCgo1Stmt()
		case prodGoCodegen:
			e.emitGoCodegenStmt(e.d.goCodegens[pos.index])
		case prodListProto:
			e.emitListProto(e.d.listProtos[pos.index])
		case prodUserverProto:
			e.emitUserverProtos(e.d.userverProtos[pos.index])
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
		case prodPchFile:
			e.emitPchFile(e.d.pchFiles[pos.index])

		case prodLlvmCompile:
			e.emitLlvmCompile(e.d.llvmCompile[pos.index])
		case prodLuaCompile:
			e.emitLuaCompile(e.d.luaCompile[pos.index])
		}
	}
}
