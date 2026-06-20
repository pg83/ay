package main

import (
	"path/filepath"
	"strings"
)

type RunProgramsForARResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

type RunProgramAuxTool struct {
	token string
	ref   NodeRef
	bin   VFS
	// rooted marks a TOOL whose path already carries a build/source root prefix
	// ($(B)/… from ${ARCADIA_BUILD_ROOT}/…). Upstream's `${tool:TOOL}` is hidden
	// and only registers the dependency; the command spells the binary path
	// literally (e.g. ${ARCADIA_BUILD_ROOT}/dir/binary), so no arg substitution
	// applies. A relative TOOL (contrib/tools/protoc/plugins/cpp_styleguide) is
	// the substituting case — ymake rewrites the matching relative arg token to
	// the resolved binary.
	rooted bool
}

func emitRunProgramsForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) *RunProgramsForARResult {
	if len(d.runPrograms) == 0 {
		return nil
	}

	reg := codegenRegForInstance(ctx, instance)
	res := &RunProgramsForARResult{}

	for _, rp := range d.runPrograms {
		prRef := emitRunProgram(ctx, instance, rp, d, reg, in)

		outs := make([]string, 0, len(rp.OUTFiles)+len(rp.OUTNoAutoFiles)+1)
		outs = append(outs, strStrings(rp.OUTFiles)...)

		// Only auto STDOUT is a module source; STDOUT_NOAUTO carries upstream's
		// `noauto` modifier and is excluded, exactly like OUT_NOAUTO.
		if rp.StdoutFile != nil && !rp.StdoutNoAuto {
			outs = append(outs, rp.StdoutFile.string())
		}

		for _, out := range outs {
			switch {
			case isCCSourceExt(out):
				ccRef, ccOut := emitPRDownstreamCC(ctx, instance, out, prRef, in)
				res.CCRefs = append(res.CCRefs, ccRef)
				res.CCOutputs = append(res.CCOutputs, ccOut)
			case isAsmSourceExt(out):
				asRef, asOut := emitCodegenDownstreamAS(ctx, instance, out, []NodeRef{prRef}, in)
				res.CCRefs = append(res.CCRefs, asRef)
				res.CCOutputs = append(res.CCOutputs, asOut)
			}
		}
	}

	return res
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

	// The run's MAIN output (ymake FindMainElemOrDefault(GetOutput(), 0)): the
	// first OUT in command order — OUT, then OUT_NOAUTO, then STDOUT. The command
	// builds one node keyed on it; the other outputs are EDT_OutTogether siblings.
	var mainOutputVFS VFS
	switch {
	case len(stmt.OUTFiles) > 0:
		mainOutputVFS = outVFSByToken[stmt.OUTFiles[0]]
	case len(stmt.OUTNoAutoFiles) > 0:
		mainOutputVFS = outVFSByToken[stmt.OUTNoAutoFiles[0]]
	case stdoutVFS != nil:
		mainOutputVFS = *stdoutVFS
	}

	// The run's $(S) source inputs are real inputs of any unit that transitively
	// consumes a generated output (directly, or after the output is archived into
	// an .inc that a CC unit #includes). Record them on each output so the archive
	// emit can propagate them as closure leaves (see emitArchive).
	//
	// A $(B) input is itself a codegen intermediate (e.g. a RUN_ANTLR-generated
	// .proto). Its own $(S) generator sources (grammar/template/jar/scripts) are
	// real inputs of every consumer of this run's outputs too, so fold them in:
	// SourceInputs is transitive through the producer chain. prGeneratedFromSources
	// (the $(B)-derived subset) additionally rides as a non-expanded ClosureLeaf of
	// each output, so walkClosure carries it to consumers — the vehicle
	// emit_proto.go uses, replacing the old fake `#include "X.proto"` bridge.
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

	// A run "self-consumes" when its own module auto-compiles a cc-source or
	// asm-source OUT (the exact set emitRunProgramsForAR builds downstream: OUT
	// files and auto STDOUT, never OUT_NOAUTO). Such a producer is the first
	// DFS-leaver of every output of the run (upstream post-order processes the
	// producing peer — compiling the sibling and leaving the OutTogether main
	// output with it — before any external consumer). So its outputs keep the
	// producer's module_dir; the consumer-claim override must not move them.
	// A header-only / OUT_NOAUTO run (LLVM *.inc) does not self-consume and keeps
	// the existing first-consumer attribution.
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

	// Reserve the PR producer's ref before registering its outputs: the input
	// closure walk below resolves sibling codegen deps that may include these
	// outputs, and registration records the producer ref.
	prRef := ctx.emit.reserve()

	// A RUN_PROGRAM may name the same file in more than one output role —
	// e.g. STDOUT and OUT_NOAUTO pointing at the same generated artifact (the
	// program's stdout *is* that declared output). They denote one physical
	// output, so register each distinct output VFS exactly once; a second
	// registration would trip the codegen registry's duplicate-producer guard.
	registeredPROut := map[VFS]bool{}

	registerPROutput := func(out VFS, parsed []IncludeDirective) {
		if registeredPROut[out] {
			return
		}

		registeredPROut[out] = true

		registerBoundGeneratedParsedOutput(ctx, instance, pkPR, out, parsed, prRef, []NodeRef{toolLDRef})
		reg.setSourceInputs(out, prSourceInputs)

		// A self-consuming run owns its outputs: record the producer module dir
		// now (before any consumer can resolve this output) so the override keeps
		// the node attributed to its producer. registerBoundGeneratedParsedOutput
		// just created the registry entry resolution depends on, so this write is
		// strictly the first claim.
		if selfConsumes {
			ctx.scannerFor(instance).markGeneratedProducerOwned(out, instance.Path.rel())
		}

		// A non-main output is an EDT_OutTogether sibling of the main output: ymake's
		// json_visitor PrepareLeaving rides the main output onto any node depending on
		// a non-main sibling. Ride it as a non-expanded closure leaf of the sibling, so
		// the scanner splices the main output into every window containing the sibling
		// — the root same-run consumer (caesar features.gen.h sibling of features.gen.cpp)
		// AND any transitive consumer whose include closure reaches the sibling through
		// a different module (the with_transitive_headers/advm_banner.pb.h wrapper class,
		// where advm_banner is the first OUT and no source #includes it directly). The
		// leaf never rides onto the PR producer itself (dropOwnOutputs strips it from the
		// producer's own input closure).
		if out != mainOutputVFS {
			reg.addClosureLeaf(out, mainOutputVFS)
		}

		for _, s := range prGeneratedFromSources {
			reg.addClosureLeaf(out, s)
		}
	}

	for _, f := range stmt.OUTFiles {
		registerPROutput(outVFSByToken[f], prEmitsIncludes(f, stmt, inVFSs))
	}

	for _, f := range stmt.OUTNoAutoFiles {
		registerPROutput(outVFSByToken[f], prEmitsIncludes(f, stmt, inVFSs))
	}

	if stmt.StdoutFile != nil {
		registerPROutput(*stdoutVFS, prEmitsIncludes(*stmt.StdoutFile, stmt, inVFSs))
	}

	inputClosure := prInputClosure(ctx, instance, d, stmt, moduleInputs)

	// A command never inputs its own outputs. prInputClosure walks this run's
	// cc-source OUTs (to surface OUTPUT_INCLUDES from `.in` templates); those
	// windows now carry the OutTogether main-output leaf (registerPROutput), which
	// must ride onto CONSUMERS, not back onto the producer. Drop any own-output VFS
	// the self-walk pulled in.
	inputClosure = dropOwnOutputs(inputClosure, outVFSByToken)

	// A build-rooted IN that is a registered codegen output but carries no include
	// parser (e.g. a FROM_SANDBOX OUT_NOAUTO fetch artifact) never enters
	// inputClosure, yet the PR still depends on its producer. Resolve producer deps
	// over the IN set as well as the walked closure; resolveCodegenDepRefs' build
	// gate + registry probe + dedup make the IN files a no-op for the common case (a
	// parsed IN is already in inputClosure, a source IN is skipped).
	depInputs := inputClosure
	if len(inVFSs) > 0 {
		depInputs = append(append(make([]VFS, 0, len(inVFSs)+len(inputClosure)), inVFSs...), inputClosure...)
	}

	// Exclude prRef as well as the tool: the outputs are now registered against
	// prRef, so a PR output appearing in another output's closure must not become a
	// self-dependency (the old two-phase code bound the ref only after this resolve).
	prExtraDepRefs := resolveCodegenDepRefs(ctx, instance, depInputs, toolLDRef, prRef)

	emitPR(instance, stmt, toolBinPath, toolLDRef, auxTools, inVFSByToken, inVFSs, outVFSByToken, stdoutVFS, inputClosure, prExtraDepRefs, prRef, ctx.emit)

	return prRef
}

