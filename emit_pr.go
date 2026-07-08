package main

import (
	"path/filepath"
	"sort"
	"strings"
)

var prKV = KV{P: pkPR, PC: pcYellow, ShowOut: true}

type RunProgramAuxTool struct {
	token  string
	ref    NodeRef
	bin    VFS
	rooted bool
}

type RunProgramNodeSpec struct {
	stmt          *RunProgramStmt
	toolBinPath   VFS
	toolLDRef     NodeRef
	auxTools      []RunProgramAuxTool
	inVFSs        []VFS
	outVFSByToken map[ANY]VFS
	stdoutVFS     *VFS
	inputClosure  []VFS
	extraDepRefs  []NodeRef
}

func (e *EmitContext) emitRunProgramStmt(rp *RunProgramStmt) {
	e.emitRunProgram(rp)

	outs := make([]string, 0, len(rp.OUTFiles)+1)

	outs = append(outs, strStrings(rp.OUTFiles)...)

	if rp.StdoutFile != nil && !rp.StdoutNoAuto {
		outs = append(outs, rp.StdoutFile.string())
	}

	for _, out := range outs {
		if !generatedOutputAutoCompiles(out) {
			continue
		}

		e.enqueueSrc(SrcMeta{
			Source:    copyFileOutputVFS(e.instance.Path.relString(), out).any(),
			Prio:      stmtPrioDefault,
			Seq:       rp.DeclSeq,
			Generated: true,
		})
	}
}

func prMainOutputRel(stmt *RunProgramStmt) string {
	switch {
	case len(stmt.OUTFiles) > 0:
		return stmt.OUTFiles[0].string()
	case len(stmt.OUTNoAutoFiles) > 0:
		return stmt.OUTNoAutoFiles[0].string()
	case stmt.StdoutFile != nil:
		return stmt.StdoutFile.string()
	}

	return ""
}

