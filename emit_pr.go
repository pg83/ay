package main

import (
	"path/filepath"
	"sort"
	"strings"
)

type RunProgramsForARResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
	// Seqs parallels CCRefs/CCOutputs: each member's RUN_PROGRAM declaration sequence.
	Seqs []int
	// SecondLevel parallels CCRefs/CCOutputs: true for a member compiled from a
	// second-level generated source, which archives after every first-level member.
	SecondLevel []bool
}

type RunProgramAuxTool struct {
	token string
	ref   NodeRef
	bin   VFS
	// rooted marks a TOOL whose path already carries a build-root prefix ($(B)/…):
	// it only registers the dependency, no arg substitution applies. A relative TOOL
	// instead rewrites its matching arg token to the resolved binary.
	rooted bool
}

func emitRunProgramsForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) *RunProgramsForARResult {
	if len(d.runPrograms) == 0 {
		return nil
	}

	reg := codegenRegForInstance(ctx, instance)
	res := &RunProgramsForARResult{}

	type runEntry struct {
		prRef NodeRef
		outs  []string
		seq   int
	}

	runs := make([]runEntry, 0, len(d.runPrograms))

	// Pass 1: emit every PR node, then bridge each auto `.fbs`/`.fbs64` STDOUT/OUT
	// to its flatc producer. Producers register before any cc compile below, so a
	// sibling run whose cc-source #includes a generated .fbs.h resolves it regardless
	// of run order.
	for _, rp := range d.runPrograms {
		prRef := emitRunProgram(ctx, instance, rp, d, reg, in)

		outs := make([]string, 0, len(rp.OUTFiles)+len(rp.OUTNoAutoFiles)+1)
		outs = append(outs, strStrings(rp.OUTFiles)...)

		// Only auto STDOUT is a module source; STDOUT_NOAUTO is excluded like OUT_NOAUTO.
		if rp.StdoutFile != nil && !rp.StdoutNoAuto {
			outs = append(outs, rp.StdoutFile.string())
		}

		runs = append(runs, runEntry{prRef: prRef, outs: outs, seq: rp.DeclSeq})

		for _, out := range outs {
			if v := flatcVariantForExt(out); v != nil {
				emitFlatcProducer(ctx, instance, d, copyFileOutputVFS(instance.Path.rel(), out), v, []NodeRef{prRef})
			}
		}
	}

	// Pass 2: direct cc/asm auto-source compiles — the first-level objects.
	for _, run := range runs {
		for _, out := range run.outs {
			switch {
			case isCCSourceExt(out):
				ccRef, ccOut := emitPRDownstreamCC(ctx, instance, out, run.prRef, in)
				res.CCRefs = append(res.CCRefs, ccRef)
				res.CCOutputs = append(res.CCOutputs, ccOut)
				res.Seqs = append(res.Seqs, run.seq)
				res.SecondLevel = append(res.SecondLevel, false)
			case isAsmSourceExt(out):
				asRef, asOut := emitCodegenDownstreamAS(ctx, instance, out, []NodeRef{run.prRef}, in)
				res.CCRefs = append(res.CCRefs, asRef)
				res.CCOutputs = append(res.CCOutputs, asOut)
				res.Seqs = append(res.Seqs, run.seq)
				res.SecondLevel = append(res.SecondLevel, false)
			}
		}
	}

	// Pass 3: flatc-generated .fbs.cpp compiles — the second-level generated
	// sources, archived after every first-level object.
	for _, run := range runs {
		for _, out := range run.outs {
			if flatcVariantForExt(out) == nil {
				continue
			}

			cppVFS := build(copyFileOutputVFS(instance.Path.rel(), out).rel() + ".cpp")
			emit := emitFlatcCppCompile(ctx, instance, cppVFS, in)
			res.CCRefs = append(res.CCRefs, emit.Ref)
			res.CCOutputs = append(res.CCOutputs, emit.OutPath)
			res.Seqs = append(res.Seqs, run.seq)
			res.SecondLevel = append(res.SecondLevel, true)
		}
	}

	return res
}

