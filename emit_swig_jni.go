package main

import (
	"fmt"
	"path"
	"strings"
)

var swigJNIKV = KV{P: pkSW, PC: pcYellow}

func swigJNIHead(python3, swigBin, src VFS, srcRel string) []ANY {
	stem := strings.TrimSuffix(srcRel, path.Ext(srcRel))

	return []ANY{
		python3.any(), swigJNIScriptVFS.any(),
		argSwig.any(), swigBin.any(),
		argDefaultModule.any(), internStr(path.Base(stem)).any(),
		argSrc.any(), src.any(),
	}
}

func swigJNIPackageArgs(src VFS) []ANY {
	return []ANY{argPackageByFile.any(), internV("ru/yandex/", src.relString()).any()}
}

func swigJNIIncludeArgs(include []VFS) []ANY {
	out := make([]ANY, 0, 3+len(include))

	out = append(out, arg2.any(), argIB.any(), argIS.any())

	for _, v := range include {
		out = append(out, internV("-I", v.string()).any())
	}

	return out
}

func swigWrapOutputs(moduleDir, srcRel string) (VFS, VFS) {
	stem := strings.TrimSuffix(srcRel, path.Ext(srcRel))

	return build(moduleDir, "/", stem, "_wrap.swg.h"), build(moduleDir, "/", stem, "_wrap.swg.cpp")
}

func (e *EmitContext) emitLibrarySwigSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	src := meta.Source

	if d.unit.SwigLang != strSwigLangJniCpp {
		ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", instance.Path.relString(), src.string()),
		})

		return
	}

	na := ctx.na
	srcRel := e.moduleSourceRel(src)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	hdr, cpp := swigWrapOutputs(instance.Path.relString(), srcRel)
	swigRef, swigBin := ctx.tool(argContribToolsSwig)
	swRef := ctx.emit.reserve()
	cv := e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainSwig)
	cvBuckets := cv.bucketList()
	swigClosure := collectBucketVFS(na, cvBuckets, func(VFS) bool { return true })
	induced := collectSwigInducedIncludes(e.scanner, srcVFS, swigClosure)
	include := d.cc.SwigInclude
	python3 := d.tc.Python3

	meta.Source = cpp.any()
	meta.Compile.CFlags = append(e.ctx.na.anyList(meta.Compile.CFlags...), argWnoDeprecatedDeclarations.any())
	e.enqueueSrc(meta)

	pe := func() {
		args := swigJNIHead(python3, swigBin, srcVFS, srcRel)

		args = append(args, argOutHeader.any(), hdr.any())
		args = append(args, swigJNIPackageArgs(srcVFS)...)
		args = append(args, swigJNIIncludeArgs(include)...)
		args = append(args, argDashO.any(), cpp.any())

		inputs := make(InputChunks, 0, 1+len(cvBuckets))

		inputs = append(inputs, na.vfsList(swigBin, swigJNIScriptVFS, srcVFS))
		inputs = append(inputs, cvBuckets...)

		e.emitReservedNode(Node{
			Platform:       instance.Platform,
			Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: envVarsVCS}),
			Env:            envVarsVCS,
			Inputs:         inputs,
			Outputs:        na.vfsList(hdr, cpp),
			KV:             &swigJNIKV,
			ForeignDepRefs: na.refList(swigRef),
			Resources:      usesPython3,
		}, swRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     hdr,
		ProducerRef:    swRef,
		GeneratorRefs:  na.refList(swigRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: induced},

		OnUse: pending,
	})

	cppParsed := make([]IncludeDirective, 0, 1+len(induced))

	cppParsed = append(cppParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(hdr.rel().any())})
	cppParsed = append(cppParsed, induced...)

	e.register(GeneratedFileInfo{
		OutputPath:      cpp,
		ProducerRef:     swRef,
		ProducerMainOut: hdr,
		GeneratorRefs:   na.refList(swigRef),
		ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: cppParsed},

		OnUse: pending,
	})
}

func (jm *JavaModule) swigInclude() []VFS {
	var out []VFS

	for _, p := range jm.unmanageable {
		out = javaPushUniq(out, p.res.SwigInclude...)
	}

	return out
}

func (ctx *GenCtx) javaRegisterSwigJsrcs(jm *JavaModule) {
	d := jm.data
	reg := ctx.javaReg(jm)

	for _, src := range d.swigSrcs() {
		srcVFS := resolveSourceVFS(ctx, jm.instance, src, d.srcDirs)
		jsrc := build(jm.dir, "/", src, ".jsrc")
		ref := ctx.emit.reserve()

		reg.register(GeneratedFileInfo{
			OutputPath:      jsrc,
			OwnerModule:     jm.path,
			ProducerRef:     ref,
			ProducerMainOut: jsrc,
			OnUse:           ctx.javaPendingEmit(func() { ctx.javaEmitSwigJsrc(jm, src, srcVFS, jsrc, ref) }),
		})

		jm.jsrcs = append(jm.jsrcs, jsrc)
	}
}

func (ctx *GenCtx) javaEmitSwigJsrc(jm *JavaModule, src string, srcVFS, jsrc VFS, ref NodeRef) {
	b := ctx.javaBuilder(jm)
	swigRef, swigBin := ctx.tool(argContribToolsSwig)
	scanCtx := newScanContext(ctx.na, ctx.parsers, nil, nil, nil, includeScannerBasePaths())

	scanCtx.swigInclude = ctx.na.vfsList(jm.swigInclude()...)

	args := swigJNIHead(javaPython3, swigBin, srcVFS, src)

	b.useResource("YMAKE_PYTHON3")
	args = append(args, swigJNIPackageArgs(srcVFS)...)
	args = append(args, argJsrc.any(), jsrc.any())
	args = append(args, swigJNIIncludeArgs(scanCtx.swigInclude)...)
	args = append(args, argDashO.any(), build(jm.dir, "/unused.cpp").any())

	b.cmd(args, 0)
	b.input(swigBin, swigJNIScriptVFS, srcVFS)
	b.deps = append(b.deps, swigRef)

	for _, bucket := range ctx.scannerForPlatform(jm.instance.Platform).walkClosure(srcVFS, scanCtx, scanDomainSwig).bucketList() {
		b.input(bucket...)

		if !bucket[0].isBuild() {
			continue
		}

		for _, v := range bucket {
			if dep := b.use(v); dep != 0 {
				b.deps = append(b.deps, dep)
			}
		}
	}

	b.setKV(pkSW, pcYellow, false)
	b.outputs = []VFS{jsrc}
	b.emitTo(ref)
}
