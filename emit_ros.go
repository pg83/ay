package main

import (
	"path"
	"strings"
)

var rosKV = KV{P: pkRS, PC: pcCyan}

func (e *EmitContext) rosOutputPaths(src string, zero bool) []VFS {
	base := strings.TrimSuffix(path.Base(src), path.Ext(src))
	dir := e.d.ros.Dir
	suffixes := []string{".cc", ".h"}

	switch e.d.ros.Language {
	case LangGo:
		return []VFS{build(dir, "/", base, ".go")}
	case LangPy:
		kind := "msg"

		if strings.HasSuffix(src, ".srv") {
			kind = "srv"
		}

		return []VFS{build(e.instance.Path.relString(), "/", kind, "/_", base, ".py")}
	case LangRosBindings:
		return []VFS{build(dir, "/", base, ".py.cc"), build(dir, "/", base, ".py.h")}
	}

	if strings.HasSuffix(src, ".srv") {
		suffixes = []string{".h", "Request.cc", "Request.h", "Response.cc", "Response.h"}
	} else if zero {
		suffixes = []string{".cc", "_zero_copy.cc", ".h", "_zero_copy.h", "_ros_converter.h"}
	}

	outs := make([]VFS, 0, len(suffixes))

	for _, suffix := range suffixes {
		outs = append(outs, build(dir, "/", base, suffix))
	}

	return outs
}

func (e *EmitContext) emitRosSource(meta SrcMeta, outputs []VFS) {
	ctx, d := e.ctx, e.d

	if d.ros == nil {
		return
	}

	src := e.sourceForEmission(meta)
	toolRef, tool := ctx.tool(internArg(rosGenerator))

	if outputs == nil {
		outputs = e.rosOutputPaths(src.relString(), meta.RosZeroCopy)
	}

	outs := ctx.na.vfsList(outputs...)
	closure := e.scanner.walkClosure(src, d.scanCtx, scanDomainRos)
	lang := "cpp"

	switch d.ros.Language {
	case LangGo:
		lang = "golang"
	case LangPy:
		lang = "python"
	case LangRosBindings:
		lang = "python_cpp_bindings"
	}

	zero := meta.RosZeroCopy && d.ros.Language == LangCPP
	args := []ANY{tool.any(), strLanguage.any(), internStr(lang).any()}

	if zero {
		args = append(args, strHasZeroCopyPeer.any())
	}

	if d.ros.Language != LangPy && d.ros.Language != LangGo {
		args = append(args, strRepositoryRoot.any(), srcRootDirVFS.any())
	}

	thirdParty := "sdg/sdc/third_party_root/ros"

	if d.ros.Language == LangGo {
		thirdParty = "sdg/sdc/third_party/ros"
	}

	args = append(args, strThirdPartyRoot.any(), source(thirdParty).any(), strPackage.any(), internStr(d.ros.Package).any())

	for _, inc := range d.scanCtx.rosInclude {
		args = append(args, internV("-I", inc.relString()).any())
	}

	outDir := build(d.ros.Dir)

	if d.ros.Language == LangPy {
		outDir = build(path.Dir(outs[0].relString()))
	}

	args = append(args, src.any(), strOutputDir.any(), outDir.any())

	cmds := []Cmd{{CmdArgs: ctx.na.chunkList(args), Cwd: srcRootDirVFS.cwd(), Env: envVarsVCS}}

	if zero {
		second := append([]ANY(nil), args[:3]...)

		second[2] = strCppZeroCopy.any()
		second = append(second, args[4:]...)

		cmds = append(cmds, Cmd{CmdArgs: ctx.na.chunkList(second), Cwd: srcRootDirVFS.cwd(), Env: envVarsVCS})
	}

	ref := ctx.emit.reserve()
	node := NodeSpec{Node: Node{Platform: e.instance.Platform, Cmds: cmds, Env: envVarsVCS, Inputs: ctx.na.rootInputList(ctx.na.vfsList(tool, src), closure.bucketList()...), Outputs: outs, KV: &rosKV, ForeignDepRefs: ctx.na.refList(toolRef)}, input: InputSpec{roots: ctx.na.inputList(ctx.na.vfsList(src), closure.bucketList()...)}}
	pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })

	var includes []string

	switch d.ros.Language {
	case LangCPP:
		includes = append(includes, rosCppIncludes...)
	case LangRosBindings:
		includes = append(includes,
			"sdg/sdc/ros/misc/python_utils/numpy_wrapper.h",
			"sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export.h",
			"sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export_arrays.h",
			"sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export_enum.h",
			build(d.ros.Dir, "/", strings.TrimSuffix(path.Base(src.relString()), path.Ext(src.relString())), ".h").string())
	}

	if zero {
		includes = append(includes, rosZeroCopyIncludes...)
	}

	if d.ros.Language == LangCPP && strings.HasSuffix(src.relString(), ".srv") {
		includes = append(includes, "sdg/sdc/bros/traits/include/ros/service_traits.h")
	}

	var parsed []IncludeDirective

	for _, inc := range includes {
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(inc).any())})
	}

	children, _ := e.scanner.cachedChildren(src)

	for _, dep := range children {
		if d.ros.Language != LangCPP && d.ros.Language != LangRosBindings {
			break
		}

		dir := path.Dir(dep.relString())

		if path.Base(dir) == "msg" || path.Base(dir) == "srv" {
			dir = path.Dir(dir)
		}

		header := build(dir, "/", strings.TrimSuffix(path.Base(dep.relString()), ".msg"), ".h")

		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(header.any())})
	}

	for _, out := range outs {
		info := GeneratedFileSpec{OutputPath: out, ProducerRef: ref, ProducerMainOut: outs[0], GeneratorRefs: ctx.na.refList(toolRef), ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed}, OnUse: pending}

		e.register(info)

		if isCCSourceExt(out.relString()) {
			child := meta

			child.Source = out.any()
			child.Global = d.ros.Language == LangCPP && strings.HasSuffix(src.relString(), ".msg")
			e.enqueueSrc(child)
		} else if d.ros.Language == LangGo {
			e.collectGoAutoSource(out, meta.Seq)
		}
	}
}

func (e *EmitContext) emitRosEmpty() {
	na := e.ctx.na
	out := build(e.instance.Path.relString(), "/empty.go")
	script := buildScriptsWriterPy
	ref := e.ctx.emit.reserve()
	inputs := append([]VFS{script}, e.ctx.scripts[script.rel()]...)
	node := NodeSpec{Node: Node{Platform: e.instance.Platform, Env: envVarsVCS, KV: &rosKV, Resources: usesPython3, Inputs: na.rootInputList(na.vfsList(inputs...)), Outputs: na.vfsList(out), Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(e.d.tc.Python3.any(), script.any(), strFile.any(), out.any(), strContent.any(), strPackageGenerated.any())), Env: envVarsVCS})}}
	pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })

	e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, OnUse: pending})
	e.collectGoAutoSource(out, 0)
}
