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

func (e *EmitContext) emitRunProgramStmt(rp *RunProgramStmt) {
	instance := e.instance
	prRef := e.emitRunProgram(rp, e.codegen)
	outs := make([]string, 0, len(rp.OUTFiles)+1)

	outs = append(outs, strStrings(rp.OUTFiles)...)

	if rp.StdoutFile != nil && !rp.StdoutNoAuto {
		outs = append(outs, rp.StdoutFile.string())
	}

	for _, out := range outs {
		if v := flatcVariantForExt(out); v != nil {
			e.emitFlatcProducer(copyFileOutputVFS(instance.Path.rel(), out), v, []NodeRef{prRef})
		}
	}

	for _, out := range outs {
		if isCCSourceExt(out) || isAsmSourceExt(out) {
			e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.rel(), out).str(), Prio: stmtPrioDefault, Seq: rp.DeclSeq, Generated: true})
		}
	}

	for _, out := range outs {
		if flatcVariantForExt(out) == nil {
			continue
		}

		cppVFS := build(copyFileOutputVFS(instance.Path.rel(), out).rel(), ".cpp")

		e.enqueueSrc(SrcMeta{Source: cppVFS.str(), Prio: stmtPrioDefault, Seq: rp.DeclSeq, Generated: true, SecondLevel: true})
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

func (e *EmitContext) emitRunProgram(stmt *RunProgramStmt, reg *CodegenRegistry) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	res := ctx.toolResult(internArg(filepath.Clean(stmt.ToolPath.string())))
	toolLDRef := res.LDRef
	toolBinPath := *res.LDPath
	auxTools := resolveRunProgramAuxTools(ctx, strStrings(stmt.ToolPaths))
	inVFSByToken := make(map[STR]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		vfs := e.runProgramInputVFS(f.string())

		inVFSByToken[f] = vfs
		inVFSs = append(inVFSs, vfs)
	}

	outVFSByToken := make(map[STR]VFS, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)

	for _, f := range stmt.OUTFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.rel(), f.string())
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.rel(), f.string())
	}

	var stdoutVFS *VFS

	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path.rel(), stmt.StdoutFile.string())

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

	var prSourceInputs []VFS
	var prGeneratedFromSources []VFS

	for _, v := range inVFSs {
		if v.isSource() {
			prSourceInputs = append(prSourceInputs, v)

			continue
		}

		if info := reg.lookup(v); info != nil {
			prGeneratedFromSources = append(prGeneratedFromSources, info.SourceInputs...)
		}
	}

	prSourceInputs = append(prSourceInputs, prGeneratedFromSources...)

	var protoImportPbH []IncludeDirective

	for _, v := range inVFSs {
		if v.isSource() && extIsProto(v.rel()) {
			protoImportPbH = append(protoImportPbH, protoDirectPbHIncludes(ctx.parsers, v.rel(), "")...)
		}
	}

	prRef := ctx.emit.reserve()
	registeredPROut := map[VFS]bool{}
	mainIsHeader := mainOutputVFS != 0 && isHeaderSource(mainOutputVFS.rel())

	mainHeaderInclude := func(ccOutRel string) (IncludeDirective, bool) {
		if !mainIsHeader || relStem(ccOutRel) != relStem(mainOutputVFS.rel()) {
			return IncludeDirective{}, false
		}

		return IncludeDirective{kind: includeQuoted, target: internStr(mainOutputVFS.rel())}, true
	}

	registerPROutput := func(out VFS, parsed []IncludeDirective, ridesHeaderViaParsed bool) {
		if registeredPROut[out] {
			return
		}

		registeredPROut[out] = true

		leaves := prGeneratedFromSources

		if out != mainOutputVFS && !ridesHeaderViaParsed {
			leaves = append([]VFS{mainOutputVFS}, prGeneratedFromSources...)
		}

		info := &GeneratedFileInfo{
			OutputPath:     out,
			ProducerRef:    prRef,
			GeneratorRefs:  []NodeRef{toolLDRef},
			ParsedIncludes: parsed,
			SourceInputs:   prSourceInputs,
			ClosureLeaves:  leaves,
		}

		e.codegen.register(info)
	}

	parsedFor := func(f STR, out VFS, auto bool) ([]IncludeDirective, bool) {
		parsed := prEmitsIncludes(f, stmt, inVFSs, protoImportPbH)

		if auto && isCCSourceExt(f.string()) {
			if inc, ok := mainHeaderInclude(out.rel()); ok {
				return append(parsed, inc), true
			}
		}

		return parsed, false
	}

	for _, f := range stmt.OUTFiles {
		out := outVFSByToken[f]
		parsed, rides := parsedFor(f, out, true)

		registerPROutput(out, parsed, rides)
	}

	for _, f := range stmt.OUTNoAutoFiles {
		out := outVFSByToken[f]
		parsed, rides := parsedFor(f, out, false)

		registerPROutput(out, parsed, rides)
	}

	if stmt.StdoutFile != nil {
		parsed, rides := parsedFor(*stmt.StdoutFile, *stdoutVFS, !stmt.StdoutNoAuto)

		registerPROutput(*stdoutVFS, parsed, rides)
	}

	inputClosure := e.prInputClosure(stmt)

	if prSourceClosure := filterSourceVFS(inputClosure); len(prSourceClosure) > 0 {
		for out := range registeredPROut {
			reg.addSourceInputs(out, prSourceClosure)
		}
	}

	depInputs := inputClosure

	if len(inVFSs) > 0 {
		depInputs = concat(inVFSs, inputClosure)
	}

	prExtraDepRefs := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, depInputs)

	emitPR(instance, stmt, toolBinPath, toolLDRef, auxTools, inVFSByToken, inVFSs, outVFSByToken, stdoutVFS, inputClosure, prExtraDepRefs, d.unit.CCTag, prRef, ctx.emit)

	return prRef
}