// prMainOutputRel returns the relative path of the run's MAIN output — the first
// in output order OUT, OUT_NOAUTO, STDOUT. Its extension decides whether the
// OUTPUT_INCLUDES closure rides the producer. Empty when the run declares no output.
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

// flatcVariantForExt returns the flatc variant for a generated .fbs/.fbs64 module
// source, or nil for any other extension.
func flatcVariantForExt(p string) *flatcVariant {
	switch {
	case strings.HasSuffix(p, ".fbs64"):
		return &flatcVariantFL64
	case strings.HasSuffix(p, ".fbs"):
		return &flatcVariantFL
	}

	return nil
}

func emitRunProgram(ctx *GenCtx, instance ModuleInstance, stmt *RunProgramStmt, d *ModuleData, reg *CodegenRegistry, moduleInputs ModuleCCInputs) NodeRef {
	res := ctx.toolResult(internArg(filepath.Clean(stmt.ToolPath.string())))
	toolLDRef := res.LDRef
	toolBinPath := *res.LDPath
	auxTools := resolveRunProgramAuxTools(ctx, strStrings(stmt.ToolPaths))
	inVFSByToken := make(map[STR]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f.string())
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

	// The run's MAIN output: the first in command order OUT, OUT_NOAUTO, STDOUT.
	// The command builds one node keyed on it; the others are OutTogether siblings.
	var mainOutputVFS VFS

	switch {
	case len(stmt.OUTFiles) > 0:
		mainOutputVFS = outVFSByToken[stmt.OUTFiles[0]]
	case len(stmt.OUTNoAutoFiles) > 0:
		mainOutputVFS = outVFSByToken[stmt.OUTNoAutoFiles[0]]
	case stdoutVFS != nil:
		mainOutputVFS = *stdoutVFS
	}

	// The run's $(S) source inputs are real inputs of any transitive consumer of a
	// generated output; recorded on each output for the archive emit to propagate as
	// closure leaves. A $(B) input's own $(S) generator sources fold in (transitive
	// through the producer chain); the $(B)-derived subset also rides as a ClosureLeaf.
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

	// A custom header generated FROM a .proto IN #includes the generated `.pb.h` of
	// that proto's imports. We never scan the generated body, so carry the import's
	// `.pb.h` on the header's parsed-include window. Headers only — a `.pb.h` OUT
	// already roots its own proto graph.
	var protoImportPbH []IncludeDirective

	for _, v := range inVFSs {
		if v.isSource() && strings.HasSuffix(v.rel(), ".proto") {
			protoImportPbH = append(protoImportPbH, protoDirectPbHIncludes(ctx.parsers, v.rel(), "")...)
		}
	}

	// A run "self-consumes" when its module auto-compiles a cc/asm-source OUT (never
	// OUT_NOAUTO): the producer is the first DFS-leaver of every output, so its outputs
	// keep the producer's module_dir and the consumer-claim override must not move
	// them. A header-only / OUT_NOAUTO run keeps first-consumer attribution.
	selfConsumes := false

	for _, f := range stmt.OUTFiles {
		if s := f.string(); isCCSourceExt(s) || isAsmSourceExt(s) {
			selfConsumes = true

			break
		}
	}

	if stmt.StdoutFile != nil && !stmt.StdoutNoAuto {
		if s := stmt.StdoutFile.string(); isCCSourceExt(s) || isAsmSourceExt(s) {
			selfConsumes = true
		}
	}

	// Reserve the PR producer's ref before registering its outputs: the input closure
	// walk below resolves sibling codegen deps that may include these outputs.
	prRef := ctx.emit.reserve()

	// A RUN_PROGRAM may name the same file in more than one output role; they denote
	// one physical output, so register each distinct output VFS once (a second
	// registration trips the codegen registry's duplicate-producer guard).
	registeredPROut := map[VFS]bool{}

	// A build-generated `.proto` declares its direct imports through the run's
	// OUTPUT_INCLUDES `.proto` entries (its `import` statements cannot be scanned).
	// The consuming CPP_PROTO emission reads these to seed the generated .pb.h's
	// direct includes.
	var protoOutputIncludeRels []string

	for _, oi := range stmt.OutputIncludes {
		rel := oi.string()

		if vfsHasPrefix(rel) {
			rel = intern(rel).rel()
		}

		if strings.HasSuffix(rel, ".proto") {
			protoOutputIncludeRels = append(protoOutputIncludeRels, rel)
		}
	}

	// A "generated header + its implementation" run emits a source #including its own
	// header. We never scan generated bodies, so model that edge when the MAIN output
	// is a header sharing the source's stem; walkClosure then expands the header's
	// window into the compile inputs.
	mainIsHeader := mainOutputVFS != 0 && isHeaderSource(mainOutputVFS.rel())

	mainHeaderInclude := func(ccOutRel string) (IncludeDirective, bool) {
		if !mainIsHeader || relStem(ccOutRel) != relStem(mainOutputVFS.rel()) {
			return IncludeDirective{}, false
		}

		return IncludeDirective{kind: includeQuoted, target: internStr(mainOutputVFS.rel())}, true
	}

	// registerPROutput registers one output's parsed includes and closure edges.
	// ridesHeaderViaParsed marks the auto-compiled cc-source that already got the
	// same-stem main header as a parsed include: skip the main-output closure leaf for
	// it to avoid doubling the edge. Other non-main outputs keep the leaf.
	registerPROutput := func(out VFS, parsed []IncludeDirective, ridesHeaderViaParsed bool) {
		if registeredPROut[out] {
			return
		}

		registeredPROut[out] = true

		registerBoundGeneratedParsedOutput(ctx, instance, pkPR, out, parsed, prRef, []NodeRef{toolLDRef})
		reg.setSourceInputs(out, prSourceInputs)

		if strings.HasSuffix(out.rel(), ".proto") {
			reg.setProtoImportRels(out, protoOutputIncludeRels)
		}

		// A self-consuming run owns its outputs: record the producer module dir now
		// (before any consumer resolves the output) so the override keeps the node
		// attributed to its producer.
		if selfConsumes {
			ctx.scannerFor(instance).markGeneratedProducerOwned(out, instance.Path.rel())
		}

		// A non-main output is an OutTogether sibling: ride the main output as a
		// non-expanded closure leaf of the sibling, so the scanner splices it into
		// every window containing the sibling. The leaf never rides onto the PR
		// producer itself (dropOwnOutputs strips it).
		if out != mainOutputVFS && !ridesHeaderViaParsed {
			reg.addClosureLeaf(out, mainOutputVFS)
		}

		for _, s := range prGeneratedFromSources {
			reg.addClosureLeaf(out, s)
		}
	}

	// parsedFor builds an output's registered parsed includes, appending the same-stem
	// main header to an auto-compiled cc-source that is its implementation unit.
	// Returns whether the header include was appended (so registerPROutput drops the
	// redundant main-output leaf).
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

	inputClosure := prInputClosure(ctx, instance, d, stmt, moduleInputs)

	// A command never inputs its own outputs. The cc-source OUT windows now carry the
	// OutTogether main-output leaf, which must ride onto CONSUMERS, not the producer;
	// drop any own-output VFS the self-walk pulled in.
	inputClosure = dropOwnOutputs(inputClosure, outVFSByToken)

	// Record the producer's transitive $(S) source closure on each registered output;
	// a bytecode node compiling a generated PY_SRCS source folds this set onto itself
	// while the $(B) intermediates stay behind the producer node edge.
	if prSourceClosure := filterSourceVFS(inputClosure); len(prSourceClosure) > 0 {
		for out := range registeredPROut {
			reg.setProducerSourceClosure(out, prSourceClosure)
		}
	}

	// A build-rooted IN that is a registered codegen output but carries no include
	// parser never enters inputClosure, yet the PR still depends on its producer.
	// Resolve producer deps over the IN set as well as the walked closure.
	depInputs := inputClosure

	if len(inVFSs) > 0 {
		depInputs = append(append(make([]VFS, 0, len(inVFSs)+len(inputClosure)), inVFSs...), inputClosure...)
	}

	// Exclude prRef as well as the tool: a PR output appearing in another output's
	// closure must not become a self-dependency.
	prExtraDepRefs := resolveCodegenDepRefs(ctx, instance, depInputs, toolLDRef, prRef)

	emitPR(instance, stmt, toolBinPath, toolLDRef, auxTools, inVFSByToken, inVFSs, outVFSByToken, stdoutVFS, inputClosure, prExtraDepRefs, cfModuleTag(d, instance), prRef, ctx.emit)

	return prRef
}

