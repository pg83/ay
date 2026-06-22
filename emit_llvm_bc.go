package main

import "strings"

// emitLLVMBC emits the upstream LLVM_BC pipeline:
//
//	per source X.cpp:
//	  BC  llvm_compile_cxx   →  $(B)/<unit>/X<suffix>.bc
//	once per stmt:
//	  LD  llvm-link          →  $(B)/<unit>/<NAME>_merged<suffix>.bc
//	  OP  llvm_opt_wrapper   →  $(B)/<unit>/<NAME>_optimized<suffix>.bc
//	  RESOURCE([<optimized.bc>, '/llvm_bc/'+NAME]) — synthesized into d.resources,
//	  picked up by emitResourceObjcopy as a normal embed → PY objcopy_<hash>.o,
//	  which joins the global archive via the existing .global.a pipeline.
func emitLLVMBC(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs, resourceGlobals []ResourceDecl) {
	na := ctx.na

	if len(d.llvmBc) == 0 {
		return
	}

	const (
		clangWrapper = "$(S)/build/scripts/clang_wrapper.py"
		optWrapper   = "$(S)/build/scripts/llvm_opt_wrapper.py"
	)
	python := d.tc.Python3.string()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	reqs := Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)}
	tp := TargetProperties{ModuleDir: instance.Path.rel()}

	for _, stmt := range d.llvmBc {
		clangRoot := resolveResourceGlobalRef(stmt.ClangBCRoot, resourceGlobals)
		clangxx := clangRoot + "/bin/clang++"
		llvmLink := clangRoot + "/bin/llvm-link"
		opt := clangRoot + "/bin/opt"

		// clangWrapperVFS / optWrapperVFS correspond to ${input:"..."} in the
		// upstream compile/opt macros: the script is a direct node input.
		clangWrapperVFS := intern(clangWrapper)
		optWrapperVFS := intern(optWrapper)

		// bcSourceInputs accumulates the $(S)-rooted inputs from every BC node for
		// flat-input propagation to the OP node. Upstream OP carries all BC source
		// closure files directly as node inputs; only $(B) generated files are
		// excluded, since the OP step does not directly open them.
		var bcSourceInputs []VFS

		bcRefs := make([]NodeRef, 0, len(stmt.Sources))
		bcPaths := make([]VFS, 0, len(stmt.Sources))
		// linksCopy records whether any compiled .bc came from a COPY product; if so
		// the merge node inherits the copy producer's fs_tools.py tool (matching the
		// per-source BC node).
		linksCopy := false

		for _, src := range stmt.Sources {
			inputVFS, producer := llvmBcSourceInfo(ctx, instance, src)
			bcOut := build(llvmBcRootRelArcSrc(ctx, instance, src) + stmt.Suffix + ".bc")
			bcArgs := composeBCCompileCmd(python, clangWrapper, clangxx, instance.Platform, in, inputVFS, bcOut)

			// Walk include closure (as for generated CC).
			closure := walkClosure(ctx.scannerFor(instance), inputVFS, in.ScanCfg)

			deps := depRefs(producer)

			if extra := resolveCodegenDepRefs(ctx, instance, closure, deps...); len(extra) > 0 {
				deps = append(deps, extra...)
			}

			// closure is a shared cached slice referenced as its own chunk, never
			// copied; it is the input window (inputVFS included).
			allInputs := na.inputList(na.vfsList(clangWrapperVFS), // ${input:"build/scripts/clang_wrapper.py"}
				closure)

			// Propagate $(S) inputs from this BC node to the OP flat-input set.
			// fs_tools.py among the inputs means this .bc came from a COPY product,
			// so the merge node inherits the tooling too.
			for _, ch := range allInputs {
				for _, v := range ch {
					if v.isSource() {
						bcSourceInputs = append(bcSourceInputs, v)
					}

					if v == copyFsToolsVFS {
						linksCopy = true
					}
				}
			}

			node := &Node{
				Platform:         instance.Platform,
				Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(bcArgs), Env: env}),
				Env:              env,
				Inputs:           allInputs,
				Outputs:          na.vfsList(bcOut),
				KV:               KV{P: pkBC, PC: pcLightGreen},
				TargetProperties: tp,
				Requirements:     reqs,
				Sandboxing:       true,
				DepRefs:          deps,
				Resources:        usesPython3Clang16,
			}
			ref := ctx.emit.emit(node)
			bcRefs = append(bcRefs, ref)
			bcPaths = append(bcPaths, bcOut)
		}

		mergedOut := build(instance.Path.rel() + "/" + stmt.Name + "_merged" + stmt.Suffix + ".bc")
		ldArgs := []STR{internStr(llvmLink)}

		for _, p := range bcPaths {
			ldArgs = append(ldArgs, (p).str())
		}

		ldArgs = append(ldArgs, argDashO.str(), (mergedOut).str())
		// bcPaths is a fresh local; the script-table slice joins as its own
		// chunk — neither is copied.
		mergeInputs := na.inputList(bcPaths)

		if linksCopy {
			mergeInputs = append(mergeInputs, ctx.scripts[copyFsToolsVFS])
		}

		ldNode := &Node{
			Platform:         instance.Platform,
			Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(ldArgs), Env: env}),
			Env:              env,
			Inputs:           mergeInputs,
			Outputs:          na.vfsList(mergedOut),
			KV:               KV{P: pkLD, PC: pcLightRed},
			TargetProperties: tp,
			Requirements:     reqs,
			Sandboxing:       true,
			DepRefs:          append([]NodeRef(nil), bcRefs...),
			Resources:        usesPython3Clang16,
		}
		ldRef := ctx.emit.emit(ldNode)

		optOutName := stmt.Name + "_optimized" + stmt.Suffix + ".bc"
		optOut := build(instance.Path.rel() + "/" + optOutName)
		optArgs := []STR{internStr(python), internStr(optWrapper), internStr(opt), (mergedOut).str(), argDashO.str(), (optOut).str()}
		passes := []string{"default<O2>", "globalopt", "globaldce"}

		if len(stmt.Symbols) > 0 {
			passes = append(passes, "internalize")
			optArgs = append(optArgs, internStr("-internalize-public-api-list="+strings.Join(stmt.Symbols, "#")))
		}

		// ${__COMMA__} expands to literal ','; the outer single-quotes are upstream
		// argument syntax stripped before graph JSON. We emit the expanded form.
		optArgs = append(optArgs, internStr(`-passes="`+strings.Join(passes, ",")+`"`))

		// OP inputs: mergedBC + llvm_opt_wrapper.py + source-root BC closure inputs.
		// Upstream OP carries the full $(S) closure from BC compilation, excluding
		// $(B) generated files which the optimizer doesn't open directly.
		optInputs := make([]VFS, 0, 2+len(bcSourceInputs))
		optInputs = append(optInputs, mergedOut)
		optInputs = append(optInputs, optWrapperVFS) // ${input:"build/scripts/llvm_opt_wrapper.py"}
		// Single flat chunk: dedup interleaves the head with bcSourceInputs (full
		// of per-BC duplicates), so the tail is a fresh slice — none shared/copied.
		optChunks := na.inputList(dedupVFS(optInputs, bcSourceInputs))

		optNode := &Node{
			Platform:         instance.Platform,
			Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(optArgs), Env: env}),
			Env:              env,
			Inputs:           optChunks,
			Outputs:          na.vfsList(optOut),
			KV:               KV{P: pkOP, PC: pcYellow},
			TargetProperties: tp,
			Requirements:     reqs,
			Sandboxing:       true,
			DepRefs:          []NodeRef{ldRef},
			Resources:        usesPython3Clang16,
		}
		opRef := ctx.emit.emit(optNode)

		if stmt.GenerateMachineCode {
			continue
		}

		ensureResourcePeer(instance.Path.rel(), d)

		// Register the optimized .bc as a codegen output so consumers resolve its
		// producer through the registry, not a side map. The .bc carries no
		// #includes, so no parsed includes / generators.
		registerBoundGeneratedParsedOutput(ctx, instance, pkOP, optOut, nil, opRef, nil)

		d.resources = append(d.resources, ResourceEntry{
			Path:      optOutName,
			Key:       "/llvm_bc/" + stmt.Name,
			EndsBatch: true,
		})
	}
}

