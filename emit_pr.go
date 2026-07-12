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
	inSources     []VFS
	inBuilds      []VFS
	outVFSByToken map[ANY]VFS
	stdoutVFS     *VFS
	closureSource []VFS
	closureBuild  []VFS
}

func (e *EmitContext) emitRunProgramStmt(rp *RunProgramStmt) {
	e.emitRunProgram(rp)

	outs := make([]string, 0, len(rp.OUTFiles)+1)

	outs = append(outs, anyStrs(rp.OUTFiles)...)

	if rp.StdoutFile != nil && !rp.StdoutNoAuto {
		outs = append(outs, rp.StdoutFile.string())
	}

	for _, out := range outs {
		if !generatedOutputAutoCompiles(out) {
			continue
		}

		e.enqueueSrc(SrcMeta{
			Source: copyFileOutputVFS(e.instance.Path.relString(), out).any(),
			Prio:   stmtPrioDefault,
			Seq:    rp.DeclSeq,
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
	auxTools := resolveRunProgramAuxTools(ctx, anyStrs(stmt.ToolPaths))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))
	inSources := make([]VFS, 0, len(stmt.INFiles))
	inBuilds := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		v := e.runProgramInputVFS(f.string())

		inVFSs = append(inVFSs, v)

		if v.isBuild() {
			inBuilds = append(inBuilds, v)
		} else {
			inSources = append(inSources, v)
		}
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

	prSourceInputs := prCollectSourceInputs(ctx.na, e.codegen, inVFSs)
	protoImportPbH := prProtoImportPbH(ctx.parsers, inVFSs, e.dirScratch[:0])

	e.dirScratch = protoImportPbH

	prRef := ctx.emit.reserve()
	registeredPROut := map[VFS]bool{}
	mainIsHeader := mainOutputVFS != 0 && isHeaderSource(mainOutputVFS.relString())

	mainHeaderInclude := func(ccOutRel string) (IncludeDirective, bool) {
		if !mainIsHeader || relStem(ccOutRel) != relStem(mainOutputVFS.relString()) {
			return IncludeDirective{}, false
		}

		return IncludeDirective{kind: includeQuoted, target: includeTarget(mainOutputVFS.rel().any())}, true
	}

	snap := &prSnap{
		ctx:       ctx,
		instance:  instance,
		scanner:   e.scanner,
		codegen:   e.codegen,
		scanCtx:   e.d.scanCtx,
		inVFSs:    ctx.na.vfsList(inVFSs...),
		inSources: ctx.na.vfsList(inSources...),
		inBuilds:  ctx.na.vfsList(inBuilds...),
	}

	pe := func() {
		inputClosure := prInputClosure(snap, stmt)

		if len(inputClosure.sources) > 0 {
			for out := range registeredPROut {
				snap.codegen.addSourceInputs(ctx.na, out, inputClosure.sources)
			}
		}

		e.emitPR(RunProgramNodeSpec{
			stmt:          stmt,
			toolBinPath:   *res.LDPath,
			toolLDRef:     res.LDRef,
			auxTools:      auxTools,
			inVFSs:        snap.inVFSs,
			inSources:     snap.inSources,
			inBuilds:      snap.inBuilds,
			outVFSByToken: outVFSByToken,
			stdoutVFS:     stdoutVFS,
			closureSource: inputClosure.sources,
			closureBuild:  inputClosure.builds,
		}, prRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	registerOutput := func(out VFS, parsed ParsedIncludeSet, ridesHeaderViaParsed bool) {
		if registeredPROut[out] {
			return
		}

		registeredPROut[out] = true

		leaves := prSourceInputs.generated

		if out != mainOutputVFS && !ridesHeaderViaParsed {
			lv := ctx.na.vfs.alloc(1 + len(prSourceInputs.generated))

			lv[0] = mainOutputVFS

			ln := 1 + copy(lv[1:], prSourceInputs.generated)

			ctx.na.vfs.commit(ln)
			leaves = lv[:ln:ln]
		}

		e.register(GeneratedFileInfo{
			OutputPath:     out,
			ProducerRef:    prRef,
			GeneratorRefs:  e.ctx.na.refList(res.LDRef),
			ParsedIncludes: parsed,
			SourceInputs:   prSourceInputs.all,
			ClosureLeaves:  leaves,
			OnUse:          pending,
		})
	}

	parsedFor := func(f ANY, out VFS, auto bool) (ParsedIncludeSet, bool) {
		parsed := prOutputParsedIncludes(ctx.na, f, stmt, inVFSs, protoImportPbH)

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
}

type PrSourceInputSet struct {
	all       []VFS
	generated []VFS
}

func prCollectSourceInputs(na *NodeArenas, reg *CodegenRegistry, inVFSs []VFS) PrSourceInputSet {
	var direct []VFS
	var generated []VFS

	for _, v := range inVFSs {
		if v.isSource() {
			direct = append(direct, v)

			continue
		}

		if info := reg.use(v); info != nil {
			generated = append(generated, info.SourceInputs...)
		}
	}

	all := na.vfs.alloc(len(direct) + len(generated))
	an := copy(all, direct)

	an += copy(all[an:], generated)
	na.vfs.commit(an)

	return PrSourceInputSet{all: all[:an:an], generated: generated}
}

func prProtoImportPbH(pm *IncludeParserManager, inVFSs []VFS, dst []IncludeDirective) []IncludeDirective {
	for _, v := range inVFSs {
		if v.isSource() && extIsProto(v.relString()) {
			dst = protoDirectPbHIncludes(pm, v.relString(), "", dst)
		}
	}

	return dst
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

type prSnap struct {
	ctx       *GenCtx
	instance  ModuleInstance
	scanner   *IncludeScanner
	codegen   *CodegenRegistry
	scanCtx   *ScanContext
	inVFSs    []VFS
	inSources []VFS
	inBuilds  []VFS
}

type PrInputClosure struct {
	sources []VFS
	builds  []VFS
}

func prInputClosure(s *prSnap, stmt *RunProgramStmt) PrInputClosure {
	ctx, instance := s.ctx, s.instance
	hasAutoCCSourceOut := stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string())
	generatesHeader := stmt.StdoutFile != nil && isHeaderSource(stmt.StdoutFile.string())

	for _, f := range stmt.OUTFiles {
		hasAutoCCSourceOut = hasAutoCCSourceOut || isCCSourceExt(f.string())
		generatesHeader = generatesHeader || isHeaderSource(f.string())
	}

	mainRel := prMainOutputRel(stmt)
	fullSourceClosure := len(stmt.INFiles) == 0 && (!hasAutoCCSourceOut || isCCSourceExt(mainRel))

	if len(stmt.INFiles) == 0 && !fullSourceClosure {
		return PrInputClosure{}
	}

	hasProtoIN := false
	hasParsedIN := false

	for _, f := range stmt.INFiles {
		hasProtoIN = hasProtoIN || extIsProto(f.string())
		hasParsedIN = hasParsedIN || ctx.parsers.registry.hasRegisteredParser(f.string())
	}

	sourceScratch := vfsScratches.get()
	buildScratch := vfsScratches.get()

	defer func() {
		vfsScratches.put(sourceScratch)
		vfsScratches.put(buildScratch)
	}()

	sources := sourceScratch
	builds := buildScratch
	appendInput := func(v VFS) {
		if v.isBuild() {
			builds = append(builds, v)
		} else {
			sources = append(sources, v)
		}
	}

	ridesMainHeader := func(ccRel string) bool {
		return isHeaderSource(mainRel) && relStem(ccRel) == relStem(mainRel)
	}

	if len(stmt.INFiles) > 0 && (hasParsedIN || !generatesHeader) {
		scanGeneratedCC := func(rel string) {
			if !isCCSourceExt(rel) || ridesMainHeader(rel) {
				return
			}

			cv := s.scanner.walkClosure(copyFileOutputVFS(instance.Path.relString(), rel), s.scanCtx, scanDomainCC)

			eachBucketVFS(cv.bucketList(), appendInput)
		}

		for _, f := range stmt.OUTFiles {
			scanGeneratedCC(f.string())
		}

		if stmt.StdoutFile != nil {
			scanGeneratedCC(stmt.StdoutFile.string())
		}
	}

	for i, f := range stmt.INFiles {
		rel := f.string()

		if ctx.parsers.registry.hasRegisteredParser(rel) {
			s.scanner.walkClosure(s.inVFSs[i], s.scanCtx, scanDomainCC).each(appendInput)

			continue
		}

		if info := s.codegen.use(s.inVFSs[i]); info != nil {
			sources = append(sources, info.SourceInputs...)
		}
	}

	if fullSourceClosure {
		for _, f := range stmt.OUTFiles {
			if !isHeaderSource(f.string()) {
				continue
			}

			cv := s.scanner.walkClosure(copyFileOutputVFS(instance.Path.relString(), f.string()), s.scanCtx, scanDomainCC)

			eachBucketVFS(cv.bucketList(), func(v VFS) {
				if v.isSource() {
					sources = append(sources, v)
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

	pbhSeen := pbhBasenameSet(sources)

	for name := range pbhBasenameSet(builds) {
		pbhSeen[name] = true
	}

	for _, oi := range stmt.OutputIncludes {
		target := oi.relOrSelf()

		var sub Closure

		selfIsInput := false

		switch info := s.codegen.lookup(target.build()); {
		case info != nil:
			sub = s.scanner.walkClosure(info.OutputPath, s.scanCtx, scanDomainCC)
		case fullSourceClosure && ctx.fs.isFile(srcRootRel, target.string()):
			sub = s.scanner.walkClosure(target.source(), s.scanCtx, scanDomainCC)
			selfIsInput = true
		default:
			continue
		}

		process := func(v VFS) {
			if !keep(v) {
				return
			}

			appendInput(v)

			if extIsPbH(v.relString()) {
				pbhSeen[filepath.Base(v.relString())] = true
			}

			if !fullSourceClosure && !hasProtoIN && v.isSource() && extIsProto(v.relString()) {
				sibling := strings.TrimSuffix(v.relString(), ".proto") + ".pb.h"
				sibDir, sibBase := splitDirName(sibling)

				if ctx.fs.isFile(dirKey(sibDir), sibBase) && !pbhSeen[sibBase] {
					sources = append(sources, source(sibling))
					pbhSeen[sibBase] = true
				}
			}
		}

		if selfIsInput {
			process(sub.self)
		}

		eachBucketVFS(sub.bucketList(), process)
	}

	sourceScratch = sources
	buildScratch = builds

	if len(sources) == 0 && len(builds) == 0 {
		return PrInputClosure{}
	}

	return PrInputClosure{
		sources: ctx.na.dedupClosure(sources),
		builds:  ctx.na.dedupClosure(builds),
	}
}

func prOutputParsedIncludes(na *NodeArenas, outFile ANY, stmt *RunProgramStmt, inVFSs []VFS, protoImportPbH []IncludeDirective) ParsedIncludeSet {
	carries := generatedOutputCarriesIncludes(outFile.string())

	if !carries && len(stmt.OutputIncludes) == 0 {
		return ParsedIncludeSet{}
	}

	local := na.dirs.alloc(len(stmt.OutputIncludes))[:0]

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = v.rel().any()
		}

		local = append(local, IncludeDirective{kind: includeQuoted, target: includeTarget(f)})
	}

	na.dirs.commit(len(local))
	local = local[:len(local):len(local)]

	carryProtoImportPbH := isHeaderSource(outFile.string()) && !extIsPbH(outFile.string())
	n := 0

	if carries {
		n += len(inVFSs)
	}

	if carryProtoImportPbH {
		n += len(protoImportPbH)
	}

	compile := na.dirs.alloc(n)[:0]

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

	na.dirs.commit(len(compile))

	return ParsedIncludeSet{parsedIncludesLocal: local, parsedIncludesCpp: compile[:len(compile):len(compile)]}
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

func (e *EmitContext) emitPR(spec RunProgramNodeSpec, id NodeRef) {
	instance := e.instance
	stmt := spec.stmt
	na := e.ctx.na
	env := envVarsVCS

	if len(stmt.EnvPairs) > 0 {
		block := na.envs.alloc(1 + len(stmt.EnvPairs))[:0]

		block = append(block, envVarsVCS...)

		for _, kv := range stmt.EnvPairs {
			parts := strings.SplitN(kv.string(), "=", 2)

			if len(parts) == 2 {
				block = append(block, EnvVar{Name: internEnv(parts[0]), Value: internStr(parts[1]).any()})
			} else {
				block = append(block, EnvVar{Name: internEnv(kv.string()), Value: strEmpty.any()})
			}
		}

		na.envs.commit(len(block))

		env = EnvVars(block[:len(block):len(block)])
	}

	cmdArgs := na.anys.alloc(1 + len(stmt.Args))[:0]

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

	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	var inputs InputChunks

	dedupers.with(func(deduper *DeDuper) {
		tools := na.vfs.alloc(1 + len(spec.auxTools))[:0]

		appendUnique := func(p VFS) {
			if !deduper.add(p.strID()) {
				return
			}

			tools = append(tools, p)
		}

		appendUnique(spec.toolBinPath)

		for _, tool := range spec.auxTools {
			appendUnique(tool.bin)
		}

		na.vfs.commit(len(tools))

		tools = tools[:len(tools):len(tools)]
		sources := na.filterSeen(deduper, spec.inSources)
		builds := na.filterSeen(deduper, spec.inBuilds)
		closureSources := na.filterSeen(deduper, spec.closureSource)
		closureBuilds := na.filterSeen(deduper, spec.closureBuild)
		inputs = na.inputList(tools, sources, builds, closureSources, closureBuilds)
	})
	outputs := na.vfs.alloc(1 + len(stmt.OUTFiles) + len(stmt.OUTNoAutoFiles))[:0]

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

	na.vfs.commit(len(outputs))

	outputs = outputs[:len(outputs):len(outputs)]

	toolRefs := na.noderefs.alloc(len(spec.auxTools) + 1)[:0]

	for _, tool := range spec.auxTools {
		if tool.ref != 0 {
			toolRefs = append(toolRefs, tool.ref)
		}
	}

	if spec.toolLDRef != 0 {
		toolRefs = append(toolRefs, spec.toolLDRef)
	}

	na.noderefs.commit(len(toolRefs))

	toolRefs = toolRefs[:len(toolRefs):len(toolRefs)]

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
		ForeignDepRefs: toolRefs,
	}

	e.emitReservedNode(node, id)
}

type PrFileToken struct {
	token  string
	rooted string
	vfs    VFS
}

func prBareFileTokens(stmt *RunProgramStmt, inVFSs []VFS, outVFSByToken map[ANY]VFS) []PrFileToken {
	toks := make([]PrFileToken, 0, len(stmt.INFiles)+len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

	add := func(tok ANY, vfs VFS) {
		if tok.vfs() != 0 {
			return
		}

		t := tok.string()

		toks = append(toks, PrFileToken{token: t, rooted: vfs.string(), vfs: vfs})
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

func rootBareFileArg(arg string, toks []PrFileToken) (string, VFS, bool) {
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
