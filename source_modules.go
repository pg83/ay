package main

import "strings"

func isPythonMultimodule(name TOK) bool {
	return name == tokPy23Library || name == tokPy23NativeLibrary || name == tokSandboxPy23Task
}

func configureCythonDirectory(ctx *GenCtx, instance ModuleInstance) *SourceModuleInfo {
	name := ctx.moduleName(instance.Path.relString())

	instance.Demand = demandNone

	if instance.Language == LangPy && name != tokProtoLibrary {
		instance.Language = LangCPP
	}

	if !isPythonMultimodule(name) {
		return configureCythonModule(ctx, instance, "")
	}

	var selected *SourceModuleInfo

	if name == tokSandboxPy23Task {
		for _, module := range sandboxPythonTaskInstances(instance, name) {
			info := configureCythonModule(ctx, module, sandboxPythonTaskVariant(name, module.Kind, module.Language))

			if module.Kind == instance.Kind && module.Language == instance.Language {
				selected = info
			}
		}

		return selected
	}

	for _, variant := range []struct {
		language Language
		tag      string
	}{{LangPy2, "PY2"}, {LangCPP, "PY3"}} {
		module := instance

		module.Language = variant.language

		info := configureCythonModule(ctx, module, variant.tag)

		if module.Language == instance.Language {
			selected = info
		}
	}

	return selected
}

func configureCythonModule(ctx *GenCtx, instance ModuleInstance, tag string) *SourceModuleInfo {
	scanner := ctx.scannerFor(instance)
	info, configured := scanner.codegen.sourceModule(instance)

	if configured {
		return info
	}

	frame := ctx.pushFrame()

	defer ctx.popFrame()

	env := buildIfEnvInto(&frame.ifEnv, instance)
	d := collectModuleInto(ctx, instance, moduleStmts(ctx, instance.Path.relString()), env, ctx.onWarn, &frame.d)

	if d.multimodule == tokSandboxPy23Task && instance.Kind == KindBin {
		info.ExcludedVariants = map[STR]bool{}

		for _, sub := range sandboxPythonTaskInstances(instance, d.multimodule) {
			variant := sandboxPythonTaskVariant(d.multimodule, sub.Kind, sub.Language)

			info.ExcludedVariants[internStr(variant)] = moduleExcludesTag(d, variant)
		}

		for _, excluded := range strings.Fields(instance.Platform.ifEnv.string(envExcludeSubmodules)) {
			info.ExcludedVariants[internStr(excluded)] = true
		}
	}

	excluded := moduleExcludesTag(d, tag)

	if d.multimodule == tokSandboxPy23Task {
		excluded = sandboxPythonTaskExcluded(ctx, instance, tag)
	}

	if d.discarded || d.moduleStmt == nil || tag != "" && excluded {
		return info
	}

	applyImplicitPeerdirs(ctx, instance, d)

	e := newEmitContextIn(frame, ctx, instance, d, nil)

	var peerIncludes, peerCythonIncludes []VFS

	peers := append(internAnys(e.defaultPeerdirsForModule()), d.peerdirs...)

	for _, peer := range peers {
		peerPath := source(peer.string())

		if !peerYaMakeExists(ctx.fs, peerPath) {
			continue
		}

		child := configureCythonDirectory(ctx, e.derivePeerInstanceVFS(peerPath))

		if child != nil {
			peerIncludes = append(peerIncludes, child.Includes...)
			peerCythonIncludes = append(peerCythonIncludes, child.CythonIncludes...)
		}
	}

	info.Includes = ctx.na.vfsList(dedup(d.addInclGlobal, peerIncludes)...)
	info.CythonIncludes = ctx.na.vfsList(dedup(d.cythonAddInclGlobal, peerCythonIncludes)...)

	scan := newModuleScanContext(ctx, instance, d, ctx.na.vfsList(dedup(d.addIncl, d.addInclGlobal)...), peerIncludes, peerIncludes, nil)

	scan.cythonPeerIncl = peerCythonIncludes

	visit := func(input VFS) {
		if input.isSource() && isCythonLangFile(input.relString()) && ctx.fs.isFile(srcRootRel, input.relString()) {
			scanner.cythonPyxLangClosure(input, scan)
		}
	}

	for _, stmt := range d.cythonCpp {
		visit(resolveSourceVFS(ctx, instance, stmt.Src, d.srcDirs))

		if pxd, ok := resolveCythonPxd(ctx, instance, stmt.Pxd); ok {
			visit(pxd)
		}
	}

	for _, src := range d.srcs {
		visit(copyFileInputVFS(ctx.fs, instance.Path, src.Source.string()))
	}

	for _, stmt := range d.runPrograms {
		for _, input := range stmt.INFiles[:len(stmt.INFiles)-stmt.INNoParse] {
			visit(copyFileInputVFS(ctx.fs, instance.Path, input.string()))
		}
	}

	for _, stmt := range d.runPython {
		for _, input := range stmt.INFiles[:len(stmt.INFiles)-stmt.INNoParse] {
			visit(copyFileInputVFS(ctx.fs, instance.Path, input.string()))
		}
	}

	if tag != "" {
		order := e.scheduleResourceActions()

		info.ResourceActions = &resourceActionState{order: order, sources: e.resourceSources, available: e.resourceAvailable}
	}

	return info
}
