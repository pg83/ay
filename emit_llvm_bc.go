package main

import "strings"

// emitLLVMBC emits the upstream LLVM_BC pipeline (build/plugins/llvm_bc.py):
//
//	per source X.cpp:
//	  BC  llvm_compile_cxx   →  $(B)/<unit>/X<suffix>.bc
//	once per stmt:
//	  LD  llvm-link          →  $(B)/<unit>/<NAME>_merged<suffix>.bc
//	  OP  llvm_opt_wrapper   →  $(B)/<unit>/<NAME>_optimized<suffix>.bc
//	  RESOURCE([<optimized.bc>, '/llvm_bc/'+NAME]) — synthesized into
//	  d.resources, picked up by emitResourceObjcopy as a normal embed → emits
//	  the PY objcopy_<hash>.o, which then participates in the global archive
//	  (lib<...>.global.a) via the existing LIBRARY .global.a pipeline.
func emitLLVMBC(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs, resourceGlobals []resourceDecl) {
	if len(d.llvmBc) == 0 {
		return
	}

	const (
		clangWrapper = "$(S)/build/scripts/clang_wrapper.py"
		optWrapper   = "$(S)/build/scripts/llvm_opt_wrapper.py"
	)
	python := d.tc.Python3.String()
	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}
	reqs := Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)}
	tp := TargetProperties{ModuleDir: instance.Path}

	for _, stmt := range d.llvmBc {
		clangRoot := resolveResourceGlobalRef(stmt.ClangBCRoot, resourceGlobals)
		clangxx := clangRoot + "/bin/clang++"
		llvmLink := clangRoot + "/bin/llvm-link"
		opt := clangRoot + "/bin/opt"

		// clangWrapperVFS / optWrapperVFS correspond to ${input:"..."} in the
		// upstream LLVM_COMPILE_CXX / LLVM_OPT macros: ymake adds the script as
		// a direct node input alongside the compile/opt command.
		clangWrapperVFS := Intern(clangWrapper)
		optWrapperVFS := Intern(optWrapper)

		// bcSourceInputs accumulates the $(S)-rooted inputs from every BC node for
		// flat-input propagation to the OP node. Upstream OP carries all source
		// closure files from the BC compilation steps directly as node inputs
		// (ymake's flat input model); only $(B) generated files are excluded since
		// the OP step does not directly open them.
		var bcSourceInputs []VFS

		bcRefs := make([]NodeRef, 0, len(stmt.Sources))
		bcPaths := make([]VFS, 0, len(stmt.Sources))
		// linksCopy records whether any compiled .bc came from a COPY product; if so
		// the merge node inherits the copy producer's fs_tools.py tool (matching the
		// per-source BC node, which picks it up via wcExtras).
		linksCopy := false

		for _, src := range stmt.Sources {
			inputVFS, producer := llvmBcSourceInfo(ctx, instance, d, src)
			bcOut := Build(llvmBcRootRelArcSrc(ctx, instance, d, src) + stmt.Suffix + ".bc")
			bcArgs := composeBCCompileCmd(python, clangWrapper, clangxx, instance.Platform, in, inputVFS, bcOut)

			// Walk include closure (same as emitCodegenDownstreamCC for generated CC).
			closure := walkClosure(ctx, instance, inputVFS, in)

			var depRefs []NodeRef

			if producer != (NodeRef(0)) {
				depRefs = []NodeRef{producer}
			}

			if extra := resolveCodegenDepRefs(ctx, instance, closure, depRefs...); len(extra) > 0 {
				depRefs = append(depRefs, extra...)
			}

			allInputs := make([]VFS, 0, 2+len(closure))
			allInputs = append(allInputs, inputVFS)
			allInputs = append(allInputs, clangWrapperVFS) // ${input:"build/scripts/clang_wrapper.py"}
			allInputs = append(allInputs, closure...)

			// Propagate $(S) inputs from this BC node to the OP flat-input set.
			// fs_tools.py in the inputs (via a consumed TEXT header's leaf, or via
			// wcExtras when inputVFS is itself a copy product) means this .bc came
			// from a COPY product, so the merge node inherits the tooling too.
			for _, v := range allInputs {
				if v.IsSource() {
					bcSourceInputs = append(bcSourceInputs, v)
				}

				if v == copyFsToolsVFS {
					linksCopy = true
				}
			}

			node := &Node{
				Platform:         instance.Platform,
				Cmds:             []Cmd{{CmdArgs: bcArgs, Env: env}},
				Env:              env,
				Inputs:           allInputs,
				Outputs:          []VFS{bcOut},
				KV:               KV{P: pkBC, PC: pcLightGreen},
				TargetProperties: tp,
				Requirements:     reqs,
				Sandboxing:       true,
				DepRefs:          depRefs,
			}
			ref := ctx.emit.Emit(withResources(node, resourcePatternYMakePython3, resourcePatternClang16))
			bcRefs = append(bcRefs, ref)
			bcPaths = append(bcPaths, bcOut)
		}

		mergedOut := Build(instance.Path + "/" + stmt.Name + "_merged" + stmt.Suffix + ".bc")
		ldArgs := []STR{internStr(llvmLink)}

		for _, p := range bcPaths {
			ldArgs = append(ldArgs, (p).str())
		}

		ldArgs = append(ldArgs, argDashO.str(), (mergedOut).str())
		mergeInputs := append([]VFS(nil), bcPaths...)

		if linksCopy {
			mergeInputs = append(mergeInputs, ctx.scripts[copyFsToolsVFS]...)
		}

		ldNode := &Node{
			Platform:         instance.Platform,
			Cmds:             []Cmd{{CmdArgs: ldArgs, Env: env}},
			Env:              env,
			Inputs:           mergeInputs,
			Outputs:          []VFS{mergedOut},
			KV:               KV{P: pkLD, PC: pcLightRed},
			TargetProperties: tp,
			Requirements:     reqs,
			Sandboxing:       true,
			DepRefs:          append([]NodeRef(nil), bcRefs...),
		}
		ldRef := ctx.emit.Emit(withResources(ldNode, resourcePatternYMakePython3, resourcePatternClang16))

		optOutName := stmt.Name + "_optimized" + stmt.Suffix + ".bc"
		optOut := Build(instance.Path + "/" + optOutName)
		optArgs := []STR{internStr(python), internStr(optWrapper), internStr(opt), (mergedOut).str(), argDashO.str(), (optOut).str()}
		passes := []string{"default<O2>", "globalopt", "globaldce"}

		if len(stmt.Symbols) > 0 {
			passes = append(passes, "internalize")
			optArgs = append(optArgs, internStr("-internalize-public-api-list="+strings.Join(stmt.Symbols, "#")))
		}

		// ${__COMMA__} is a ymake macro that expands to literal ','; the outer
		// single-quotes in the Python plugin are ymake argument syntax stripped
		// before graph JSON is written. We emit the already-expanded form directly.
		optArgs = append(optArgs, internStr(`-passes="`+strings.Join(passes, ",")+`"`))

		// OP inputs: mergedBC + llvm_opt_wrapper.py + source-root BC closure inputs.
		// Upstream OP carries the full $(S) closure from BC compilation (flat input
		// model): excludes $(B) generated files (build-root source copy, proto
		// headers, generated includes) which the optimizer doesn't open directly.
		optInputs := make([]VFS, 0, 2+len(bcSourceInputs))
		optInputs = append(optInputs, mergedOut)
		optInputs = append(optInputs, optWrapperVFS) // ${input:"build/scripts/llvm_opt_wrapper.py"}
		optInputs = dedupVFS(optInputs, bcSourceInputs)

		optNode := &Node{
			Platform:         instance.Platform,
			Cmds:             []Cmd{{CmdArgs: optArgs, Env: env}},
			Env:              env,
			Inputs:           optInputs,
			Outputs:          []VFS{optOut},
			KV:               KV{P: pkOP, PC: pcYellow},
			TargetProperties: tp,
			Requirements:     reqs,
			Sandboxing:       true,
			DepRefs:          []NodeRef{ldRef},
		}
		opRef := ctx.emit.Emit(withResources(optNode, resourcePatternYMakePython3, resourcePatternClang16))

		if stmt.GenerateMachineCode {
			continue
		}

		ensureResourcePeer(instance.Path, d)

		if d.prOutputProducer == nil {
			d.prOutputProducer = map[string]NodeRef{}
		}

		d.prOutputProducer[optOutName] = opRef

		// Propagate the OP node's inputs into prOutputInputs so that
		// emitResourceObjcopy's prResourceExtraInputs picks up the full BC
		// compilation closure (clang_wrapper.py, llvm_opt_wrapper.py, and all
		// header dependencies) and adds them as inputs to the PY objcopy node.
		// Upstream ymake propagates producer inputs transitively via its
		// ${input:...} resolution; our code uses the prOutputInputs map for this.
		if d.prOutputInputs == nil {
			d.prOutputInputs = map[string][]VFS{}
		}

		d.prOutputInputs[optOutName] = optInputs // read-only consumers (node inputs + prResourceExtraInputs copies out)
		d.resources = append(d.resources, resourceEntry{
			Path:      optOutName,
			Key:       "/llvm_bc/" + stmt.Name,
			EndsBatch: true,
		})
	}
}