func filterSourceVFS(vs []VFS) []VFS {
	n := 0

	for _, v := range vs {
		if v.isSource() {
			n++
		}
	}

	if n == len(vs) {
		return vs
	}

	out := make([]VFS, 0, n)

	for _, v := range vs {
		if v.isSource() {
			out = append(out, v)
		}
	}

	return out
}

func pbhBasenameSet(vs []VFS) map[string]bool {
	m := map[string]bool{}

	for _, v := range vs {
		if extIsPbH(v.rel()) {
			m[filepath.Base(v.rel())] = true
		}
	}

	return m
}

func (e *EmitContext) prInputClosure(stmt *RunProgramStmt) []VFS {
	ctx, instance, d := e.ctx, e.instance, e.d
	hasAutoCCSourceOut := stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string())

	for _, f := range stmt.OUTFiles {
		if isCCSourceExt(f.string()) {
			hasAutoCCSourceOut = true

			break
		}
	}

	mainIsCCSource := isCCSourceExt(prMainOutputRel(stmt))
	fullSourceClosure := len(stmt.INFiles) == 0 && (!hasAutoCCSourceOut || mainIsCCSource)

	if len(stmt.INFiles) == 0 && !fullSourceClosure {
		return nil
	}

	hasProtoIN := false
	hasParsedIN := false

	for _, f := range stmt.INFiles {
		if extIsProto(f.string()) {
			hasProtoIN = true
		}

		if ctx.parsers.registry.hasRegisteredParser(f.string()) {
			hasParsedIN = true
		}
	}

	generatesHeader := stmt.StdoutFile != nil && isHeaderSource(stmt.StdoutFile.string())

	for _, f := range stmt.OUTFiles {
		if isHeaderSource(f.string()) {
			generatesHeader = true

			break
		}
	}

	selfScanGeneratedCC := len(stmt.INFiles) > 0 && (hasParsedIN || !generatesHeader)
	scanCfg := newScanContext(ctx.parsers, d.cc.AddIncl, d.cc.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

	var out []VFS

	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.rel(), rel)
		sub := walkClosureTail(e.scanner, buildRootPath, scanCfg)

		out = append(out, sub...)
	}

	walkInput := func(rel string) {
		inputVFS := e.runProgramInputVFS(rel)
		sub := walkClosure(e.scanner, inputVFS, scanCfg)

		out = append(out, sub...)
	}

	mainRel := prMainOutputRel(stmt)

	ridesMainHeader := func(ccRel string) bool {
		return isHeaderSource(mainRel) && relStem(ccRel) == relStem(mainRel)
	}

	if selfScanGeneratedCC {
		for _, f := range stmt.OUTFiles {
			if !isCCSourceExt(f.string()) || ridesMainHeader(f.string()) {
				continue
			}

			walkOne(f.string())
		}
	}

	if selfScanGeneratedCC && stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string()) &&
		!ridesMainHeader(stmt.StdoutFile.string()) {
		walkOne(stmt.StdoutFile.string())
	}

	for _, f := range stmt.INFiles {
		rel := f.string()

		if ctx.parsers.registry.hasRegisteredParser(rel) {
			walkInput(rel)

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

			for _, v := range walkClosureTail(e.scanner, copyFileOutputVFS(instance.Path.rel(), f.string()), scanCfg) {
				if v.isSource() {
					out = append(out, v)
				}
			}
		}
	}

	reg := e.codegen

	keep := func(v VFS) bool {
		if fullSourceClosure {
			return v.isSource()
		}

		return extIsProto(v.rel())
	}

	pbhSeen := pbhBasenameSet(out)

	for _, oi := range stmt.OutputIncludes {
		target := oi

		if vfsHasPrefix(target.string()) {
			target = internStr(intern(target.string()).rel())
		}

		candidate := build(target.string())

		var sub []VFS

		switch info := reg.lookup(candidate); {
		case info != nil:

			sub = walkClosureTail(e.scanner, info.OutputPath, scanCfg)
		case fullSourceClosure && ctx.fs.isFile(srcRootVFS, target.string()):

			sub = walkClosure(e.scanner, source(target.string()), scanCfg)
		default:
			continue
		}

		for _, v := range sub {
			if !keep(v) {
				continue
			}

			out = append(out, v)

			if extIsPbH(v.rel()) {
				pbhSeen[filepath.Base(v.rel())] = true
			}

			if !fullSourceClosure && !hasProtoIN && v.isSource() && extIsProto(v.rel()) {
				sibling := strings.TrimSuffix(v.rel(), ".proto") + ".pb.h"
				sibDir, sibBase := splitDirName(sibling)

				if ctx.fs.isFile(dirKey(sibDir), sibBase) && !pbhSeen[sibBase] {
					out = append(out, source(sibling))
					pbhSeen[sibBase] = true
				}
			}
		}
	}

	if len(out) == 0 {
		return nil
	}

	out = dedup(out, nil)

	return out
}