func (e *EmitContext) emitRunProgram(stmt *RunProgramStmt) {
	ctx, instance := e.ctx, e.instance
	res := ctx.toolResult(internArg(filepath.Clean(stmt.ToolPath.string())))
	auxTools := resolveRunProgramAuxTools(ctx, strStrings(stmt.ToolPaths))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		inVFSs = append(inVFSs, e.runProgramInputVFS(f.string()))
	}

	outVFSByToken := make(map[ANY]VFS, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)

	for _, f := range stmt.OUTFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.relString(), f.string())
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.relString(), f.string())
	}

	var stdoutVFS *VFS

	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path.relString(), stmt.StdoutFile.string())

		stdoutVFS = &vfs
		outVFSByToken[*stmt.StdoutFile] = vfs
	}

	var mainOutputVFS VFS

	switch {
	case len(stmt.OUTFiles) > 0:
		mainOutputVFS = outVFSByToken[stmt.OUTFiles[0]]
	case len(stmt.OUTNoAutoFiles) > 0:
		mainOutputVFS = outVFSByToken[stmt.OUTNoAutoFiles[0]]
	case stdoutVFS != nil:
		mainOutputVFS = *stdoutVFS
	}

	prSourceInputs := prCollectSourceInputs(e.codegen, inVFSs)
	protoImportPbH := prProtoImportPbH(ctx.parsers, inVFSs)
	prRef := ctx.emit.reserve()
	registeredPROut := map[VFS]bool{}
	mainIsHeader := mainOutputVFS != 0 && isHeaderSource(mainOutputVFS.relString())

	mainHeaderInclude := func(ccOutRel string) (IncludeDirective, bool) {
		if !mainIsHeader || relStem(ccOutRel) != relStem(mainOutputVFS.relString()) {
			return IncludeDirective{}, false
		}

		return IncludeDirective{kind: includeQuoted, target: includeTarget(mainOutputVFS.rel().any())}, true
	}

	registerOutput := func(out VFS, parsed ParsedIncludeSet, ridesHeaderViaParsed bool) {
		if registeredPROut[out] {
			return
		}

		registeredPROut[out] = true

		leaves := prSourceInputs.generated

		if out != mainOutputVFS && !ridesHeaderViaParsed {
			leaves = append([]VFS{mainOutputVFS}, prSourceInputs.generated...)
		}

		e.codegen.register(&GeneratedFileInfo{
			OutputPath:     out,
			ProducerRef:    prRef,
			GeneratorRefs:  []NodeRef{res.LDRef},
			ParsedIncludes: parsed,
			SourceInputs:   prSourceInputs.all,
			ClosureLeaves:  leaves,
		})
	}

	parsedFor := func(f ANY, out VFS, auto bool) (ParsedIncludeSet, bool) {
		parsed := prOutputParsedIncludes(f, stmt, inVFSs, protoImportPbH)

		if auto && isCCSourceExt(f.string()) {
			if inc, ok := mainHeaderInclude(out.relString()); ok {
				return appendParsedDirectives(parsed, parsedIncludesCpp, inc), true
			}
		}

		return parsed, false
	}

	for _, f := range stmt.OUTFiles {
		out := outVFSByToken[f]
		parsed, rides := parsedFor(f, out, true)

		registerOutput(out, parsed, rides)
	}

	for _, f := range stmt.OUTNoAutoFiles {
		out := outVFSByToken[f]
		parsed, rides := parsedFor(f, out, false)

		registerOutput(out, parsed, rides)
	}

	if stmt.StdoutFile != nil {
		parsed, rides := parsedFor(*stmt.StdoutFile, *stdoutVFS, !stmt.StdoutNoAuto)

		registerOutput(*stdoutVFS, parsed, rides)
	}

	e.deferPass2(func() {
		inputClosure := e.prInputClosure(stmt)

		if prSourceClosure := filterSourceVFS(inputClosure); len(prSourceClosure) > 0 {
			for out := range registeredPROut {
				e.codegen.addSourceInputs(out, prSourceClosure)
			}
		}

		depInputs := inputClosure

		if len(inVFSs) > 0 {
			depInputs = concat(inVFSs, inputClosure)
		}

		emitPR(instance, RunProgramNodeSpec{
			stmt:          stmt,
			toolBinPath:   *res.LDPath,
			toolLDRef:     res.LDRef,
			auxTools:      auxTools,
			inVFSs:        inVFSs,
			outVFSByToken: outVFSByToken,
			stdoutVFS:     stdoutVFS,
			inputClosure:  inputClosure,
			extraDepRefs:  resolveCodegenDepRefsIncl(ctx, instance, ctx.na, depInputs),
		}, prRef, ctx.emit)
	})
}

type prSourceInputSet struct {
	all       []VFS
	generated []VFS
}

func prCollectSourceInputs(reg *CodegenRegistry, inVFSs []VFS) prSourceInputSet {
	var direct []VFS
	var generated []VFS

	for _, v := range inVFSs {
		if v.isSource() {
			direct = append(direct, v)

			continue
		}

		if info := reg.lookup(v); info != nil {
			generated = append(generated, info.SourceInputs...)
		}
	}

	return prSourceInputSet{all: append(direct, generated...), generated: generated}
}

func prProtoImportPbH(pm *IncludeParserManager, inVFSs []VFS) []IncludeDirective {
	var out []IncludeDirective

	for _, v := range inVFSs {
		if v.isSource() && extIsProto(v.relString()) {
			out = append(out, protoDirectPbHIncludes(pm, v.relString(), "")...)
		}
	}

	return out
}

func pbhBasenameSet(vs []VFS) map[string]bool {
	m := map[string]bool{}

	for _, v := range vs {
		if extIsPbH(v.relString()) {
			m[filepath.Base(v.relString())] = true
		}
	}

	return m
}