// composeBCCompileCmd assembles the upstream LLVM_COMPILE_CXX command
// (build/ymake.core.conf macro):
//
//	$YMAKE_PYTHON3 ${input:"build/scripts/clang_wrapper.py"} $WINDOWS
//	  ${CLANG_BC_ROOT}/bin/clang++ ${pre=-I:_C__INCLUDE}
//	  $BC_CXXFLAGS $C_FLAGS_PLATFORM
//	  -Wno-unknown-warning-option $LLVM_OPTS ... -emit-llvm -c Input -o Output
//
// $BC_CXXFLAGS = $CXXFLAGS (same flags as a regular CXX compile: includes all
// of debugPrefixMap, xclangDebug, bundle.CFlags, warningBundle, defines, etc.).
// $C_FLAGS_PLATFORM = --target=... [ArchArgs] -B/usr/bin (comes AFTER $BC_CXXFLAGS, not before).
//
// Differences from CC compile command:
//   - BC starts with python3/wrapper/no/clangBC instead of bare clangCC
//   - --target and -B come AFTER all flags (not early like in CC)
//   - No extra catboostOpenSourceDefine after OwnGlobalBucket (CC always adds one)
//   - No builtinMacroDateTime
//   - No macroPrefixMapFlags
//   - No PerSrcCFlags
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

	// $BC_CXXFLAGS = full CC flag pipeline (same as appendCompileFlagPipeline).
	// The upstream macro passes the same CXXFLAGS set to the BC compile as to
	// regular CC; the only structural differences are ordering of --target/-B
	// and the omissions listed in the function comment above.
	args = appendCompileFlagPipeline(args, bundle, warningBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags)

	args = appendCxxStdAndOwn(args, true, in.Flags.NoCompilerWarnings, true, ownExtras)

	// OwnGlobalBucket + catboostOpenSourceDefine: same as CC compile — the
	// upstream $BC_CXXFLAGS includes catboost from both the pipeline (inside
	// appendCompileFlagPipeline) and from the OwnGlobalBucket/PeerCXXFlagsGlobal
	// slot. The explicit catboostOpenSourceDefine ensures the flag is present even
	// when PeerCXXFlagsGlobal is empty (same reason composeTargetCC always adds it).
	args = appendArgStr(args, ownGlobalBucket, catboostOpenSourceDefine, composePostCatboostBucket(ownGlobalBucket))

	// $C_FLAGS_PLATFORM comes after $BC_CXXFLAGS (not before like in CC).
	args = append(args, platform.TargetArg)
	args = appendArgStr(args, bundle.ArchArgs)
	args = append(args, argDashBBin)

	// BC-specific tail flags from upstream macro
	args = append(args, argWnoUnknownWarningOption.str(), argEmitLlvm.str(), argDashC.str(), (inVFS).str(), argDashO.str(), (outVFS).str())

	return args
}