// dropOwnOutputs removes any of this run's declared output VFSs from its own
// input closure. A producer never depends on a file it produces; the self-walk
// of cc-source OUTs can otherwise pull a sibling output in via the OutTogether
// closure leaf. Returns closure unchanged when nothing collides.
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
	// OUTPUT_INCLUDES are upstream's `${hide;output_include:OUTPUT_INCLUDES}`:
	// induced deps recorded ON the OUT files (per the macro doc, "includes of the
	// output files needed to build them"). They are realized as node inputs at the
	// point an OUT enters an include scan:
	//   - an auto cc-source OUT is C-scanned by its downstream CC, so the closure
	//     surfaces on that CONSUMER (caesar features.gen.cpp); a no-IN run that
	//     emits such an OUT lists only the tool on the producer.
	//   - a header-only OUT (no cc-source OUT, no cc-source STDOUT) is never itself
	//     compiled, so with no IN to root the graph the OUTPUT_INCLUDES closure has
	//     nowhere else to surface and rides the PRODUCER command node — the full
	//     $(S) include closure of every OUTPUT_INCLUDES file (plutonium dsp.yaff.h).
	// With an IN file the producer's graph is rooted at IN regardless (control_board
	// .{h,cpp}.in #include the proto headers their OUTPUT_INCLUDES name).
	hasAutoCCSourceOut := stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string())
	for _, f := range stmt.OUTFiles {
		if isCCSourceExt(f.string()) {
			hasAutoCCSourceOut = true
			break
		}
	}

	headerOnlyNoIN := len(stmt.INFiles) == 0 && !hasAutoCCSourceOut

	if len(stmt.INFiles) == 0 && !headerOnlyNoIN {
		return nil
	}

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

	for _, f := range stmt.OUTFiles {
		if !isCCSourceExt(f.string()) {
			continue
		}

		walkOne(f.string())
	}

	// OUT_NOAUTO outputs use upstream's `${hide;noauto;output:OUT_NOAUTO}`
	// modifier: registered as outputs but explicitly EXCLUDED from the
	// auto-input/scan chain — the PR node does not walk their closures.
	// (yql/.../v1_proto_split_antlr4 uses OUT_NOAUTO for .pb.h/.pb.cc, and
	// upstream tracks only IN + tools as PR inputs; walking the .pb.cc here
	// over-emits 1253 libcxx/protobuf headers via the parsed pb.h chain.)
	if stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string()) {
		walkOne(stmt.StdoutFile.string())
	}

	// Upstream's RUN_PROGRAM macro registers every IN as an input with
	// scan-on-include (`${hide;input:IN}`); the scanner walks each IN's
	// parsed-include closure when the file's extension is explicitly mapped
	// to an include parser (cpp/h/.h.in/etc.). Files outside that map —
	// Jinja templates (.jnj), JSON, libmagic Magdir entries without an
	// extension — must not be parsed: our default parser is the C parser,
	// and it would surface spurious `#include "/mach-o/fat.h"` matches on
	// random binary data. Gate IN-walk on hasRegisteredParser so unknown
	// extensions contribute zero closure entries (matches REF's
	// yql_*_expr_nodes.gen.h PR nodes, which list only the tool + IN
	// files).
	for _, f := range stmt.INFiles {
		if rel := f.string(); includeDirectiveParsers.hasRegisteredParser(rel) {
			walkInput(rel)
		}
	}

	// OUTPUT_INCLUDES closure realized on the producer.
	//
	// headerOnlyNoIN (plutonium dsp.yaff.h): the run has no IN and emits only
	// header OUTs, so every OUTPUT_INCLUDES file roots a scan here — codegen .pb.h
	// via the registry's OutputPath, source-tree headers (library/cpp/yaff/*.h)
	// via their source path. Keep every $(S) entry of the closure (drops the
	// intermediate $(B) generated .pb.h; the proto-import graph already surfaces
	// the $(S) .proto sources, and source-tree WKT .pb.h siblings are added below).
	//
	// Otherwise (run rooted at IN): the producer's C graph is rooted at IN, which
	// already carries the protobuf/libcxx closure, so a codegen .pb.h
	// OUTPUT_INCLUDES contributes only its TRANSITIVE .proto SOURCES (+ WKT .pb.h
	// sibling); source-tree OUTPUT_INCLUDES are not walked here (they would
	// redundantly drag libcxx).
	{
		reg := codegenRegForInstance(ctx, instance)

		keep := func(v VFS) bool {
			if headerOnlyNoIN {
				return v.isSource()
			}
			return strings.HasSuffix(v.rel(), ".proto")
		}

		for _, oi := range stmt.OutputIncludes {
			target := oi

			if vfsHasPrefix(target.string()) {
				target = internStr(intern(target.string()).rel())
			}

			candidate := build(target.string())

			var sub []VFS
			switch info := reg.lookup(candidate); {
			case info != nil:
				// Codegen .pb.h: a build output that always leads its window —
				// strip the intermediate $(B) root, keep its proto/C closure.
				sub = walkClosureTail(ctx.scannerFor(instance), info.OutputPath, scanIn.ScanCfg)
			case headerOnlyNoIN && ctx.fs.isFile(srcRootVFS, target.string()):
				// Source-tree OUTPUT_INCLUDES header: scan its own $(S) closure,
				// keeping the header itself (a real header may be an SCC member,
				// so walkClosureTail is unsound here).
				sub = walkClosure(ctx.scannerFor(instance), source(target.string()), scanIn.ScanCfg)
			default:
				continue
			}

			for _, v := range sub {
				if !keep(v) {
					continue
				}

				out = append(out, v)

				// Protobuf WKTs (google/protobuf/{any,duration,empty,struct,
				// timestamp,...}.proto) ship pre-built `.pb.h` headers checked
				// in alongside the .proto. Upstream lists both the .proto and
				// the pre-built .pb.h as PR inputs when the chain transits
				// through one. For purely-generated .pb.h's (no source-tree
				// .pb.h sibling) the IsFile probe returns false, so this is a
				// no-op outside the WKT path. headerOnlyNoIN keeps the whole C
				// closure, so a genuinely-#included WKT .pb.h already rides as a
				// source — re-adding the sibling of every .proto would over-emit
				// the .pb.h of a variant (protobuf_old) that is never #included.
				if !headerOnlyNoIN && v.isSource() && strings.HasSuffix(v.rel(), ".proto") {
					sibling := strings.TrimSuffix(v.rel(), ".proto") + ".pb.h"
					sibDir, sibBase := splitDirName(sibling)

					if ctx.fs.isFile(dirKey(sibDir), sibBase) {
						out = append(out, source(sibling))
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
// inVFSs mirrors stmt.INFiles in order (computed once by emitRunProgram), so the
// per-output call needn't re-resolve every IN file.
func prEmitsIncludes(outFile STR, stmt *RunProgramStmt, inVFSs []VFS) []IncludeDirective {
	if !generatedOutputCarriesIncludes(outFile.string()) {
		return nil
	}

	includes := make([]IncludeDirective, 0, len(inVFSs)+len(stmt.OutputIncludes))

	for _, v := range inVFSs {
		// A generated output never #includes its $(B) inputs — those are codegen
		// intermediates (e.g. a RUN_ANTLR-generated .proto) reached via the
		// producer dep edge, not C++ includes. Their $(S) generator sources ride
		// to consumers as this output's ClosureLeaves (see emitRunProgram),
		// matching emit_proto.go; fake-including the intermediate here dragged the
		// $(B) file into every consumer's closure.
		if v.isBuild() {
			continue
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
	}

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = internStr(v.rel())
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: f})
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

		// A TOOL spelled ${ARCADIA_BUILD_ROOT}/dir (expanded to $(B)/dir) names the
		// built module `dir`; the root prefix only marks it as an output reference.
		// toolResult expects the source-root module path, so strip the prefix.
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

	for _, aTok := range stmt.Args {
		a := aTok.string()
		key := aTok

		for _, tool := range auxTools {
			// A rooted TOOL ($(B)/dir) contributes only the dependency; its binary
			// path is already spelled literally in the args, so substituting would
			// corrupt it (token $(B)/dir is a prefix of the literal $(B)/dir/binary).
			if tool.rooted {
				continue
			}

			if strings.Contains(a, tool.token) {
				a = strings.ReplaceAll(a, tool.token, tool.bin.string())
				key = internStr(a)
			}
		}

		if !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			if vfs, ok := inVFSByToken[key]; ok {
				cmdArgs = append(cmdArgs, vfs.str())

				continue
			}

			if vfs, ok := outVFSByToken[key]; ok {
				cmdArgs = append(cmdArgs, vfs.str())

				continue
			}

			// A path with a trailing modifier (e.g. CFFI's `build.py:ffi`): the head
			// before ':' is the relative path declared in IN/OUT, which "becomes
			// absolute" per RUN_PROGRAM semantics; the modifier rides along.
			if i := strings.IndexByte(a, ':'); i > 0 {
				head := internStr(a[:i])

				if vfs, ok := inVFSByToken[head]; ok {
					cmdArgs = append(cmdArgs, internStr(vfs.string()+a[i:]))

					continue
				}

				if vfs, ok := outVFSByToken[head]; ok {
					cmdArgs = append(cmdArgs, internStr(vfs.string()+a[i:]))

					continue
				}
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

	// inVFSs mirrors stmt.INFiles in order — same values the token map holds,
	// without re-probing it.
	for _, v := range inVFSs {
		appendUnique(v)
	}

	// The closure tail is filtered against the head set; filterSeen returns
	// inputClosure itself when nothing collides, so the closure is referenced,
	// not copied, into the chunk list.
	inputs := na.inputList(head, deduper.filterSeen(inputClosure))

	// Upstream's output set is path-keyed: a file declared through more than one
	// output modifier (e.g. STDOUT and OUT_NOAUTO naming the same artifact — the
	// program's stdout *is* the declared output) is listed once. Collapse equal
	// VFS in declaration order, mirroring the registeredPROut dedup above.
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

	// toolRefs is a fresh local, not mutated after this; the node owns it as its
	// foreign (tool) deps. The graph's "deps" array is DepRefs ∪ ForeignDepRefs
	// (Node.buildDeps), so the tools are not duplicated here.
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
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          deps,
		ForeignDepRefs:   foreignDepRefs,
	}

	emit.emitReserved(node, id)
}