func (e *EmitContext) prInputClosure(stmt *RunProgramStmt) []VFS {
	ctx, instance, d := e.ctx, e.instance, e.d
	hasAutoCCSourceOut := stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string())
	generatesHeader := stmt.StdoutFile != nil && isHeaderSource(stmt.StdoutFile.string())

	for _, f := range stmt.OUTFiles {
		hasAutoCCSourceOut = hasAutoCCSourceOut || isCCSourceExt(f.string())
		generatesHeader = generatesHeader || isHeaderSource(f.string())
	}

	mainRel := prMainOutputRel(stmt)
	fullSourceClosure := len(stmt.INFiles) == 0 && (!hasAutoCCSourceOut || isCCSourceExt(mainRel))

	if len(stmt.INFiles) == 0 && !fullSourceClosure {
		return nil
	}

	hasProtoIN := false
	hasParsedIN := false

	for _, f := range stmt.INFiles {
		hasProtoIN = hasProtoIN || extIsProto(f.string())
		hasParsedIN = hasParsedIN || ctx.parsers.registry.hasRegisteredParser(f.string())
	}

	scanCfg := d.cc.ScanCfg
	out := ctx.prClosureScratch[:0]

	ridesMainHeader := func(ccRel string) bool {
		return isHeaderSource(mainRel) && relStem(ccRel) == relStem(mainRel)
	}

	if len(stmt.INFiles) > 0 && (hasParsedIN || !generatesHeader) {
		scanGeneratedCC := func(rel string) {
			if !isCCSourceExt(rel) || ridesMainHeader(rel) {
				return
			}

			cv := walkClosure(e.scanner, copyFileOutputVFS(instance.Path.relString(), rel), scanCfg)

			eachBucketVFS(cv.buckets, func(v VFS) { out = append(out, v) })
		}

		for _, f := range stmt.OUTFiles {
			scanGeneratedCC(f.string())
		}

		if stmt.StdoutFile != nil {
			scanGeneratedCC(stmt.StdoutFile.string())
		}
	}

	for _, f := range stmt.INFiles {
		rel := f.string()

		if ctx.parsers.registry.hasRegisteredParser(rel) {
			walkClosure(e.scanner, e.runProgramInputVFS(rel), scanCfg).each(func(v VFS) { out = append(out, v) })

			continue
		}

		if info := e.codegen.lookup(e.runProgramInputVFS(rel)); info != nil {
			out = append(out, info.SourceInputs...)
		}
	}

	if fullSourceClosure {
		for _, f := range stmt.OUTFiles {
			if !isHeaderSource(f.string()) {
				continue
			}

			cv := walkClosure(e.scanner, copyFileOutputVFS(instance.Path.relString(), f.string()), scanCfg)

			eachBucketVFS(cv.buckets, func(v VFS) {
				if v.isSource() {
					out = append(out, v)
				}
			})
		}
	}

	keep := func(v VFS) bool {
		if fullSourceClosure {
			return v.isSource()
		}

		return extIsProto(v.relString())
	}

	pbhSeen := pbhBasenameSet(out)

	for _, oi := range stmt.OutputIncludes {
		target := oi.relOrSelf()

		var sub Closure

		selfIsInput := false

		switch info := e.codegen.lookup(target.build()); {
		case info != nil:
			sub = walkClosure(e.scanner, info.OutputPath, scanCfg)
		case fullSourceClosure && ctx.fs.isFile(srcRootRel, target.string()):
			sub = walkClosure(e.scanner, target.source(), scanCfg)
			selfIsInput = true
		default:
			continue
		}

		process := func(v VFS) {
			if !keep(v) {
				return
			}

			out = append(out, v)

			if extIsPbH(v.relString()) {
				pbhSeen[filepath.Base(v.relString())] = true
			}

			if !fullSourceClosure && !hasProtoIN && v.isSource() && extIsProto(v.relString()) {
				sibling := strings.TrimSuffix(v.relString(), ".proto") + ".pb.h"
				sibDir, sibBase := splitDirName(sibling)

				if ctx.fs.isFile(dirKey(sibDir), sibBase) && !pbhSeen[sibBase] {
					out = append(out, source(sibling))
					pbhSeen[sibBase] = true
				}
			}
		}

		if selfIsInput {
			process(sub.self)
		}

		eachBucketVFS(sub.buckets, process)
	}

	if len(out) == 0 {
		ctx.prClosureScratch = out

		return nil
	}

	res := dedup(out, nil)

	ctx.prClosureScratch = out

	return res
}

