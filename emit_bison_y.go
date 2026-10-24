package main

import (
	"path/filepath"
	"strings"
)

var (
	bisonCppSkeletonDirectives = quotedDirectives(bisonCppSkeletonInputs)
	bisonYKV                   = KV{P: pkYC, PC: pcLightGreen}
)

var bisonCppSkeletonInputs = []VFS{
	source("contrib/tools/bison/data/m4sugar/foreach.m4"),
	source("contrib/tools/bison/data/m4sugar/m4sugar.m4"),
	source("contrib/tools/bison/data/skeletons/bison.m4"),
	source("contrib/tools/bison/data/skeletons/c++-skel.m4"),
	source("contrib/tools/bison/data/skeletons/c++.m4"),
	source("contrib/tools/bison/data/skeletons/c-like.m4"),
	source("contrib/tools/bison/data/skeletons/c-skel.m4"),
	source("contrib/tools/bison/data/skeletons/c.m4"),
	source("contrib/tools/bison/data/skeletons/glr.cc"),
	source("contrib/tools/bison/data/skeletons/lalr1.cc"),
	source("contrib/tools/bison/data/skeletons/location.cc"),
	source("contrib/tools/bison/data/skeletons/stack.hh"),
	source("contrib/tools/bison/data/skeletons/variant.hh"),
	source("contrib/tools/bison/data/skeletons/yacc.c"),
}

func bisonGeneratedRel(srcRel, genExt string) string {
	if strings.Contains(srcRel, "/") {
		return "_/" + srcRel + genExt
	}

	return srcRel + genExt
}

func (e *EmitContext) emitBisonProducer(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	srcRel := e.moduleSourceRel(src)
	genExt := d.cc.BisonGenExt
	bisonRef, bisonBin := bisonTool(ctx, instance)
	m4Ref, m4Bin := m4Tool(ctx, instance)
	preprocessHeader := genExt != ".c"
	baseNoExt := strings.TrimSuffix(srcRel, filepath.Ext(srcRel))
	headerRel := baseNoExt + ".h"
	generatedRel := bisonGeneratedRel(srcRel, genExt)
	headerVFS := build(instance.Path.relString(), "/", headerRel)
	generatedVFS := build(instance.Path.relString(), "/", generatedRel)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	localBucket := e.scanner.parsedBucketForInput(srcVFS, parsedIncludesLocal, nil)
	ycRef := ctx.emit.reserve()
	env := na.envList(EnvVar{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}, EnvVar{Name: envBISON_PKGDATADIR, Value: strBisonPkgData.any()}, EnvVar{Name: envM4, Value: m4Bin.any()})
	preprocessEnv := envVarsVCS
	python3 := d.cc.TC.Python3
	scanner := e.scanner
	scanCtx := d.scanCtx
	head := na.anys.alloc(6 + len(d.cc.BisonFlags))[:0]

	head = append(head, bisonBin.any(), argV.any())
	head = appendAnyLists(head, d.cc.BisonFlags)

	head = append(head,
		internV("--defines=", headerVFS.prefix(), headerVFS.relString()).any(),
		argDashO.any(),
		generatedVFS.any(),
		srcVFS.any())

	na.anys.commit(len(head))

	head = head[:len(head):len(head)]

	pe := func() {
		var views []Closure

		if preprocessHeader {
			for _, sk := range bisonCppSkeletonInputs {
				views = append(views, scanner.walkClosure(sk, scanCtx, scanDomainCC))
			}
		}

		inputs := na.inputs.alloc(3 + len(bisonCppSkeletonInputs)*closureBuckets)[:0]

		dedupers.with(func(deduper *DeDuper) {
			tools := na.vfsList(bldContribToolsBisonBison, bldContribToolsM4M4)
			source := na.vfsList(srcVFS)

			for _, v := range tools {
				deduper.add(v.strID())
			}

			deduper.add(srcVFS.strID())
			inputs = append(inputs, tools, source)

			if !preprocessHeader {
				return
			}

			scripts := na.vfs.alloc(1 + len(bisonCppSkeletonInputs))[:0]

			scripts = append(scripts, bisonPreprocessPyVFS)
			scripts = append(scripts, bisonCppSkeletonInputs...)
			na.vfs.commit(len(scripts))
			scripts = scripts[:len(scripts):len(scripts)]

			for _, v := range scripts {
				deduper.add(v.strID())
			}

			inputs = append(inputs, scripts)

			for _, skCV := range views {
				for _, bucket := range skCV.bucketList() {
					if filtered := na.filterSeen(deduper, bucket); len(filtered) > 0 {
						inputs = append(inputs, filtered)
					}
				}
			}
		})

		na.inputs.commit(len(inputs))
		inputs = inputs[:len(inputs):len(inputs)]

		cmds := na.cmds.alloc(2)[:0]

		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(head), Env: env})

		if preprocessHeader {
			cmds = append(cmds, Cmd{
				CmdArgs: na.chunkList(na.anyList(python3.any(),
					bisonPreprocessPyVFS.any(),
					headerVFS.any())),
				Env: preprocessEnv,
			})
		}

		na.cmds.commit(len(cmds))

		cmds = cmds[:len(cmds):len(cmds)]

		e.emitReservedNode(Node{
			Platform:        instance.Platform,
			Cmds:            cmds,
			DepRefs:         na.refList(bisonRef, m4Ref),
			Env:             env,
			Inputs:          inputs,
			BuildFromInputs: inputs[1:],
			Outputs:         na.vfsList(headerVFS, generatedVFS),
			KV:              &bisonYKV,
			Resources:       usesPython3,
		}, ycRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    ycRef,
		GeneratorRefs:  e.ctx.na.refList(bisonRef, m4Ref),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: localBucket},
		OnUse:          pending,
	})

	generatedParsed := na.dirList(append([]IncludeDirective{{kind: includeQuoted, target: includeTarget(headerVFS.rel().any())}}, localBucket...)...)

	e.register(GeneratedFileInfo{
		OutputPath:     generatedVFS,
		ProducerRef:    ycRef,
		GeneratorRefs:  e.ctx.na.refList(bisonRef, m4Ref),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: generatedParsed},
		OnUse:          pending,
	})
}

func (e *EmitContext) emitBisonY(meta SrcMeta) {
	_, instance, d := e.ctx, e.instance, e.d
	src := meta.Source

	e.emitBisonProducer(src)

	generatedRel := bisonGeneratedRel(e.moduleSourceRel(src), d.cc.BisonGenExt)
	generatedVFS := build(instance.Path.relString(), "/", generatedRel)

	meta.Source = generatedVFS.any()
	e.enqueueSrc(meta)
}

func bisonTool(ctx *GenCtx, instance ModuleInstance) (NodeRef, VFS) {
	return ctx.tool(argContribToolsBison)
}

func m4Tool(ctx *GenCtx, instance ModuleInstance) (NodeRef, VFS) {
	return ctx.tool(argContribToolsM4)
}