// dropOwnOutputs removes this run's declared output VFSs from its own input
// closure — a producer never depends on a file it produces.
func dropOwnOutputs(closure []VFS, outVFSByToken map[STR]VFS) []VFS {
	if len(closure) == 0 || len(outVFSByToken) == 0 {
		return closure
	}

	owned := make(map[VFS]bool, len(outVFSByToken))

	for _, v := range outVFSByToken {
		owned[v] = true
	}

	kept := closure[:0:0]

	for _, v := range closure {
		if owned[v] {
			continue
		}

		kept = append(kept, v)
	}

	return kept
}

// filterSourceVFS returns the $(S)-rooted subset of vs, sharing the backing array
// when nothing is dropped.
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

// isCodegenProtoHeader reports whether v is a registered generated proto header
// (`x.pb.h`), excluding the lite `.deps.pb.h` intermediate.
func isCodegenProtoHeader(reg *CodegenRegistry, v VFS) bool {
	rel := v.rel()

	return strings.HasSuffix(rel, ".pb.h") && !strings.HasSuffix(rel, ".deps.pb.h") && reg.lookup(v) != nil
}

// pbhBasenameSet collects the basenames of every `.pb.h` already in vs. The WKT
// checked-in sibling synthesis skips a kept .proto whose basename is already here.
func pbhBasenameSet(vs []VFS) map[string]bool {
	m := map[string]bool{}

	for _, v := range vs {
		if strings.HasSuffix(v.rel(), ".pb.h") {
			m[filepath.Base(v.rel())] = true
		}
	}

	return m
}