func prOutputParsedIncludes(outFile ANY, stmt *RunProgramStmt, inVFSs []VFS, protoImportPbH []IncludeDirective) ParsedIncludeSet {
	carries := generatedOutputCarriesIncludes(outFile.string())

	if !carries && len(stmt.OutputIncludes) == 0 {
		return ParsedIncludeSet{}
	}

	local := make([]IncludeDirective, 0, len(stmt.OutputIncludes))

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = v.rel().any()
		}

		local = append(local, IncludeDirective{kind: includeQuoted, target: includeTarget(f)})
	}

	carryProtoImportPbH := isHeaderSource(outFile.string()) && !extIsPbH(outFile.string())
	n := 0

	if carries {
		n += len(inVFSs)
	}

	if carryProtoImportPbH {
		n += len(protoImportPbH)
	}

	compile := make([]IncludeDirective, 0, n)

	if carries {
		for _, v := range inVFSs {
			if v.isBuild() {
				continue
			}

			compile = append(compile, IncludeDirective{kind: includeQuoted, target: includeTarget(v.rel().any())})
		}
	}

	if carryProtoImportPbH {
		compile = append(compile, protoImportPbH...)
	}

	return ParsedIncludeSet{parsedIncludesLocal: local, parsedIncludesCpp: compile}
}

func resolveRunProgramAuxTools(ctx *GenCtx, toolPaths []string) []RunProgramAuxTool {
	if len(toolPaths) == 0 {
		return nil
	}

	out := make([]RunProgramAuxTool, 0, len(toolPaths))
	seen := make(map[string]struct{}, len(toolPaths))

	for _, toolPath := range toolPaths {
		if _, dup := seen[toolPath]; dup {
			continue
		}

		seen[toolPath] = struct{}{}

		rooted := vfsHasPrefix(toolPath)
		modulePath := toolPath

		if rooted {
			modulePath = intern(toolPath).relString()
		}

		res := ctx.toolResult(internArg(filepath.Clean(modulePath)))

		out = append(out, RunProgramAuxTool{
			token:  toolPath,
			ref:    res.LDRef,
			bin:    *res.LDPath,
			rooted: rooted,
		})
	}

	return out
}

func (e *EmitContext) runProgramInputVFS(rel string) VFS {
	ctx, instance, d := e.ctx, e.instance, e.d

	if vfsHasPrefix(rel) {
		return e.requireProducedInput("IN", rel, copyFileInputVFS(ctx.fs, instance.Path, rel))
	}

	buildVFS := buildJoinClean(instance.Path.relString(), rel)

	if e.codegen.lookup(buildVFS) != nil {
		return buildVFS
	}

	if ctx.fs.isFile(srcRootRel, rel) {
		return source(rel)
	}

	return e.resolveModuleSourceVFS(internStr(rel).any(), d.srcDirs)
}