// composeBCCompileCmd assembles the upstream LLVM_COMPILE_CXX command:
//
//	$PYTHON3 ${input:"build/scripts/clang_wrapper.py"} $WINDOWS
//	  ${CLANG_BC_ROOT}/bin/clang++ ${pre=-I:_C__INCLUDE}
//	  $BC_CXXFLAGS $C_FLAGS_PLATFORM
//	  -Wno-unknown-warning-option $LLVM_OPTS ... -emit-llvm -c Input -o Output
//
// $BC_CXXFLAGS = $CXXFLAGS (same flags as a regular CXX compile). $C_FLAGS_PLATFORM
// = --target=... [ArchArgs] -B/usr/bin, which comes AFTER $BC_CXXFLAGS, not before.
//
// Differences from the CC compile command:
//   - BC starts with python3/wrapper/no/clangBC instead of bare clangCC
//   - --target and -B come AFTER all flags (not early like in CC)
//   - No extra catboostOpenSourceDefine after OwnGlobalBucket (CC always adds one)
//   - No builtinMacroDateTime, macroPrefixMapFlags, or PerSrcCFlags
//   - Ends with -Wno-unknown-warning-option -emit-llvm -c input -o output
func composeBCCompileCmd(python, clangWrapper, clangBC string, platform *Platform, in ModuleCCInputs, inVFS, outVFS VFS) []STR {
	bundle := compileFlagBundleFor(platform)
	warningBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)

	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, platform)
	ownGlobalBucket := composeOwnAndPeerGlobalBucket(in, true /* isCxx */)

	ownExtras := in.CXXFlags

	if len(platform.CXXFlags) > 0 {
		ownExtras = append(append([]ARG{}, ownExtras...), platform.CXXFlags...)
	}

	args := make([]STR, 0, 200+len(in.AddIncl)+len(in.PeerAddInclGlobal)+
		len(bundle.Defines)+len(ownCFlags)+2*len(bundle.NoLibcBlock)+
		len(in.ModuleScopeCFlags)+len(ownExtras)+len(ownGlobalBucket)+
		len(bundle.ArchArgs)+len(bundle.CFlags)+len(warningBundle))

	// Wrapper prefix: python3 clang_wrapper.py no clangBC++
	args = append(args, internStr(python), internStr(clangWrapper), argNo.str(), internStr(clangBC))

	// ${pre=-I:_C__INCLUDE}: include paths (same layout as CC compile)
	args = appendArgStr(args, ccIncludesPrefix)
	args = appendAddIncl(args, in.AddIncl, in.InclArgs)
	peerAddIncl := in.PeerAddInclGlobal

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		args = append(args, in.InclArgs.arg(peerAddIncl[0]))
		peerAddIncl = peerAddIncl[1:]
	}

	args = appendAddIncl(args, peerAddIncl, in.InclArgs)

	// $BC_CXXFLAGS = full CC flag pipeline. Upstream passes the same CXXFLAGS
	// set to BC as to regular CC; the only structural differences are --target/-B
	// ordering and the omissions listed in the function comment above.
	args = appendCompileFlagPipeline(args, bundle, warningBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(platform))

	args = appendCxxStdAndOwn(args, true, in.Flags.NoCompilerWarnings, true, ownExtras)

	// OwnGlobalBucket + catboostOpenSourceDefine: same as CC compile — $BC_CXXFLAGS
	// includes catboost from both the pipeline and the OwnGlobalBucket/PeerCXXFlagsGlobal
	// slot. The explicit define ensures the flag is present even when
	// PeerCXXFlagsGlobal is empty (as composeTargetCC always adds it).
	args = appendArgStr(args, ownGlobalBucket, catboostOpenSourceDefineFor(platform), composePostCatboostBucket(ownGlobalBucket))

	// $C_FLAGS_PLATFORM comes after $BC_CXXFLAGS (not before like in CC).
	args = append(args, platform.TargetArg)
	args = appendArgStr(args, bundle.ArchArgs)
	args = append(args, argDashBBin)

	// BC-specific tail flags from upstream macro
	args = append(args, argWnoUnknownWarningOption.str(), argEmitLlvm.str(), argDashC.str(), (inVFS).str(), argDashO.str(), (outVFS).str())

	return args
}