// relStem strips a path's final extension, leaving dir + basename-without-ext.
func relStem(rel string) string {
	return strings.TrimSuffix(rel, filepath.Ext(rel))
}

func isCCSourceExt(p string) bool {
	return strings.HasSuffix(p, ".cpp") ||
		strings.HasSuffix(p, ".cc") ||
		strings.HasSuffix(p, ".cxx") ||
		strings.HasSuffix(p, ".c")
}

func isAsmSourceExt(p string) bool {
	return strings.HasSuffix(p, ".asm") ||
		strings.HasSuffix(p, ".s") ||
		strings.HasSuffix(p, ".S")
}

func generatedOutputCarriesIncludes(p string) bool {
	return isCCSourceExt(p) || isHeaderSource(p) || strings.HasSuffix(p, ".inc")
}

func prInputClosure(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *RunProgramStmt, moduleInputs ModuleCCInputs) []VFS {
	// OUTPUT_INCLUDES are induced deps on the run's MAIN output; whether their $(S)
	// source closure surfaces on the PRODUCER depends on that output's type: a
	// cc-source main rides the closure on the producer; a HEADER with a compiled
	// cc-source sibling routes the includes through the header to consumers; a
	// header-only / OUT_NOAUTO run has nowhere else and rides the producer. An IN file
	// roots the producer's graph regardless.
	hasAutoCCSourceOut := stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string())

	for _, f := range stmt.OUTFiles {
		if isCCSourceExt(f.string()) {
			hasAutoCCSourceOut = true

			break
		}
	}

	mainIsCCSource := isCCSourceExt(prMainOutputRel(stmt))

	// The OUTPUT_INCLUDES source closure rides the producer when the run has no IN and
	// either no cc-source sibling surfaces it or the main output is itself the cc-source.
	fullSourceClosure := len(stmt.INFiles) == 0 && (!hasAutoCCSourceOut || mainIsCCSource)

	if len(stmt.INFiles) == 0 && !fullSourceClosure {
		return nil
	}

	// A run consuming .proto IN files roots its proto graph at those IN .proto, whose
	// scan induces .proto imports — never their generated .pb.h. So the OUTPUT_INCLUDES
	// walk below must NOT re-synthesize the checked-in WKT .pb.h sibling for such a run.
	hasProtoIN := false
	// An IN roots a real C++ include graph only when its extension maps to a registered
	// parser; a data IN induces no include edges.
	hasParsedIN := false

	for _, f := range stmt.INFiles {
		if strings.HasSuffix(f.string(), ".proto") {
			hasProtoIN = true
		}

		if includeDirectiveParsers.hasRegisteredParser(f.string()) {
			hasParsedIN = true
		}
	}

	// When the run also generates a HEADER output, its OUTPUT_INCLUDES ride that header
	// to the cc-source's consumers, so the producer does not self-carry the closure. A
	// run generating NO header has nowhere else to route them, so the generated
	// cc-source's closure surfaces on the producer's self-scan. A parsed IN roots the
	// producer's graph regardless.
	generatesHeader := stmt.StdoutFile != nil && isHeaderSource(stmt.StdoutFile.string())

	for _, f := range stmt.OUTFiles {
		if isHeaderSource(f.string()) {
			generatesHeader = true

			break
		}
	}

	// The generated cc-source is self-scanned onto the producer for an IN-rooted run,
	// EXCEPT a data-IN run whose generated header sibling already carries the
	// OUTPUT_INCLUDES. A no-IN run never self-scans.
	selfScanGeneratedCC := len(stmt.INFiles) > 0 && (hasParsedIN || !generatesHeader)

	scanIn := ModuleCCInputs{
		TC:                d.tc,
		InclArgs:          ctx.inclArgs,
		Flags:             moduleInputs.Flags,
		AddIncl:           moduleInputs.AddIncl,
		PeerAddInclGlobal: moduleInputs.PeerAddInclGlobal,
		SrcDirs:           moduleInputs.SrcDirs,
		FS:                ctx.fs,
		ScanCfg:           newScanContext(ctx.parsers, moduleInputs.AddIncl, moduleInputs.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel()),
	}

	var out []VFS
	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.rel(), rel)
		sub := walkClosureTail(ctx.scannerFor(instance), buildRootPath, scanIn.ScanCfg)
		out = append(out, sub...)
	}
	walkInput := func(rel string) {
		inputVFS := runProgramInputVFS(ctx, instance, d, rel)
		sub := walkClosure(ctx.scannerFor(instance), inputVFS, scanIn.ScanCfg)
		out = append(out, sub...)
	}

	// A generated cc-source that is the implementation unit of a same-producer HEADER
	// main output is modeled as #including that header. Its header-routed closure
	// belongs to CONSUMERS, not the producer, so self-scanning it (which would expand
	// the header's window back onto the producer) is skipped.
	mainRel := prMainOutputRel(stmt)
	ridesMainHeader := func(ccRel string) bool {
		return isHeaderSource(mainRel) && relStem(ccRel) == relStem(mainRel)
	}

	// For an IN-rooted run the producer scans its own generated cc-source, surfacing
	// its OUTPUT_INCLUDES closure. A no-IN run, or a data-IN run with a header sibling
	// carrying those includes, must NOT scan its own .cpp (selfScanGeneratedCC gates).
	if selfScanGeneratedCC {
		for _, f := range stmt.OUTFiles {
			if !isCCSourceExt(f.string()) || ridesMainHeader(f.string()) {
				continue
			}

			walkOne(f.string())
		}
	}

	// OUT_NOAUTO outputs are registered but EXCLUDED from the auto-input/scan chain —
	// the PR node does not walk their closures.
	if selfScanGeneratedCC && stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string()) &&
		!ridesMainHeader(stmt.StdoutFile.string()) {
		walkOne(stmt.StdoutFile.string())
	}

	// The scanner walks an IN's closure only when its extension maps to an include
	// parser; an unrecognized extension would let the default C parser surface spurious
	// `#include` matches on binary data, so gate IN-walk on hasRegisteredParser.
	for _, f := range stmt.INFiles {
		rel := f.string()

		if includeDirectiveParsers.hasRegisteredParser(rel) {
			walkInput(rel)

			continue
		}

		// An opaque generated IN has no include parser, so its window is never walked,
		// but a node consuming it still lists the producer's full transitive SOURCE
		// closure. Fold both recorded sets: SourceInputs (direct source leaves, incl.
		// unparsed INs) and ProducerSourceClosure (transitive parsed source closure).
		if info := codegenRegForInstance(ctx, instance).lookup(runProgramInputVFS(ctx, instance, d, rel)); info != nil {
			out = append(out, info.SourceInputs...)
			out = append(out, info.ProducerSourceClosure...)
		}
	}

	// A header OUT whose generator tool declares INDUCED_DEPS(h …) carries those
	// induced headers (and their $(S) closure) on its OWN producer node. Walking the
	// header's closure surfaces exactly the induced bucket selected by the output kind.
	// Gated by fullSourceClosure: a header with a cc-source sibling routes induced deps
	// through that sibling, so only a header-only run surfaces them here.
	if fullSourceClosure {
		for _, f := range stmt.OUTFiles {
			if !isHeaderSource(f.string()) {
				continue
			}

			for _, v := range walkClosureTail(ctx.scannerFor(instance), copyFileOutputVFS(instance.Path.rel(), f.string()), scanIn.ScanCfg) {
				if v.isSource() {
					out = append(out, v)
				}
			}
		}
	}

	// OUTPUT_INCLUDES closure realized on the producer. fullSourceClosure (no IN):
	// every OUTPUT_INCLUDES file roots a scan, keeping every $(S) entry (drops the $(B)
	// .pb.h; WKT siblings added below). Otherwise (IN-rooted): a codegen .pb.h
	// contributes only its TRANSITIVE .proto SOURCES (+ WKT sibling); source-tree
	// OUTPUT_INCLUDES are not walked.
	{
		reg := codegenRegForInstance(ctx, instance)

		// keep decides which walked-closure entries ride the producer. A pkPR custom
		// header re-exports its proto imports' .pb.h, so keep its $(S) source closure
		// plus those .pb.h; a pkPB proto header lists only the transitive .proto SOURCES.
		keep := func(v VFS, customPR bool) bool {
			if fullSourceClosure {
				return v.isSource()
			}

			if customPR {
				return v.isSource() || isCodegenProtoHeader(reg, v)
			}

			return strings.HasSuffix(v.rel(), ".proto")
		}

		// Basenames of .pb.h already resolved on this producer; the WKT sibling
		// synthesis below skips a kept .proto whose .pb.h basename is already present.
		pbhSeen := pbhBasenameSet(out)

		for _, oi := range stmt.OutputIncludes {
			target := oi

			if vfsHasPrefix(target.string()) {
				target = internStr(intern(target.string()).rel())
			}

			candidate := build(target.string())

			var sub []VFS
			customPR := false

			switch info := reg.lookup(candidate); {
			case info != nil:
				// Codegen header: strip the intermediate $(B) root, keep its proto/C closure.
				sub = walkClosureTail(ctx.scannerFor(instance), info.OutputPath, scanIn.ScanCfg)
				customPR = info.ProducerKvP == pkPR

				// This run is a CONSUMER of the named codegen output, but walkClosureTail
				// leaves it unresolved so no first-claim is recorded; Node2Module would
				// then attribute the producer NODE to this consumer. Record a node-level
				// claim so the override re-attributes a RUN_PROGRAM-class producer here
				// even when a far peer later resolves an individual sibling output.
				ctx.scannerFor(instance).recordNodeClaim(info.ProducerRef, instance.Path.rel())
			case fullSourceClosure && ctx.fs.isFile(srcRootVFS, target.string()):
				// Source-tree OUTPUT_INCLUDES header: scan its own $(S) closure, keeping
				// the header itself (a real header may be an SCC member).
				sub = walkClosure(ctx.scannerFor(instance), source(target.string()), scanIn.ScanCfg)
			default:
				continue
			}

			for _, v := range sub {
				if !keep(v, customPR) {
					continue
				}

				out = append(out, v)

				if strings.HasSuffix(v.rel(), ".pb.h") {
					pbhSeen[filepath.Base(v.rel())] = true
				}

				// Protobuf WKTs ship a pre-built `.pb.h` checked in alongside the
				// .proto; both are PR inputs when the chain transits through one. For
				// purely-generated .pb.h's the IsFile probe returns false (no-op). The
				// basename guard drops a same-named non-canonical variant.
				if !fullSourceClosure && !hasProtoIN && v.isSource() && strings.HasSuffix(v.rel(), ".proto") {
					sibling := strings.TrimSuffix(v.rel(), ".proto") + ".pb.h"
					sibDir, sibBase := splitDirName(sibling)

					if ctx.fs.isFile(dirKey(sibDir), sibBase) && !pbhSeen[sibBase] {
						out = append(out, source(sibling))
						pbhSeen[sibBase] = true
					}
				}
			}
		}
	}

	if len(out) == 0 {
		return nil
	}

	out = dedupVFS(out, nil)

	return out
}

