package main

import "strings"

func (ctx *GenCtx) javaRegisterFbsSources(jm *JavaModule) {
	for _, src := range jm.data.srcs {
		if !strings.HasSuffix(src, ".fbs") {
			continue
		}

		source := resolveSourceOrGeneratedVFS(ctx, jm.instance, src, jm.data.srcDirs)
		out := build(strings.TrimSuffix(source.relString(), ".fbs"), ".fbs.jsrc")

		jm.jsrcs = append(jm.jsrcs, out)

		ref := ctx.emit.reserve()

		pending := ctx.javaPendingEmit(func() {
			tool := ctx.toolResult(argContribLibsFlatbuffersFlatc)
			b := ctx.javaBuilder(jm)
			binary := *tool.LDPath

			b.inputs = append(b.inputs, binary, source)
			b.deps = append(b.deps, tool.LDRef)

			if source.isBuild() {
				b.deps = append(b.deps, b.use(source))
			}

			args := ctx.na.anyList(binary.any(), strJava.any(), argGenMutable.any())

			args = append(args, jm.data.flatcFlags...)
			args = append(args, argI.any(), argB.any(), argI.any(), argS.any())

			for _, inc := range jm.protoInclude {
				args = append(args, argI.any(), inc.any())
			}

			args = append(args, argDashO.any(), build(jm.dir).any(), source.any())
			b.cmd(args, bldRootDirVFS)

			scan := newScanContext(ctx.na, ctx.parsers, nil, nil, nil, includeScannerBasePaths())

			scan.flatcInclude = ctx.na.vfsList(jm.protoInclude...)

			view := ctx.scannerForPlatform(jm.instance.Platform).walkClosure(source, scan, scanDomainFlatc)

			b.views = append(b.views, view)

			for _, bucket := range view.bucketList() {
				for _, input := range bucket {
					b.inputs = append(b.inputs, input)

					if input.isBuild() {
						b.deps = append(b.deps, b.use(input))
					}
				}
			}

			tar := b.python("build/scripts/tar_sources.py")

			tar = append(tar, strInput.any(), build(jm.dir).any(), strOutput.any(), out.any(), strExts.any(), strJava3.any())
			b.cmd(tar, 0)
			b.outputs = []VFS{out}
			b.setKV(pkFJ, pcLightGreen, false)
			b.emitTo(ref)
		})

		ctx.javaReg(jm).register(GeneratedFileInfo{OutputPath: out, SourcePath: source, OwnerModule: jm.path, ProducerRef: ref, ProducerMainOut: out, OnUse: pending})
	}
}
