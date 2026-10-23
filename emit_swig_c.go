package main

import (
	"path/filepath"
	"strings"
)

var (
	swigAddIncls = []VFS{source(swigLibRoot, "/python"), source(swigLibRoot)}
	swigCKV      = KV{P: pkSW, PC: pcYellow}
)

var swigConstArgs = []ANY{
	argIB.any(),
	argIS.any(),
	argISContribToolsSwigLibPython.any(),
	argISContribToolsSwigLib.any(),
}

const swigLibRoot = "contrib/tools/swig/Lib"

type SwigSrc struct {
	Src    string
	Module string
	CMode  bool
}

func (e *EmitContext) emitSwigC() {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	swigRef, swigBin := ctx.tool(argContribToolsSwig)

	for _, stmt := range d.swigC {
		prefix := swigOutputPrefix(stmt.Src, stmt.Module)
		cOutRel := prefix + ".swg" + cythonOutExt(stmt.CMode)
		pyOutRel := prefix + ".py"
		srcVFS := resolveSourceVFS(ctx, instance, stmt.Src, d.srcDirs)
		cOutVFS := build(instance.Path.relString(), "/", cOutRel)
		pyOutVFS := build(instance.Path.relString(), "/", pyOutRel)
		swRef := ctx.emit.reserve()
		moduleName := swigModuleName(stmt.Module)

		var outputInfo *GeneratedFileInfo

		pe := func() {
			cv := e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainSwig)
			cvBuckets := cv.bucketList()
			inputs := na.inputs.alloc(2 + len(cvBuckets))[:2+len(cvBuckets)]

			inputs[0] = na.vfsList(bldContribToolsSwigSwig)
			inputs[1] = na.vfsList(srcVFS)
			copy(inputs[2:], cvBuckets)
			na.inputs.commit(len(inputs))

			inputChunks := InputChunks(inputs[:len(inputs):len(inputs)])
			swigClosure := collectBucketVFS(ctx.na, cvBuckets, func(VFS) bool { return true })

			outputInfo.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: collectSwigInducedIncludes(e.scanner, srcVFS, swigClosure)}

			var cppArgs []ANY

			if !stmt.CMode {
				cppArgs = na.anyList(strC5.any())
			}

			cmdArgs := na.chunkList(na.anyList(swigBin.any()), swigConstArgs, cppArgs, na.anyList(argPython.any(), argModule.any(), internStr(moduleName).any(),
				argInterface.any(),
				internV(moduleName, "_swg").any(),
				argDashO.any(),
				cOutVFS.any(),
				srcVFS.any()))

			e.emitReservedNode(Node{
				Platform: instance.Platform,
				Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
					Env: envVarsVCS}),
				DepRefs:         na.refList(swigRef),
				Env:             envVarsVCS,
				Inputs:          inputChunks,
				BuildFromInputs: inputChunks[1:],
				InputClosures:   []Closure{cv},
				Outputs:         na.vfsList(cOutVFS, pyOutVFS),
				KV:              &swigCKV,
			}, swRef)
		}

		pending := e.pendingEmit(pe)

		outputInfo = e.register(GeneratedFileInfo{
			OutputPath:    cOutVFS,
			ProducerRef:   swRef,
			GeneratorRefs: e.ctx.na.refList(swigRef),
			OnUse:         pending,
		})

		e.register(GeneratedFileInfo{
			OutputPath:      pyOutVFS,
			ProducerRef:     swRef,
			ProducerMainOut: cOutVFS,
			GeneratorRefs:   e.ctx.na.refList(swigRef),
			OnUse:           pending,
		})

		key := stmt.Module

		if !d.usePython2 {
			key = strings.ReplaceAll(key, ".", "/") + ".py"
		}

		e.enqueueSrc(SrcMeta{
			Source: pyOutVFS.any(), Prio: stmtPrioDefault,
			PyMeta: e.addPyMeta(PySourceMeta{
				Module: internStr(key),
				Token:  internV("${ARCADIA_BUILD_ROOT}/", pyOutVFS.relString()).any(),
				Kind:   pySourceGenerated,
			}),
		})

		e.enqueueSrc(SrcMeta{Source: cOutVFS.any(), Prio: stmtPrioDefault, Bucket: bkSwig})
	}
}

func swigOutputPrefix(src, module string) string {
	dir := filepath.ToSlash(filepath.Dir(src))

	if dir == "." {
		return swigModuleName(module)
	}

	return dir + "/" + swigModuleName(module)
}

func swigModuleName(module string) string {
	if dot := strings.LastIndexByte(module, '.'); dot >= 0 {
		return module[dot+1:]
	}

	return module
}

func collectSwigInducedIncludes(scanner *IncludeScanner, src VFS, closure []VFS) []IncludeDirective {
	swigParser := IncludeDirectiveParser(SwigIncludeDirectiveParser{})

	var out []IncludeDirective

	add := func(v VFS) {
		out = append(out, scanner.parsedBucketForInput(v, parsedIncludesCpp, swigParser)...)
	}

	add(src)

	for _, v := range closure {
		add(v)
	}

	return out
}
