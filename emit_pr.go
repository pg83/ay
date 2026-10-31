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

func (spec *RunProgramNodeSpec) isOutput(v VFS) bool {
	for _, out := range spec.outVFSByToken {
		if out == v {
			return true
		}
	}

	return false
}

func (e *EmitContext) emitRunProgramStmt(rp *RunProgramStmt) {
	e.emitRunProgram(rp)
	e.collectRunProgramObjects(rp)

	outs := make([]string, 0, len(rp.OUTFiles)+1)

	outs = append(outs, anyStrs(rp.OUTFiles)...)

	if isGoModuleType(e.d.unit.Type) {
		for _, out := range rp.OUTNoAutoFiles {
			if strings.HasSuffix(out.string(), ".go") {
				outs = append(outs, out.string())
			}
		}
	}

	if rp.StdoutFile != nil && (!rp.StdoutNoAuto || isGoModuleType(e.d.unit.Type) && strings.HasSuffix(rp.StdoutFile.string(), ".go")) {
		outs = append(outs, rp.StdoutFile.string())
	}

	for _, out := range outs {
		if isGoModuleType(e.d.unit.Type) && strings.HasSuffix(out, ".go") {
			v := copyFileOutputVFS(e.instance.Path.relString(), out)

			e.codegen.use(v)
			e.collectGoAutoSource(v, rp.DeclSeq)

			continue
		}

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

func (e *EmitContext) collectRunProgramObjects(rp *RunProgramStmt) {
	collect := func(out ANY) {
		ext := filepath.Ext(out.string())

		if ext != ".o" && ext != ".obj" && ext != ".a" && (ext != ".lib" || isGoModuleType(e.d.unit.Type)) {
			return
		}

		path := copyFileOutputVFS(e.instance.Path.relString(), out.string())

		e.collectObj(e.codegen.mustInfo(path).ProducerRef, path, SrcMeta{Prio: stmtPrioDefault, Seq: rp.DeclSeq})
	}

	for _, out := range rp.OUTFiles {
		collect(out)
	}

	for _, out := range rp.OUTNoAutoFiles {
		collect(out)
	}

	if rp.StdoutFile != nil {
		collect(*rp.StdoutFile)
	}
}

func (e *EmitContext) emitRunProgram(stmt *RunProgramStmt) {
	ctx, instance := e.ctx, e.instance
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

	e.collectSCInputs(inVFSs[:len(inVFSs)-stmt.INNoParse])

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

	protoImportPbH := prProtoImportPbH(ctx.parsers, inVFSs, e.dirScratch[:0])

	e.dirScratch = protoImportPbH

	res := e.foreignToolResult(internArg(filepath.Clean(stmt.ToolPath.relOrSelf().string())))

	if res.LDPath == nil {
		throwFmt("gen: tool %s has no linkable output", stmt.ToolPath.string())
	}

	auxTools := e.resolveRunProgramAuxTools(anyStrs(stmt.ToolPaths))
	generatorRefs := []NodeRef{res.LDRef}

	for _, tool := range auxTools {
		generatorRefs = append(generatorRefs, tool.ref)
	}

	generatorRefs = ctx.na.refList(dedupRefs(generatorRefs)...)

	prRef := ctx.emit.reserve()

	e.collectCPPStyleInputs(inVFSs...)

	registeredPROut := map[VFS]bool{}

	snap := &prSnap{
		ctx:       ctx,
		scanner:   e.scanner,
		scanCtx:   e.d.scanCtx,
		inVFSs:    ctx.na.vfsList(inVFSs...),
		inSources: ctx.na.vfsList(inSources...),
		inBuilds:  ctx.na.vfsList(inBuilds...),
	}

	pe := func() {
		inputClosure := prInputClosure(snap, stmt)
		inputInduced := prInputInducedIncludes(ctx, e.codegen, snap.scanner, snap.inVFSs[:len(snap.inVFSs)-stmt.INNoParse])

		if len(inputInduced[parsedIncludesAll])+len(inputInduced[parsedIncludesHeader])+len(inputInduced[parsedIncludesCpp]) > 0 {
			for _, output := range outVFSByToken {
				e.codegen.inheritParsedIncludes(output, prRef, inputInduced)
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

	pending := e.pendingEmit(pe)
	induced := preparedInducedIncludes(stmt.InducedDeps)

	registerOutput := func(out VFS, parsed ParsedIncludeSet) {
		if registeredPROut[out] {
			return
		}

		registeredPROut[out] = true
		e.scanner.inheritInducedIncludes(out, induced, snap.inVFSs[:len(snap.inVFSs)-stmt.INNoParse])

		e.register(GeneratedFileSpec{
			OutputPath:      out,
			BuildInputs:     snap.inVFSs,
			ProducerRef:     prRef,
			ProducerMainOut: mainOutputVFS,
			GeneratorRefs:   generatorRefs,
			ParsedIncludes:  ctx.parsers.outputIncludes(out, parsed),
			OnUse:           pending,
		})
	}

	for _, f := range stmt.OUTFiles {
		out := outVFSByToken[f]
		parsed := prOutputParsedIncludes(ctx.na, f, stmt, protoImportPbH)

		registerOutput(out, parsed)
	}

	for _, f := range stmt.OUTNoAutoFiles {
		out := outVFSByToken[f]
		parsed := prOutputParsedIncludes(ctx.na, f, stmt, protoImportPbH)

		registerOutput(out, parsed)
	}

	if stmt.StdoutFile != nil {
		parsed := prOutputParsedIncludes(ctx.na, *stmt.StdoutFile, stmt, protoImportPbH)

		registerOutput(*stdoutVFS, parsed)
	}

	for _, input := range snap.inVFSs[:len(snap.inVFSs)-stmt.INNoParse] {
		snap.scanner.discoverInput(input, snap.scanCtx)
	}
}

func prProtoImportPbH(pm *IncludeParserManager, inVFSs []VFS, dst []IncludeDirective) []IncludeDirective {
	for _, v := range inVFSs {
		if v.isSource() && extIsProto(v.relString()) {
			dst = protoDirectPbHIncludes(pm, v.relString(), "", dst)
		}
	}

	return dst
}

type prSnap struct {
	ctx       *GenCtx
	scanner   *IncludeScanner
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
	var sources, builds []VFS

	for i, f := range stmt.INFiles[:len(stmt.INFiles)-stmt.INNoParse] {
		if s.inVFSs[i].isBuild() || !s.ctx.parsers.registry.scansIncludes(f.string()) {
			continue
		}

		cv := s.scanner.walkClosure(s.inVFSs[i], s.scanCtx, scanDomainCC)

		cv.each(func(v VFS) {
			if v.isBuild() {
				builds = append(builds, v)
			} else {
				sources = append(sources, v)
			}
		})
	}

	return PrInputClosure{sources: s.ctx.na.dedupClosure(sources), builds: s.ctx.na.dedupClosure(builds)}
}

func prInputInducedIncludes(ctx *GenCtx, registry *CodegenRegistry, scanner *IncludeScanner, inputs []VFS) ParsedIncludeSet {
	var induced ParsedIncludeSet

	for _, input := range inputs {
		if input.isBuild() {
			registry.use(input)
		}
	}

	dedupers.with(func(seen *DeDuper) {
		var visit func(VFS)
		visit = func(input VFS) {
			if !seen.addStable(input.strID()) {
				return
			}

			var parsed ParsedIncludeSet

			passIncludes := false

			if input.isBuild() {
				if info := registry.lookup(input); info != nil {
					parsed = info.parsedIncludes()
				}
			} else {
				if extIsProto(input.relString()) {
					return
				}

				cached, ok := ctx.parsers.cache.parsed.get(input.rel())

				if !ok {
					return
				}

				parsed = cached.includes(ctx.parsers.cache)
				passIncludes = cached.parser == includeParserSwig
			}

			for _, bucket := range [...]ParsedIncludeBucket{parsedIncludesAll, parsedIncludesHeader, parsedIncludesCpp} {
				induced[bucket] = append(induced[bucket], parsed[bucket]...)
			}

			if passIncludes {
				if children, ok := scanner.cachedChildren(input); ok {
					for _, child := range children {
						visit(child)
					}
				}
			}
		}

		for _, input := range inputs {
			visit(input)
		}
	})

	for _, bucket := range [...]ParsedIncludeBucket{parsedIncludesAll, parsedIncludesHeader, parsedIncludesCpp} {
		if len(induced[bucket]) > 0 {
			induced[bucket] = ctx.na.dirList(induced[bucket]...)
		}
	}

	return induced
}

func prOutputParsedIncludes(na *NodeArenas, outFile ANY, stmt *RunProgramStmt, protoImportPbH []IncludeDirective) ParsedIncludeSet {
	local := na.dirs.alloc(len(stmt.OutputIncludes))[:0]

	for _, f := range stmt.OutputIncludes {
		local = append(local, IncludeDirective{kind: includeQuoted, target: includeTarget(f)})
	}

	na.dirs.commit(len(local))
	local = local[:len(local):len(local)]

	carryProtoImportPbH := !extIsPbH(outFile.string())
	compile := na.dirs.alloc(len(protoImportPbH))[:0]

	if carryProtoImportPbH {
		compile = append(compile, protoImportPbH...)
	}

	na.dirs.commit(len(compile))

	compile = compile[:len(compile):len(compile)]

	return ParsedIncludeSet{parsedIncludesLocal: local, parsedIncludesHeader: compile, parsedIncludesCpp: compile}
}

func resolveRunProgramAuxTools(ctx *GenCtx, toolPaths []string) []RunProgramAuxTool {
	return collectRunProgramAuxTools(toolPaths, ctx.toolResult)
}

func (e *EmitContext) resolveRunProgramAuxTools(toolPaths []string) []RunProgramAuxTool {
	return collectRunProgramAuxTools(toolPaths, e.foreignToolResult)
}

func collectRunProgramAuxTools(toolPaths []string, resolve func(ARG) *ModuleEmitResult) []RunProgramAuxTool {
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

		res := resolve(internArg(filepath.Clean(modulePath)))

		if res.LDPath == nil {
			throwFmt("gen: tool %s has no linkable output", modulePath)
		}

		out = append(out, RunProgramAuxTool{
			token:  toolPath,
			ref:    res.LDRef,
			bin:    *res.LDPath,
			rooted: rooted,
		})
	}

	return out
}

func prAuxToolTokens(tools []RunProgramAuxTool) []PrFileToken {
	toks := make([]PrFileToken, 0, len(tools))

	for _, tool := range tools {
		if !tool.rooted {
			toks = append(toks, PrFileToken{token: tool.token, rooted: tool.bin.string(), vfs: tool.bin})
		}
	}

	sort.SliceStable(toks, func(i, j int) bool {
		return len(toks[i].token) > len(toks[j].token)
	})

	return toks
}

func replaceAuxToolArg(arg ANY, tools []PrFileToken) (ANY, bool) {
	rooted, file, whole := rootBareFileArg(arg.string(), tools)

	if file == 0 {
		return arg, false
	}

	if whole {
		return file.any(), true
	}

	return internStr(rooted).any(), true
}

func (e *EmitContext) runProgramInputVFS(rel string) VFS {
	if vfsHasPrefix(rel) {
		return e.requireProducedInput("IN", rel, copyFileInputVFS(e.ctx.fs, e.instance.Path, rel))
	}

	return e.ctx.programInputVFS(e.instance, e.d.srcDirs, rel)
}

func (ctx *GenCtx) programInputVFS(instance ModuleInstance, srcDirs []VFS, rel string) VFS {
	if vfsHasPrefix(rel) {
		return internClean(rel)
	}

	registry := ctx.scannerForPlatform(instance.Platform).codegen
	buildVFS := buildJoinClean(instance.Path.relString(), rel)

	if registry.lookup(buildVFS) != nil {
		return buildVFS
	}

	if ctx.fs.isFile(srcRootRel, rel) {
		return sourceClean(rel)
	}

	if rootBuild := buildClean(rel); registry.use(rootBuild) != nil {
		return rootBuild
	}

	return resolveSourceOrGeneratedVFS(ctx, instance, rel, srcDirs)
}

func (e *EmitContext) emitPR(spec RunProgramNodeSpec, id NodeRef) {
	instance := e.instance
	stmt := spec.stmt
	na := e.ctx.na
	env := envVarsVCS
	dirs := Environment{s: &e.frame.ifEnv}

	if e.ctx.frameDepth > 0 {
		dirs = Environment{s: &e.ctx.frames[e.ctx.frameDepth-1].ifEnv}
	}

	var resourceUses []STR
	dirs.resourceUses = &resourceUses

	envPairs := stmt.EnvPairs

	if stmt.CallArgs != nil {
		value := compilerCommandFlags(stmt.EnvPairs, dirs)

		envPairs = value.Args
		resourceUses = resourceKeyUnion(resourceUses, value.Resources)
	} else {
		envPairs = expandTokens(stmt.EnvPairs, dirs, false, tokenExpandFinal)
	}

	if len(envPairs) > 0 {
		block := na.envs.alloc(1 + len(envPairs))[:0]

		block = append(block, envVarsVCS...)

		for _, kv := range envPairs {
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

	args := stmt.Args

	if stmt.CallArgs != nil {
		value := compilerCommandFlags(stmt.Args, dirs)

		args = value.Args
		resourceUses = resourceKeyUnion(resourceUses, value.Resources)
	} else {
		args = expandTokens(stmt.Args, dirs, false, tokenExpandFinal)
	}

	induced := inducedDepsArgs(stmt.InducedDeps)
	cmdArgs := na.anys.alloc(1 + len(args) + len(induced))[:0]

	cmdArgs = append(cmdArgs, spec.toolBinPath.any())

	fileTokens := prBareFileTokens(stmt, spec.inVFSs, spec.outVFSByToken)
	auxToolTokens := prAuxToolTokens(spec.auxTools)

	var argFiles []VFS

	for _, aTok := range args {
		a := aTok.string()
		key, toolReplaced := replaceAuxToolArg(aTok, auxToolTokens)

		if !toolReplaced {
			if rooted, file, whole := rootBareFileArg(a, fileTokens); file != 0 {
				argFiles = append(argFiles, file)

				if whole {
					cmdArgs = append(cmdArgs, file.any())

					continue
				}

				key = internStr(rooted).any()
			}
		}

		cmdArgs = append(cmdArgs, key)
	}

	cmdArgs = append(cmdArgs, induced...)
	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

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

	for _, v := range argFiles {
		if spec.isOutput(v) {
			appendOutput(v)
		}
	}

	for _, f := range stmt.OUTFiles {
		appendOutput(spec.outVFSByToken[f])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		appendOutput(spec.outVFSByToken[f])
	}

	if spec.stdoutVFS != nil {
		stdoutPath = *spec.stdoutVFS
		appendOutput(*spec.stdoutVFS)
	}

	na.vfs.commit(len(outputs))

	outputs = outputs[:len(outputs):len(outputs)]
	e.codegen.setMainOut(id, outputs)

	tools := na.vfs.alloc(1 + len(spec.auxTools))[:0]

	tools = append(tools, spec.toolBinPath)

	for _, tool := range spec.auxTools {
		tools = append(tools, tool.bin)
	}

	na.vfs.commit(len(tools))
	tools = tools[:len(tools):len(tools)]

	inputNode := NodeSpec{Node: Node{Inputs: na.inputList(tools, spec.inSources, spec.inBuilds), Outputs: outputs}, input: InputSpec{roots: na.inputList(spec.inVFSs), text: e.textInputSources(spec.inVFSs, len(spec.inVFSs)-stmt.INNoParse)}}

	e.includeNodeInputs(&inputNode)

	var inputs InputChunks

	dedupers.with(func(deduper *DeDuper) {
		chunks := na.inputs.alloc(len(inputNode.Inputs) + 2)[:0]

		for _, chunk := range inputNode.Inputs {
			chunks = append(chunks, na.filterSeen(deduper, chunk))
		}

		chunks = append(chunks, na.filterSeen(deduper, spec.closureSource), na.filterSeen(deduper, spec.closureBuild))
		na.inputs.commit(len(chunks))
		inputs = chunks[:len(chunks):len(chunks)]
	})

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
		Env:     env, Resources: resourceUses,
	}

	if stdoutPath != 0 {
		cmd.Stdout = stdoutPath
	}

	if stmt.CWD != nil {
		cmd.Cwd = cwdVFS(expandToken((*stmt.CWD).string(), dirs, tokenExpandFinal))
	}

	kv := stmt.KV

	if kv == nil {
		kv = &prKV
	}

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(cmd),
		Env:  env, Inputs: inputs, Outputs: outputs,
		KV:             kv,
		ForeignDepRefs: toolRefs,
		DepRefs:        inputNode.DepRefs, InputFixes: inputNode.InputFixes}, input: InputSpec{roots: inputNode.input.roots, text: inputNode.input.text},
	}

	e.resolveNodeCodegenRefs(&node)
	e.ctx.emit.emitReservedNodePtr(&node.Node, id)
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

func rootBareFileArg(arg string, toks []PrFileToken) (rooted string, file VFS, whole bool) {
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

			return arg[:idx] + c.rooted + arg[end:], c.vfs, false
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