// llvmBcSourceInfo returns the compile input VFS and optional producer NodeRef
// for a given source in an LLVM_BC statement. Checks both the module's
// prOutputProducer map (for RUN_PROGRAM / PR outputs) and the codegen registry
// (for COPY WITH_CONTEXT generated sources like yt_codec_bc.cpp).
func llvmBcSourceInfo(ctx *genCtx, instance ModuleInstance, d *moduleData, src string) (inputVFS VFS, producer NodeRef) {
	// RUN_PROGRAM / PR generated output
	if ref := d.prOutputProducer[src]; ref != (NodeRef(0)) {
		return copyFileOutputVFS(instance.Path, src), ref
	}

	// COPY WITH_CONTEXT generated source — build-root copy is authoritative
	if buildVFS := generatedModuleSourceVFS(ctx, instance, src); buildVFS != nil {
		ref := NodeRef(0)

		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			if info := reg.Lookup(*buildVFS); info != nil && info.HasProducerRef {
				ref = info.ProducerRef
			}
		}

		return *buildVFS, ref
	}

	return copyFileInputVFS(ctx.fs, instance.Path, src), NodeRef(0)
}

// llvmBcRootRelArcSrc mirrors upstream's `rootrel_arc_src(src, unit)` quirk
// (yatool/build/plugins/_common.py). For a build-produced source (COPY,
// RUN_PROGRAM output), `resolve_arc_path` fails to map back to $S/<...>, so
// rootrel_arc_src returns the bare src as-is — yielding `bc_path =
// $(B)/<src><suffix>.bc` at $(B) root (no module prefix). For a genuine
// source-tree file, resolve_arc_path returns $S/<module>/<src> and
// rootrel_arc_src strips $S/ → module-rel path. yt_codec_bc.cpp is the
// canonical build-rooted case (sg5: $(B)/yt_codec_bc.cpp.16.bc).
func llvmBcRootRelArcSrc(ctx *genCtx, instance ModuleInstance, d *moduleData, src string) string {
	if _, ok := d.prOutputProducer[src]; ok {
		return src
	}

	if generatedModuleSourceVFS(ctx, instance, src) != nil {
		return src
	}

	if sourceInputVFS(ctx.fs, instance.Path, src) != nil {
		return instance.Path + "/" + src
	}

	return src
}
