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

func (e *EmitContext) emitRosSource(meta SrcMeta) {
	ctx, d := e.ctx, e.d

	if d.ros == nil {
		return
	}

	src := resolveSourceVFS(ctx, e.instance, meta.Source.string(), d.srcDirs)
	toolRef, tool := ctx.tool(internArg(rosGenerator))
	outs := e.rosOutputPaths(src.relString(), meta.RosZeroCopy)
	closure := e.scanner.walkClosure(src, d.scanCtx, scanDomainRos)
	args := []ANY{tool.any(), strLanguage.any(), strCpp2.any()}

	if meta.RosZeroCopy {
		args = append(args, strHasZeroCopyPeer.any())
	}

	args = append(args, internAnys([]string{"--repository-root", "$(S)", "--third-party-root", "$(S)/sdg/sdc/third_party_root/ros", "--package", d.ros.Package})...)

	for _, inc := range d.scanCtx.rosInclude {
		args = append(args, internV("-I", inc.relString()).any())
	}

	args = append(args, src.any(), strOutputDir.any(), build(d.ros.Dir).any())

	cmds := []Cmd{{CmdArgs: ctx.na.chunkList(args), Cwd: srcRootDirVFS, Env: envVarsVCS}}

	if meta.RosZeroCopy {
		second := append([]ANY(nil), args[:3]...)

		second[2] = strCppZeroCopy.any()
		second = append(second, args[4:]...)

		cmds = append(cmds, Cmd{CmdArgs: ctx.na.chunkList(second), Cwd: srcRootDirVFS, Env: envVarsVCS})
	}

	ref := ctx.emit.reserve()
	node := Node{Platform: e.instance.Platform, Cmds: cmds, Env: envVarsVCS, Inputs: append(ctx.na.inputList(ctx.na.vfsList(tool, src)), closure.bucketList()...), Outputs: outs, KV: &rosKV, ForeignDepRefs: ctx.na.refList(toolRef)}
	pending := e.pendingEmit(func() { e.emitReservedNode(node, ref) })
	includes := append([]string(nil), rosCppIncludes...)

	if meta.RosZeroCopy {
		includes = append(includes, rosZeroCopyIncludes...)
	}

	if strings.HasSuffix(src.relString(), ".srv") {
		includes = append(includes, "sdg/sdc/bros/traits/include/ros/service_traits.h")
	}

	var parsed []IncludeDirective

	for _, inc := range includes {
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(inc).any())})
	}

	children, _ := e.scanner.cachedChildren(src)

	for _, dep := range children {
		dir := path.Dir(dep.relString())

		if path.Base(dir) == "msg" || path.Base(dir) == "srv" {
			dir = path.Dir(dir)
		}

		header := build(dir, "/", strings.TrimSuffix(path.Base(dep.relString()), ".msg"), ".h")

		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(header.any())})
	}

	for _, out := range outs {
		info := GeneratedFileInfo{OutputPath: out, ProducerRef: ref, GeneratorRefs: ctx.na.refList(toolRef), ClosureLeaves: ctx.na.vfsList(src), ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed}, OnUse: pending}

		e.register(info)

		if isCCSourceExt(out.relString()) {
			child := meta

			child.Source = out.any()
			child.Global = strings.HasSuffix(src.relString(), ".msg")
			e.enqueueSrc(child)
		}
	}
}
