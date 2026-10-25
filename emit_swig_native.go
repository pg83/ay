package main

import (
	"path"
	"strings"
)

func (e *EmitContext) emitNativeSwigSource(meta SrcMeta, python bool) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	src := e.resolveModuleSourceVFS(meta.Source, d.cc.SrcDirs)
	srcRel := composeSrcDirOutputRel(instance.Path.relString(), src.relString())
	cpp := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, path.Ext(srcRel)), "_wrap.swg.cpp")
	name, module := e.nativeSwigModuleNames()
	extension := ".pm"

	if python {
		extension = ".py"
	}

	moduleOutput := build(instance.Path.relString(), "/", name, extension)
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

		var args []ANY

		if python {
			args = []ANY{swigBin.any(), strModule.any(), internStr(name).any(), strCpperraswarn.any(),
				strC5.any(), argPython.any(), argInterface.any(), internStr(module).any(),
				argDashO.any(), cpp.any(), strOutdir2.any(), build(instance.Path.relString()).any()}
		} else {
			args = []ANY{swigBin.any(), strC5.any(), strCpperraswarn.any(),
				strModule.any(), internStr(name).any(), strShadow.any(), strPerl.any(),
				argDashO.any(), cpp.any(), strOutdir2.any(), build(instance.Path.relString()).any()}
		}

		args = append(args, swigJNIIncludeArgs(d.cc.SwigInclude)[1:]...)
		args = append(args, src.any())

		inputs := InputChunks{na.vfsList(swigBin, src)}

		inputs = append(inputs, buckets...)

		e.emitReservedNode(Node{
			Platform:        instance.Platform,
			Cmds:            na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: envVarsVCS}),
			Env:             envVarsVCS,
			inputPlan:       na.nodeInputs(inputs),
			BuildFromInputs: na.inputList(inputs[0][1:], inputs[1:]...),
			Outputs:         na.vfsList(cpp, moduleOutput),
			KV:              &swigJNIKV,
			ForeignDepRefs:  na.refList(swigRef),
		}, ref)
	})

	cppInfo = e.register(GeneratedFileInfo{
		OutputPath:    cpp,
		ProducerRef:   ref,
		GeneratorRefs: na.refList(swigRef),
		ModuleOutputs: na.vfsList(moduleOutput),
		OnUse:         pending,
	})

	e.register(GeneratedFileInfo{
		OutputPath:      moduleOutput,
		ProducerRef:     ref,
		ProducerMainOut: cpp,
		GeneratorRefs:   na.refList(swigRef),
		OnUse:           pending,
	})
}

func (e *EmitContext) nativeSwigModuleNames() (string, string) {
	stmt := e.d.moduleStmt
	dir := e.instance.Path.relString()

	if isDllModuleType(stmt.Name) {
		name := javaRealPrjName(dir, javaNameTwoDirs)
		args := parseDllModuleArgs(stmt.Args)

		if args.hasName {
			name = args.name.string()
		}

		return name, strings.TrimSuffix(dllOutputName(e.instance, stmt), ".so")
	}

	if isProgramModuleType(stmt.Name) {
		module := programBinaryName(e.instance, stmt)

		return strings.TrimPrefix(module, stmt.Prefix), module
	}

	name := javaRealPrjName(dir, javaNameThreeDirs)

	if len(stmt.Args) > 0 {
		name = stmt.Args[0].string()
	}

	return name, e.d.unit.ARPrefix + name
}

func (d *ModuleData) finishNativeSwigConfig(env Environment) {
	if d.unit.SwigLang == 0 {
		value, bound := env.lookup("SWIG_LANG")

		if !bound {
			value = "python"
		}

		d.unit.SwigLang = internStr(value)
	}
}

func (d *ModuleData) nativeSwigPeerdirs() []ANY {
	for _, meta := range d.srcs {
		if !extIsSwg(meta.Source.string()) {
			continue
		}

		switch d.unit.SwigLang.string() {
		case "python":
			return []ANY{strContribToolsSwigLibPython.any()}
		case "perl":
			return []ANY{strContribToolsSwigLibPerl5.any(), strBuildPlatformPerl.any()}
		case "":
			throwFmt("gen: SWIG_LANG is empty and _SWIG_PEERDIR is not defined")
		}
	}

	return nil
}
