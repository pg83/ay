package main

import "strings"

// emitFromSandboxes emits the SB (Sandbox fetch) node for every FROM_SANDBOX and
// registers its OUT/OUT_NOAUTO files as outputs bound to that node. Must run
// before the module's RUN_PROGRAM/RUN_PYTHON emit so the registry holds them.
//
// Returns the auto OUT `.a`/`.o` outputs as module link members, archived/linked
// by the enclosing module.
func emitFromSandboxes(ctx *GenCtx, instance ModuleInstance, d *ModuleData) (memberRefs []NodeRef, memberPaths []VFS) {
	for _, fs := range d.fromSandboxes {
		refs, paths := emitFromSandbox(ctx, instance, d, fs)
		memberRefs = append(memberRefs, refs...)
		memberPaths = append(memberPaths, paths...)
	}

	return memberRefs, memberPaths
}

// fromSandboxScriptInputs are the scripts FROM_SANDBOX names on its command path.
// ${input:"…"} adds exactly the named file, not the Python import closure, so the
// SB node carries only these, never the helpers they import.
var fromSandboxScriptInputs = []VFS{
	buildScriptsFetchFromSandboxPy,
	buildScriptsProcessCommandFilesPy,
	buildScriptsFetchFromPy,
}

// fromSandboxAutoLinkMember reports whether an auto OUT file is a linkable
// artifact routed into $AUTO_INPUT.
func fromSandboxAutoLinkMember(name string) bool {
	return strings.HasSuffix(name, ".a") || strings.HasSuffix(name, ".o")
}

func emitFromSandbox(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *FromSandboxStmt) (memberRefs []NodeRef, memberPaths []VFS) {
	na := ctx.emit.nodeArenas()
	id := stmt.ResourceId.string()

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

	// Auto OUT `.a`/`.o` outputs are link members, in the first len(OUTFiles)
	// entries of outVFSs.
	for i, f := range stmt.OUTFiles {
		if fromSandboxAutoLinkMember(f.string()) {
			memberRefs = append(memberRefs, ref)
			memberPaths = append(memberPaths, outVFSs[i])
		}
	}

	// Register each fetched output as a codegen producer so a consuming IN depends
	// on this SB node. OUTPUT_INCLUDES, when present, ride as the outputs'
	// registered includes for downstream scans.
	parsed := fromSandboxOutputIncludes(stmt)
	reg := codegenRegForInstance(ctx, instance)

	for _, out := range outVFSs {
		registerBoundGeneratedParsedOutput(ctx, instance, pkSB, out, parsed, ref, nil)

		// The flat-input model lists a producer's source closure on every consumer.
		// The SB node's sources are exactly its fetch scripts; record them so a
		// consumer carries them without parsing the opaque fetched data as includes.
		reg.setSourceInputs(out, fromSandboxScriptInputs)

		// One fetch command owns all outputs; its main output is the first declared
		// file. A consumer of any additional output still lists the main output via
		// the OutTogether edge, so record it for the objcopy emitter to reproduce.
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