// llvmBcSourceInfo returns the compile input VFS and optional producer NodeRef
// for a source in an LLVM_BC statement. Resolves the producer through the codegen
// registry — for RUN_PROGRAM / PR / OP outputs and COPY WITH_CONTEXT sources.
func llvmBcSourceInfo(ctx *GenCtx, instance ModuleInstance, src string) (inputVFS VFS, producer NodeRef) {
	reg := codegenRegForInstance(ctx, instance)

	// RUN_PROGRAM / PR / OP generated output (producer ref keyed by output VFS).
	outVFS := copyFileOutputVFS(instance.Path.rel(), src)

	if info := reg.lookup(outVFS); info != nil {
		return outVFS, info.ProducerRef
	}

	// COPY WITH_CONTEXT generated source — build-root copy is authoritative
	if buildVFS := generatedModuleSourceVFS(ctx, instance, src); buildVFS != nil {
		ref := NodeRef(0)

		if info := reg.lookup(*buildVFS); info != nil {
			ref = info.ProducerRef
		}

		return *buildVFS, ref
	}

	return copyFileInputVFS(ctx.fs, instance.Path.rel(), src), NodeRef(0)
}

// llvmBcRootRelArcSrc mirrors upstream's `rootrel_arc_src(src, unit)` quirk. For
// a build-produced source (COPY, RUN_PROGRAM output), resolve_arc_path fails to
// map back to $S/<...>, so the bare src is returned — yielding bc_path =
// $(B)/<src><suffix>.bc at $(B) root (no module prefix). For a genuine source-tree
// file, it returns $S/<module>/<src> and strips $S/ → module-rel path.
func llvmBcRootRelArcSrc(ctx *GenCtx, instance ModuleInstance, src string) string {
	if reg := codegenRegForInstance(ctx, instance); reg.lookup(copyFileOutputVFS(instance.Path.rel(), src)) != nil {
		return src
	}

	if generatedModuleSourceVFS(ctx, instance, src) != nil {
		return src
	}

	if sourceInputVFS(ctx.fs, instance.Path.rel(), src) != nil {
		return instance.Path.rel() + "/" + src
	}

	return src
}