// prEmitsIncludes builds the parsed-include set registered on one PR output.
// inVFSs mirrors stmt.INFiles in order.
func prEmitsIncludes(outFile STR, stmt *RunProgramStmt, inVFSs []VFS, protoImportPbH []IncludeDirective) []IncludeDirective {
	// OUTPUT_INCLUDES are induced deps on EVERY output, so even a non-carrying data
	// output exposes them. A carrying output (cc/header/inc) additionally models its
	// $(S) IN sources and re-exported proto .pb.h as #includes.
	carries := generatedOutputCarriesIncludes(outFile.string())

	if !carries && len(stmt.OutputIncludes) == 0 {
		return nil
	}

	// A custom header generated from a .proto IN re-exports the import's `.pb.h`. A
	// `.pb.h` OUT is excluded: it roots its proto graph at its own IN .proto.
	carryProtoImportPbH := isHeaderSource(outFile.string()) && !strings.HasSuffix(outFile.string(), ".pb.h")

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
			// A generated output never #includes its $(B) inputs — those are codegen
			// intermediates reached via the producer dep edge. Their $(S) generator
			// sources ride to consumers as this output's ClosureLeaves.
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

		// A TOOL spelled $(B)/dir names the built module `dir`; toolResult expects the
		// source-root module path, so strip the prefix.
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

func runProgramInputVFS(ctx *GenCtx, instance ModuleInstance, d *ModuleData, rel string) VFS {
	switch {
	case strings.HasPrefix(rel, "$(S)/"),
		strings.HasPrefix(rel, "$(B)/"),
		strings.HasPrefix(rel, "${ARCADIA_ROOT}/"),
		strings.HasPrefix(rel, "${CURDIR}/"),
		strings.HasPrefix(rel, "${ARCADIA_BUILD_ROOT}/"),
		strings.HasPrefix(rel, "${BINDIR}/"):
		return copyFileInputVFS(ctx.fs, instance.Path.rel(), rel)
	}

	buildVFS := build(filepath.ToSlash(filepath.Clean(instance.Path.rel() + "/" + rel)))

	if codegenRegForInstance(ctx, instance).lookup(buildVFS) != nil {
		return buildVFS
	}

	if ctx.fs.isFile(srcRootVFS, rel) {
		return source(rel)
	}

	return resolveModuleSourceVFS(ctx, instance, d, rel, d.srcDirs)
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
	emit Emitter,
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

	// IN/OUT path deep-replace candidates: every relative IN/OUT path occurring as a
	// boundary-delimited substring of a positional arg is rewritten to its rooted
	// spelling ($(S)/… inputs, $(B)/… outputs), rooting `--in_file=<rel>` flag-args.
	// STDOUT is not a candidate. Sorted longest→shortest; each arg replaced once.
	cands := deepReplaceCandidates(stmt, inVFSByToken, outVFSByToken)

	for _, aTok := range stmt.Args {
		a := aTok.string()
		key := aTok
		toolReplaced := false

		for _, tool := range auxTools {
			// A rooted TOOL ($(B)/dir) contributes only the dependency; its binary
			// is already spelled literally, and substituting would corrupt it (the
			// token is a prefix of the literal $(B)/dir/binary).
			if tool.rooted {
				continue
			}

			if strings.Contains(a, tool.token) {
				a = strings.ReplaceAll(a, tool.token, tool.bin.string())
				key = internStr(a)
				toolReplaced = true
			}
		}

		// A position already consumed by a TOOL substitution is not re-rooted.
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

	// inVFSs mirrors stmt.INFiles in order.
	for _, v := range inVFSs {
		appendUnique(v)
	}

	// The closure tail is filtered against the head set; filterSeen returns
	// inputClosure itself when nothing collides.
	inputs := na.inputList(head, deduper.filterSeen(inputClosure))

	// The output set is path-keyed: a file declared through more than one output
	// modifier is listed once. Collapse equal VFS in declaration order.
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

	// toolRefs is a fresh local; the node owns it as its foreign (tool) deps. The
	// graph's "deps" array is DepRefs ∪ ForeignDepRefs.
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
		Platform:         instance.Platform,
		Cmds:             na.cmdList(cmd),
		Env:              env,
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               KV{P: pkPR, PC: pcYellow, ShowOut: true},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleTag: moduleTag},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          deps,
		ForeignDepRefs:   foreignDepRefs,
	}

	emit.emitReserved(node, id)
}