func prEmitsIncludes(outFile STR, stmt *RunProgramStmt, inVFSs []VFS, protoImportPbH []IncludeDirective) []IncludeDirective {
	carries := generatedOutputCarriesIncludes(outFile.string())

	if !carries && len(stmt.OutputIncludes) == 0 {
		return nil
	}

	carryProtoImportPbH := isHeaderSource(outFile.string()) && !extIsPbH(outFile.string())
	n := len(stmt.OutputIncludes)

	if carries {
		n += len(inVFSs)
	}

	if carryProtoImportPbH {
		n += len(protoImportPbH)
	}

	includes := make([]IncludeDirective, 0, n)

	if carries {
		for _, v := range inVFSs {
			if v.isBuild() {
				continue
			}

			includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
		}
	}

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = internStr(v.rel())
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: f})
	}

	if carryProtoImportPbH {
		includes = append(includes, protoImportPbH...)
	}

	return includes
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
			modulePath = intern(toolPath).rel()
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

	switch {
	case strings.HasPrefix(rel, "$(S)/"),
		strings.HasPrefix(rel, "$(B)/"),
		strings.HasPrefix(rel, "${ARCADIA_ROOT}/"),
		strings.HasPrefix(rel, "${CURDIR}/"),
		strings.HasPrefix(rel, "${ARCADIA_BUILD_ROOT}/"),
		strings.HasPrefix(rel, "${BINDIR}/"):
		return e.requireProducedInput("IN", rel, copyFileInputVFS(ctx.fs, instance.Path, rel))
	}

	buildVFS := build(filepath.ToSlash(filepath.Clean(instance.Path.rel() + "/" + rel)))

	if e.codegen.lookup(buildVFS) != nil {
		return buildVFS
	}

	if ctx.fs.isFile(srcRootVFS, rel) {
		return source(rel)
	}

	return e.resolveModuleSourceVFS(internStr(rel), d.srcDirs)
}