func emitPR(instance ModuleInstance, spec RunProgramNodeSpec, id NodeRef, emit *StreamingEmitter) {
	stmt := spec.stmt
	na := emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}

	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv.string(), "=", 2)

		if len(parts) == 2 {
			env = append(env, EnvVar{Name: internEnv(parts[0]), Value: internStr(parts[1]).any()})
		} else {
			env = append(env, EnvVar{Name: internEnv(kv.string()), Value: strEmpty.any()})
		}
	}

	cmdArgs := make([]ANY, 0, 1+len(stmt.Args))

	cmdArgs = append(cmdArgs, spec.toolBinPath.any())

	fileTokens := prBareFileTokens(stmt, spec.inVFSs, spec.outVFSByToken)

	for _, aTok := range stmt.Args {
		a := aTok.string()
		key := aTok
		toolReplaced := false

		for _, tool := range spec.auxTools {
			if tool.rooted {
				continue
			}

			if strings.Contains(a, tool.token) {
				a = strings.ReplaceAll(a, tool.token, tool.bin.string())
				key = internStr(a).any()
				toolReplaced = true
			}
		}

		if !toolReplaced {
			if rooted, vfs, ok := rootBareFileArg(a, fileTokens); ok {
				if vfs != 0 {
					cmdArgs = append(cmdArgs, vfs.any())

					continue
				}

				key = internStr(rooted).any()
			}
		}

		cmdArgs = append(cmdArgs, key)
	}

	head := make([]VFS, 0, 1+len(spec.auxTools)+len(stmt.INFiles))

	deduper.reset()

	appendUnique := func(p VFS) {
		if !deduper.add(p.strID()) {
			return
		}

		head = append(head, p)
	}

	appendUnique(spec.toolBinPath)

	for _, tool := range spec.auxTools {
		appendUnique(tool.bin)
	}

	for _, v := range spec.inVFSs {
		appendUnique(v)
	}

	inputs := na.inputList(head, deduper.filterSeen(spec.inputClosure))

	var outputs []VFS
	var stdoutPath VFS

	emittedOut := map[VFS]bool{}

	appendOutput := func(v VFS) {
		if emittedOut[v] {
			return
		}

		emittedOut[v] = true
		outputs = append(outputs, v)
	}

	if spec.stdoutVFS != nil {
		stdoutPath = *spec.stdoutVFS
		appendOutput(*spec.stdoutVFS)
	}

	for _, f := range stmt.OUTFiles {
		appendOutput(spec.outVFSByToken[f])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		appendOutput(spec.outVFSByToken[f])
	}

	var toolRefs []NodeRef

	for _, tool := range spec.auxTools {
		toolRefs = append(toolRefs, depRefs(tool.ref)...)
	}

	toolRefs = append(toolRefs, depRefs(spec.toolLDRef)...)

	cmd := Cmd{
		CmdArgs: na.chunkList(cmdArgs),
		Env:     env,
	}

	if stdoutPath != 0 {
		cmd.Stdout = stdoutPath
	}

	if stmt.CWD != nil {
		cmd.Cwd = cwdVFS((*stmt.CWD).string())
	}

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd),
		Env:            env,
		Inputs:         inputs,
		Outputs:        outputs,
		KV:             &prKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        append([]NodeRef(nil), spec.extraDepRefs...),
		ForeignDepRefs: toolRefs,
	}

	emit.emitReservedNode(node, id)
}

type prFileToken struct {
	token  string
	rooted string
	vfs    VFS
}

func prBareFileTokens(stmt *RunProgramStmt, inVFSs []VFS, outVFSByToken map[ANY]VFS) []prFileToken {
	toks := make([]prFileToken, 0, len(stmt.INFiles)+len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

	add := func(tok ANY, vfs VFS) {
		if tok.vfs() != 0 {
			return
		}

		t := tok.string()

		toks = append(toks, prFileToken{token: t, rooted: vfs.string(), vfs: vfs})
	}

	for i, f := range stmt.INFiles {
		add(f, inVFSs[i])
	}

	for _, f := range stmt.OUTFiles {
		add(f, outVFSByToken[f])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		add(f, outVFSByToken[f])
	}

	sort.SliceStable(toks, func(i, j int) bool {
		return len(toks[i].token) > len(toks[j].token)
	})

	return toks
}

func rootBareFileArg(arg string, toks []prFileToken) (string, VFS, bool) {
	for _, c := range toks {
		idx := strings.Index(arg, c.token)

		if idx < 0 {
			continue
		}

		end := idx + len(c.token)
		beforeOK := idx == 0 || isBareTokenBoundary(arg[idx-1])
		afterOK := end == len(arg) || isBareTokenBoundary(arg[end])

		if beforeOK && afterOK {
			if idx == 0 && end == len(arg) {
				return "", c.vfs, true
			}

			return arg[:idx] + c.rooted + arg[end:], 0, true
		}
	}

	return arg, 0, false
}

func isBareTokenBoundary(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return false
	case c == '.', c == '_', c == '-', c == '"', c == '/':
		return false
	}

	return true
}