// deepReplaceCand is one IN/OUT path candidate: its relative token and the rooted
// spelling it resolves to.
type deepReplaceCand struct {
	token  string
	rooted string
}

// deepReplaceCandidates builds the IN/OUT deep-replace candidate set, longest token
// first, dropping tokens already root-typed.
func deepReplaceCandidates(stmt *RunProgramStmt, inVFSByToken, outVFSByToken map[STR]VFS) []deepReplaceCand {
	cands := make([]deepReplaceCand, 0, len(stmt.INFiles)+len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

	add := func(tok STR, vfs VFS, ok bool) {
		if !ok {
			return
		}

		t := tok.string()

		if !mustDeepReplacePath(t) {
			return
		}

		cands = append(cands, deepReplaceCand{token: t, rooted: vfs.string()})
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

// mustDeepReplacePath reports whether a path needs rooting: only when it is not
// already root-typed or absolute.
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

// deepReplacePathArg rewrites the first (longest) candidate token occurring in arg
// with valid boundaries to its rooted spelling. A boundary is valid when the char
// before/after the match is not part of a path token. Each arg is replaced once.
func deepReplacePathArg(arg string, cands []deepReplaceCand) (string, bool) {
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

// isDeepReplaceBoundary reports whether c may delimit a deep-replaced path token.
func isDeepReplaceBoundary(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return false
	case c == '.', c == '_', c == '-', c == '"', c == '/':
		return false
	}

	return true
}
