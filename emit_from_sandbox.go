package main

import "strings"

// emitFromSandboxes emits the SB (Sandbox fetch) node for every FROM_SANDBOX in
// the module and registers its OUT/OUT_NOAUTO files as build outputs bound to
// that node. A name so declared is a fetched build artifact: a RUN_PROGRAM/macro
// IN that consumes it then resolves (via runProgramInputVFS' codegen-registry
// probe) to the $(B) fetch output and takes the SB node as a dependency, instead
// of resolving to a nonexistent on-disk source path. Must run before the module's
// RUN_PROGRAM/RUN_PYTHON emit so the registry already holds the outputs.
//
// Returns the auto OUT (never OUT_NOAUTO) `.a`/`.o` outputs as module link
// members: ymake folds an auto FROM_SANDBOX output into $AUTO_INPUT, so a LIBRARY
// archives it into its own `.a` (LINK_LIB = … $TARGET $AUTO_INPUT) and a PROGRAM
// links it. The caller appends these to the module's ccRefs/ccOutputs.
func emitFromSandboxes(ctx *GenCtx, instance ModuleInstance, d *ModuleData) (memberRefs []NodeRef, memberPaths []VFS) {
	for _, fs := range d.fromSandboxes {
		refs, paths := emitFromSandbox(ctx, instance, d, fs)
		memberRefs = append(memberRefs, refs...)
		memberPaths = append(memberPaths, paths...)
	}

	return memberRefs, memberPaths
}

// fromSandboxScriptInputs are the three scripts the FROM_SANDBOX macro names
// explicitly on its command path (ymake.core.conf FROM_SANDBOX .CMD): the
// fetch_from_sandbox.py wrapper plus the hidden process_command_files.py and
// fetch_from.py. ymake's ${input:"…"} adds exactly the named file — it does not
// expand the script's Python import closure — so the SB node carries only these
// three, never the helpers they import (retry.py, error.py).
var fromSandboxScriptInputs = []VFS{
	buildScriptsFetchFromSandboxPy,
	buildScriptsProcessCommandFilesPy,
	buildScriptsFetchFromPy,
}

// fromSandboxAutoLinkMember reports whether an auto OUT file is a linkable
// artifact (static archive or object) that ymake routes into $AUTO_INPUT.
func fromSandboxAutoLinkMember(name string) bool {
	return strings.HasSuffix(name, ".a") || strings.HasSuffix(name, ".o")
}

func emitFromSandbox(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *FromSandboxStmt) (memberRefs []NodeRef, memberPaths []VFS) {
	na := ctx.emit.nodeArenas()
	id := stmt.ResourceId.string()

	// Mirrors upstream's FROM_SANDBOX .CMD: $YMAKE_PYTHON3 fetch_from_sandbox.py
	// … --resource-id $Id (--untar-to | --copy-to-dir) $PREFIX [--executable] --
	// $OUT $OUT_NOAUTO, run in the module build dir.
	mode := "--untar-to"

	if stmt.File {
		mode = "--copy-to-dir"
	}

	args := []STR{
		d.tc.Python3,
		buildScriptsFetchFromSandboxPy.str(),
		argYaStartCommandFile.str(),
		strResourceFile,
		internStr("$(RESOURCE_ROOT)/sbr/" + id + "/resource"),
		strResourceId,
		stmt.ResourceId,
		internStr(mode),
		internStr(stmt.Prefix),
	}

	// Upstream ${pre=--rename :RENAME}: one `--rename <item>` pair per rename,
	// rendered after $PREFIX and before $EXECUTABLE/--.
	for _, r := range stmt.Renames {
		args = append(args, strRename, r)
	}

	if stmt.Executable {
		args = append(args, strExecutable)
	}

	args = append(args, arg2.str())

	outVFSs := make([]VFS, 0, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

	for _, f := range stmt.OUTFiles {
		args = append(args, f)
		outVFSs = append(outVFSs, copyFileOutputVFS(instance.Path.rel(), f.string()))
	}

	for _, f := range stmt.OUTNoAutoFiles {
		args = append(args, f)
		outVFSs = append(outVFSs, copyFileOutputVFS(instance.Path.rel(), f.string()))
	}

	args = append(args, argYaEndCommandFile.str())

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(args), Cwd: build(instance.Path.rel()).str(), Env: env}),
		Env:              env,
		Inputs:           na.inputList(fromSandboxScriptInputs),
		KV:               KV{P: pkSB, PC: pcYellow, ShowOut: true},
		Outputs:          na.vfsList(outVFSs...),
		Requirements:     Requirements{CPU: float64(1), Network: nwFull, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Resources:        usesPython3,
	}

	ref := ctx.emit.emit(node)

	// Auto OUT (not OUT_NOAUTO) `.a`/`.o` outputs are module link members: they
	// occupy the first len(OUTFiles) entries of outVFSs.
	for i, f := range stmt.OUTFiles {
		if fromSandboxAutoLinkMember(f.string()) {
			memberRefs = append(memberRefs, ref)
			memberPaths = append(memberPaths, outVFSs[i])
		}
	}

	// Register each fetched output as a codegen producer so a consuming IN resolves
	// to the $(B) path and depends on this SB node. The files are opaque fetched
	// artifacts (no parsed includes); OUTPUT_INCLUDES, when present, ride as the
	// outputs' registered includes for downstream scans.
	parsed := fromSandboxOutputIncludes(stmt)
	reg := codegenRegForInstance(ctx, instance)

	for _, out := range outVFSs {
		registerBoundGeneratedParsedOutput(ctx, instance, pkSB, out, parsed, ref, nil)

		// Upstream's flat-input model lists a producer's transitive source closure
		// on every consumer of its outputs. The SB node's source inputs are exactly
		// its three explicit fetch scripts; record them so a RUN_PROGRAM that
		// consumes this fetched file as IN (and any further ARCHIVE_ASM/RD consumer)
		// carries them — without parsing the opaque fetched data as includes.
		reg.setSourceInputs(out, fromSandboxScriptInputs)

		// The fetch is one command with all OUT/OUT_NOAUTO files as outputs; its
		// main output is the first declared file (outVFSs[0]). A consumer that
		// embeds only the additional outputs (e.g. a RESOURCE_FILES objcopy chunk
		// over later dicts) still lists the main output as a spurious input,
		// because it depends on the single fetch node via the OutTogether
		// main-output edge (json_visitor.cpp:999-1023). Record the main output so
		// the objcopy emitter reproduces that edge.
		reg.setProducerMainOut(out, outVFSs[0])
	}

	return memberRefs, memberPaths
}

func fromSandboxOutputIncludes(stmt *FromSandboxStmt) []IncludeDirective {
	if len(stmt.OutputIncludes) == 0 {
		return nil
	}

	includes := make([]IncludeDirective, 0, len(stmt.OutputIncludes))

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = internStr(v.rel())
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: f})
	}

	return includes
}