func emitPR(
	instance ModuleInstance,
	stmt *RunProgramStmt,
	toolBinPath VFS,
	toolLDRef NodeRef,
	auxTools []RunProgramAuxTool,
	inVFSByToken map[STR]VFS,
	inVFSs []VFS,
	outVFSByToken map[STR]VFS,
	stdoutVFS *VFS,
	inputClosure []VFS,
	extraDepRefs []NodeRef,
	moduleTag STR,
	id NodeRef,
	emit *StreamingEmitter,
) {
	na := emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv.string(), "=", 2)

		if len(parts) == 2 {
			env = append(env, EnvVar{Name: internEnv(parts[0]), Value: internStr(parts[1])})
		} else {
			env = append(env, EnvVar{Name: internEnv(kv.string()), Value: strEmpty})
		}
	}

	cmdArgs := make([]STR, 0, 1+len(stmt.Args))

	cmdArgs = append(cmdArgs, (toolBinPath).str())

	cands := deepReplaceCandidates(stmt, inVFSByToken, outVFSByToken)

	for _, aTok := range stmt.Args {
		a := aTok.string()
		key := aTok
		toolReplaced := false

		for _, tool := range auxTools {
			if tool.rooted {
				continue
			}

			if strings.Contains(a, tool.token) {
				a = strings.ReplaceAll(a, tool.token, tool.bin.string())
				key = internStr(a)
				toolReplaced = true
			}
		}

		if !toolReplaced {
			if rooted, ok := deepReplacePathArg(a, cands); ok {
				key = internStr(rooted)
			}
		}

		cmdArgs = append(cmdArgs, key)
	}

	head := make([]VFS, 0, 1+len(auxTools)+len(stmt.INFiles))

	deduper.reset()

	appendUnique := func(p VFS) {
		if !deduper.add(p) {
			return
		}

		head = append(head, p)
	}

	appendUnique(toolBinPath)

	for _, tool := range auxTools {
		appendUnique(tool.bin)
	}

	for _, v := range inVFSs {
		appendUnique(v)
	}

	inputs := na.inputList(head, deduper.filterSeen(inputClosure))

	var outputs []VFS
	var stdoutPath STR

	emittedOut := map[VFS]bool{}

	appendOutput := func(v VFS) {
		if emittedOut[v] {
			return
		}

		emittedOut[v] = true
		outputs = append(outputs, v)
	}

	if stdoutVFS != nil {
		stdoutPath = stdoutVFS.str()
		appendOutput(*stdoutVFS)
	}

	for _, f := range stmt.OUTFiles {
		appendOutput(outVFSByToken[f])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		appendOutput(outVFSByToken[f])
	}

	var toolRefs []NodeRef

	for _, tool := range auxTools {
		toolRefs = append(toolRefs, depRefs(tool.ref)...)
	}

	toolRefs = append(toolRefs, depRefs(toolLDRef)...)

	deps := append([]NodeRef(nil), extraDepRefs...)
	foreignDepRefs := toolRefs

	cmd := Cmd{
		CmdArgs: na.chunkList(cmdArgs),
		Env:     env,
	}

	if stdoutPath != 0 {
		cmd.Stdout = stdoutPath
	}

	if stmt.CWD != nil {
		cmd.Cwd = *stmt.CWD
	}

	node := &Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd),
		Env:            env,
		Inputs:         inputs,
		Outputs:        outputs,
		KV:             &prKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        deps,
		ForeignDepRefs: foreignDepRefs,
	}

	emit.emitReserved(node, id)
}

type DeepReplaceCand struct {
	token  string
	rooted string
}

func deepReplaceCandidates(stmt *RunProgramStmt, inVFSByToken, outVFSByToken map[STR]VFS) []DeepReplaceCand {
	cands := make([]DeepReplaceCand, 0, len(stmt.INFiles)+len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

	add := func(tok STR, vfs VFS, ok bool) {
		if !ok {
			return
		}

		t := tok.string()

		if !mustDeepReplacePath(t) {
			return
		}

		cands = append(cands, DeepReplaceCand{token: t, rooted: vfs.string()})
	}

	for _, f := range stmt.INFiles {
		vfs, ok := inVFSByToken[f]

		add(f, vfs, ok)
	}

	for _, f := range stmt.OUTFiles {
		vfs, ok := outVFSByToken[f]

		add(f, vfs, ok)
	}

	for _, f := range stmt.OUTNoAutoFiles {
		vfs, ok := outVFSByToken[f]

		add(f, vfs, ok)
	}

	sort.SliceStable(cands, func(i, j int) bool {
		return len(cands[i].token) > len(cands[j].token)
	})

	return cands
}

func mustDeepReplacePath(p string) bool {
	switch {
	case strings.HasPrefix(p, "$(S)/"),
		strings.HasPrefix(p, "$(B)/"),
		strings.HasPrefix(p, "${ARCADIA_ROOT}/"),
		strings.HasPrefix(p, "${ARCADIA_BUILD_ROOT}/"),
		strings.HasPrefix(p, "${CURDIR}/"),
		strings.HasPrefix(p, "${BINDIR}/"),
		strings.HasPrefix(p, "/"):
		return false
	}

	return true
}

func deepReplacePathArg(arg string, cands []DeepReplaceCand) (string, bool) {
	for _, c := range cands {
		idx := strings.Index(arg, c.token)

		if idx < 0 {
			continue
		}

		end := idx + len(c.token)
		beforeOK := idx == 0 || isDeepReplaceBoundary(arg[idx-1])
		afterOK := end == len(arg) || isDeepReplaceBoundary(arg[end])

		if beforeOK && afterOK {
			return arg[:idx] + c.rooted + arg[end:], true
		}
	}

	return arg, false
}

func isDeepReplaceBoundary(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return false
	case c == '.', c == '_', c == '-', c == '"', c == '/':
		return false
	}

	return true
}
