package main

import "strings"

// emitLLVMBC emits the upstream LLVM_BC pipeline (build/plugins/llvm_bc.py):
//   per source X.cpp:
//     BC  llvm_compile_cxx   →  $(B)/<unit>/X<suffix>.bc
//   once per stmt:
//     LD  llvm-link          →  $(B)/<unit>/<NAME>_merged<suffix>.bc
//     OP  llvm_opt_wrapper   →  $(B)/<unit>/<NAME>_optimized<suffix>.bc
//     RESOURCE([<optimized.bc>, '/llvm_bc/'+NAME]) — synthesized into
//     d.resources, picked up by emitResourceObjcopy as a normal embed → emits
//     the PY objcopy_<hash>.o, which then participates in the global archive
//     (lib<...>.global.a) via the existing LIBRARY .global.a pipeline.
//
// Cmd contents are intentionally lean: sg5 parity is by output-set, not
// byte-exact self_uid (which is covered by sg2/sg2_x86_64/sg3/sg4 gating).
func emitLLVMBC(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.llvmBc) == 0 {
		return
	}

	const (
		clangWrapper = "$(S)/build/scripts/clang_wrapper.py"
		optWrapper   = "$(S)/build/scripts/llvm_opt_wrapper.py"
	)
	python := ctx.host.Tools.Python3
	env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}
	reqs := map[string]interface{}{
		"cpu":     float64(1),
		"network": "restricted",
		"ram":     float64(32),
	}
	tp := map[string]string{"module_dir": instance.Path}

	for _, stmt := range d.llvmBc {
		clangRoot := stripResourceName(stmt.ClangBCRoot)
		clangxx := clangRoot + "/bin/clang++"
		llvmLink := clangRoot + "/bin/llvm-link"
		opt := clangRoot + "/bin/opt"

		bcRefs := make([]NodeRef, 0, len(stmt.Sources))
		bcPaths := make([]VFS, 0, len(stmt.Sources))
		for _, src := range stmt.Sources {
			inputVFS := llvmBcInputVFS(ctx, instance, d, src)
			bcOut := Build(llvmBcRootRelArcSrc(ctx, instance, d, src) + stmt.Suffix + ".bc")
			bcArgs := []string{
				python, clangWrapper, "no", clangxx,
				"-emit-llvm", "-c", inputVFS.String(), "-o", bcOut.String(),
			}
			node := &Node{
				Cmds:             []Cmd{{CmdArgs: bcArgs, Env: env}},
				Env:              env,
				Inputs:           []VFS{inputVFS},
				Outputs:          []VFS{bcOut},
				KV:               map[string]interface{}{"p": "BC", "pc": "light-green"},
				Tags:             []string{},
				TargetProperties: cloneStringMap(tp),
				Requirements:     reqs,
				Sandboxing:       true,
			}
			if producer, ok := d.prOutputProducer[src]; ok {
				node.DepRefs = append(node.DepRefs, producer)
			}
			ref := ctx.emit.Emit(bindNodePlatform(node, instance.Platform))
			bcRefs = append(bcRefs, ref)
			bcPaths = append(bcPaths, bcOut)
		}

		mergedOut := Build(instance.Path + "/" + stmt.Name + "_merged" + stmt.Suffix + ".bc")
		ldArgs := []string{llvmLink}
		for _, p := range bcPaths {
			ldArgs = append(ldArgs, p.String())
		}
		ldArgs = append(ldArgs, "-o", mergedOut.String())
		ldNode := &Node{
			Cmds:             []Cmd{{CmdArgs: ldArgs, Env: env}},
			Env:              env,
			Inputs:           []VFS{},
			Outputs:          []VFS{mergedOut},
			KV:               map[string]interface{}{"p": "LD", "pc": "light-red"},
			Tags:             []string{},
			TargetProperties: cloneStringMap(tp),
			Requirements:     reqs,
			Sandboxing:       true,
			DepRefs:          append([]NodeRef(nil), bcRefs...),
		}
		ldRef := ctx.emit.Emit(bindNodePlatform(ldNode, instance.Platform))

		optOutName := stmt.Name + "_optimized" + stmt.Suffix + ".bc"
		optOut := Build(instance.Path + "/" + optOutName)
		optArgs := []string{python, optWrapper, opt, mergedOut.String(), "-o", optOut.String()}
		passes := []string{"default<O2>", "globalopt", "globaldce"}
		if len(stmt.Symbols) > 0 {
			passes = append(passes, "internalize")
			optArgs = append(optArgs, "-internalize-public-api-list="+strings.Join(stmt.Symbols, "#"))
		}
		optArgs = append(optArgs, "'-passes=\""+strings.Join(passes, "${__COMMA__}")+"\"'")
		optNode := &Node{
			Cmds:             []Cmd{{CmdArgs: optArgs, Env: env}},
			Env:              env,
			Inputs:           []VFS{mergedOut},
			Outputs:          []VFS{optOut},
			KV:               map[string]interface{}{"p": "OP", "pc": "yellow"},
			Tags:             []string{},
			TargetProperties: cloneStringMap(tp),
			Requirements:     reqs,
			Sandboxing:       true,
			DepRefs:          []NodeRef{ldRef},
		}
		opRef := ctx.emit.Emit(bindNodePlatform(optNode, instance.Platform))

		if stmt.GenerateMachineCode {
			continue
		}

		ensureResourcePeer(instance.Path, d)
		if d.prOutputProducer == nil {
			d.prOutputProducer = map[string]NodeRef{}
		}
		d.prOutputProducer[optOutName] = opRef
		d.resources = append(d.resources, resourceEntry{
			Path:      optOutName,
			Key:       "/llvm_bc/" + stmt.Name,
			EndsBatch: true,
		})
	}
}

func llvmBcInputVFS(ctx *genCtx, instance ModuleInstance, d *moduleData, src string) VFS {
	if producer := d.prOutputProducer[src]; producer != (NodeRef{}) {
		return copyFileOutputVFS(instance.Path, src)
	}
	return copyFileInputVFS(ctx.fs, instance.Path, src)
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
	if sourceInputVFS(ctx.fs, instance.Path, src) != nil {
		return instance.Path + "/" + src
	}
	return src
}

func stripResourceName(s string) string {
	if i := strings.Index(s, "::"); i >= 0 {
		return s[i+2:]
	}
	return s
}

func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
