package main

import "strings"

// flexDefaultGenExt is _FLEX_GEN_EXT's default (.cpp) — old flex emits C++.
const flexDefaultGenExt = ".cpp"

// flexOutputInclude is the util/system/compiler.h that _SRC("l")'s
// `${hide;output_include:"util/system/compiler.h"}` forces into every
// generated lexer source (bison_lex.conf).
var flexOutputInclude = IncludeDirective{kind: includeQuoted, target: internStr("util/system/compiler.h")}

// flexGeneratedVFS is the generated-source path of a lex/flex source — the same
// nopath placement as ragel6OutVFS / bisonGeneratedRel: a flat (in-module)
// source lands its .cpp directly in the module build dir; a subdir source is
// rebased under the _/ namespace.
func flexGeneratedVFS(instance ModuleInstance, srcRel string) VFS {
	if strings.Contains(srcRel, "/") {
		return build(instance.Path.rel() + "/_/" + srcRel + flexDefaultGenExt)
	}

	return build(instance.Path.rel() + "/" + srcRel + flexDefaultGenExt)
}

// emitLibraryFlexSource reproduces default old-flex _SRC("l"/"lex"/"lpp")
// (bison_lex.conf): the LX/yellow node runs `contrib/tools/flex-old/flex
// -o<src>.cpp <src>` and the generated .cpp is compiled as C++ and archived.
// Shaped like emitLibraryRagel6Source (single tool, generates a .cpp no sibling
// consumes as a header), so it stays in the pass-1 codegen loop rather than the
// bison two-phase pre-pass. The flex-old ADDINCL is applied module-wide in
// collectModule so the generated lexer's <FlexLexer.h> resolves here.
func emitLibraryFlexSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	flexRef, flexBin := ctx.tool(argContribToolsFlexOld)

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	outVFS := flexGeneratedVFS(instance, srcRel)

	// The generated .cpp carries util/system/compiler.h (output_include) ++ the
	// .l's own prologue #includes (flex copies the %{…%} block verbatim). Register
	// before walking so the closure resolves through the codegen registry.
	parsed := make([]IncludeDirective, 0, 1)
	parsed = append(parsed, flexOutputInclude)
	parsed = append(parsed, ctx.scannerFor(instance).parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)

	lxRef := ctx.emit.reserve()
	registerBoundGeneratedParsedOutput(ctx, instance, pkLX, outVFS, parsed, lxRef, []NodeRef{flexRef})

	window := walkClosure(ctx.scannerFor(instance), outVFS, in.ScanCfg)

	// flex only reads source files (the .l + the headers its prologue pulls);
	// generated $(B) headers in the closure reach the LX node's cache key through
	// their own producers, so keep only source-level inputs — same as the ragel
	// node (and matching the reference LX node, whose sole $(B) input is the tool).
	lxClosure := keepOnlySourceVFS(window)

	emitFlexLX(instance, flexRef, flexBin, srcVFS, outVFS, lxClosure, lxRef, ctx.emit)

	ccSrcRel := strings.TrimPrefix(outVFS.rel(), instance.Path.rel()+"/")

	ccIn := in
	ccIn.IncludeInputs = window
	ccIn.ExtraDepRefs = append([]NodeRef{lxRef}, resolveCodegenDepRefs(ctx, instance, window, lxRef)...)

	// _LANG_CFLAGS_VALUE_NEW (ymake.core.conf): the generated .l.cpp compile gets
	// `-Wno-unused-variable` (_LANG_CFLAGS_LEX) and the `.l` source as a hidden
	// input — both from the single `${pre=$_LANG_CFLAGS_LEX;ext=.l;…;input:SRC}`
	// directive, which keys on the `.l` extension only (not .lex/.lpp).
	if strings.HasSuffix(srcRel, ".l") {
		ccIn.IncludeInputs = dedupVFS(window, []VFS{srcVFS})
		ccIn.PerSourceCFlags = append(append([]ARG(nil), in.PerSourceCFlags...), argWnoUnusedVariable)
	}

	ccRef, ccOut, _ := emitCC(instance, ccSrcRel, outVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}

func emitFlexLX(instance ModuleInstance, flexRef NodeRef, flexBin VFS, srcVFS, outVFS VFS, closure []VFS, id NodeRef, emit Emitter) {
	na := emit.nodeArenas()

	cmdArgs := na.chunkList(na.strList(
		flexBin.str(),
		internStr(argDashO.string()+outVFS.string()),
		srcVFS.str(),
	))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	emit.emitReserved(&Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(flexBin, srcVFS), closure),
		Outputs:          na.vfsList(outVFS),
		KV:               KV{P: pkLX, PC: pcYellow},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		ForeignDepRefs:   []NodeRef{flexRef},
	}, id)
}
