package main

import "strings"

const ytProtoYsonTool = "yt/yt/orm/go/codegen/yson/internal/proto-yson-gen/cmd/proto-yson-gen"

func applyYtOrmProtoYson(modulePath string, args []ANY, d *ModuleData, env Environment) {
	files, lists := goMacroLists(args, "OUT_OPTS")

	d.goCodegens = append(d.goCodegens, GoCodegenStmt{
		kind: goCodegenYson, seq: d.nextDeclSeq(), files: files, args: lists["OUT_OPTS"],
	})

	d.protoAddIncl = append(d.protoAddIncl, source(modulePath))

	if imp := env.string(envGO_TEST_IMPORT_PATH); imp != "" {
		d.protoAddIncl = append(d.protoAddIncl, source(imp))
	}

	d.protoAddIncl = append(d.protoAddIncl, yt, contribLibsProtobufSrc)
	d.addIncl = append(d.addIncl, bldYt)

	std := env.string(envGOSTD)

	for _, peer := range []string{
		std + "/strings", std + "/fmt", std + "/errors", std + "/encoding/json",
		"library/go/core/xerrors", "yt/go/yson", "yt/go/yterrors",
		"yt/yt/orm/go/codegen/yson/ytypes", "contrib/libs/protobuf",
	} {
		d.macroPeerdirs = append(d.macroPeerdirs, internStr(peer).any())
	}
}

func (e *EmitContext) emitYtOrmProtoYson(st GoCodegenStmt) {
	ctx, d := e.ctx, e.d
	na := ctx.na
	protocRef, protoc := ctx.tool(argContribToolsProtoc)
	pluginRef, plugin := ctx.tool(internArg(ytProtoYsonTool))

	args := []ANY{
		protoc.any(), internV("--plugin=protoc-gen-custom=", plugin.string()).any(),
		internV("-I=$(S)/", e.goProtoNamespace()).any(), argIB2.any(), argIS3.any(),
	}

	for _, p := range concat(dedup(d.protoAddInclGlobal, d.protoAddIncl), e.peers.ProtoInclude) {
		args = append(args, internV("-I=", p.string()).any())
	}

	args = append(args, argIS3.any(),
		internV("--custom_out=", strings.Join(anyStrings(st.args), " "), " paths=base_name:.").any(),
		internV("--custom_opt=goroot=", strGoToolsRoot.string()).any())

	args = append(args, d.protocFlags...)

	inputs := [][]VFS{na.vfsList(protoc, plugin)}
	outs := make([]VFS, 0, len(st.files))
	names := st.outputNames()

	for i, file := range st.files {
		src := e.resolveModuleSourceVFS(file, d.srcDirs)

		args = append(args, src.any())
		inputs = append(inputs, na.vfsList(src))

		view := e.scanner.walkClosure(src, d.scanCtx, scanDomainProto)

		inputs = append(inputs, view.bucketList()...)

		out := build(e.instance.Path.relString(), "/", names[i])

		outs = append(outs, out)
	}

	ref := e.emitNode(Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)),
			Cwd: build(e.instance.Path.relString()), Env: envVarsVCS}),
		Env: envVarsVCS, Inputs: NodeInputs{files: na.rootInputList(inputs[0], inputs[1:]...), roots: InputChunks(inputs[1:])}, Outputs: na.vfsList(outs...),
		KV: &pbKV, ForeignDepRefs: na.refList(protocRef, pluginRef),
	})

	for _, out := range outs {
		e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, ProducerMainOut: outs[0]})
		e.collectGoAutoSource(out, st.seq)
	}
}
