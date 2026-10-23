package main

import (
	"path"
	"strings"
)

func (e *EmitContext) emitPerlSwigSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	src := e.resolveModuleSourceVFS(meta.Source, d.cc.SrcDirs)
	srcRel := composeSrcDirOutputRel(instance.Path.relString(), src.relString())
	cpp := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, path.Ext(srcRel)), "_wrap.swg.cpp")
	name := ""

	if len(d.moduleStmt.Args) > 0 {
		name = d.moduleStmt.Args[0].string()
	} else if isDllModuleType(d.moduleStmt.Name) {
		name = javaRealPrjName(instance.Path.relString(), javaNameTwoDirs)
	} else if isProgramModuleType(d.moduleStmt.Name) {
		name = path.Base(instance.Path.relString())
	} else {
		name = javaRealPrjName(instance.Path.relString(), javaNameThreeDirs)
	}

	perl := build(instance.Path.relString(), "/", name, ".pm")
	swigRef, swigBin := ctx.tool(argContribToolsSwig)
	ref := ctx.emit.reserve()

	meta.Source = cpp.any()
	e.enqueueSrc(meta)

	var cppInfo *GeneratedFileInfo

	pending := e.pendingEmit(func() {
		cv := e.scanner.walkClosure(src, d.scanCtx, scanDomainSwig)
		buckets := cv.bucketList()
		closure := collectBucketVFS(na, buckets, func(VFS) bool { return true })

		cppInfo.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: collectSwigInducedIncludes(e.scanner, src, closure)}

		args := []ANY{
			swigBin.any(), strC5.any(), strCpperraswarn.any(),
			strModule.any(), internStr(name).any(), strShadow.any(), strPerl.any(),
			argDashO.any(), cpp.any(), strOutdir2.any(), build(instance.Path.relString()).any(),
		}

		args = append(args, swigJNIIncludeArgs(d.cc.SwigInclude)[1:]...)
		args = append(args, src.any())

		inputs := InputChunks{na.vfsList(swigBin, src)}

		inputs = append(inputs, buckets...)

		e.emitReservedNode(Node{
			Platform:        instance.Platform,
			Cmds:            na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: envVarsVCS}),
			Env:             envVarsVCS,
			Inputs:          inputs,
			BuildFromInputs: na.inputList(inputs[0][1:], inputs[1:]...),
			InputClosures:   []Closure{cv},
			Outputs:         na.vfsList(cpp, perl),
			KV:              &swigJNIKV,
			ForeignDepRefs:  na.refList(swigRef),
		}, ref)
	})

	cppInfo = e.register(GeneratedFileInfo{
		OutputPath:    cpp,
		ProducerRef:   ref,
		GeneratorRefs: na.refList(swigRef),
		ModuleOutputs: na.vfsList(perl),
		OnUse:         pending,
	})

	e.register(GeneratedFileInfo{
		OutputPath:      perl,
		ProducerRef:     ref,
		ProducerMainOut: cpp,
		GeneratorRefs:   na.refList(swigRef),
		OnUse:           pending,
	})
}
