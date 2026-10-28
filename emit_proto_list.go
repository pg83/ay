package main

import "strings"

type ListProtoStmt struct {
	Output ANY
	Files  []ANY
}

func listProtoHeader(output VFS) VFS {
	return build(strings.TrimSuffix(output.relString(), ".proto"), ".pb.h")
}

func (e *EmitContext) emitListProto(stmt ListProtoStmt) {
	na := e.ctx.na
	output := copyFileOutputVFS(e.instance.Path.relString(), stmt.Output.string())
	header := listProtoHeader(output)

	var sources []VFS
	var parsed []IncludeDirective

	for _, file := range stmt.Files {
		src := e.resolveModuleSourceVFS(file, e.d.srcDirs)

		sources = append(sources, src)
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(src.rel().any())})

		view := e.scanner.walkClosure(src, e.d.scanCtx, scanDomainProto)

		for _, bucket := range view.bucketList() {
			sources = append(sources, bucket...)
		}
	}

	sources = na.vfsList(dedup(sources)...)

	ref, headerRef := e.ctx.emit.reserve(), e.ctx.emit.reserve()
	listScript, touchScript := buildScriptsListPy, buildScriptsTouchPy
	args := na.anyList(append([]ANY{e.d.tc.Python3.any(), listScript.any()}, stmt.Files...)...)

	pending := e.pendingEmit(func() {
		e.emitReservedNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(args), Stdout: output, Env: envVarsVCS}),
			Env:  envVarsVCS, Inputs: na.inputList(na.vfsList(listScript), sources), Outputs: na.vfsList(output),
			KV: &pbKV, Resources: usesPython3},
		}, ref)
	})

	e.register(GeneratedFileSpec{
		OutputPath: output, ProducerRef: ref, OnUse: pending,

		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: na.dirs.list(parsed...)},
	})

	touch := e.pendingEmit(func() {
		e.emitReservedNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(e.d.tc.Python3.any(), touchScript.any(), header.any())), Env: envVarsVCS}),
			Env:  envVarsVCS, Inputs: na.inputList(na.vfsList(touchScript), na.vfsList(output)), Outputs: na.vfsList(header),
			KV: &pbKV, Resources: usesPython3},
		}, headerRef)
	})

	induced := protoInducedPbH(e.ctx.parsers, parsed, nil)

	e.register(GeneratedFileSpec{
		OutputPath: header, ProducerRef: headerRef, OnUse: touch,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: na.dirs.list(induced...)},
	})
}
