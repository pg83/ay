package main

type FbsFlatcRecipe struct {
	flags []ANY
	emit  *PendingEmit
	used  bool
}

type FbsNativeRecipe struct {
	owned bool
	local fbsNativeContext
}

type fbsNativeContext struct {
	serial             uint32
	env                *ModuleCompileEnv
	compilerEnv        Environment
	cFlags             []ANY
	cxxFlags           []ANY
	cOnlyFlags         []ANY
	clangWarnings      []ANY
	moduleScopeCFlags  []ANY
	perSourceCFlags    []ANY
	noOptimize         bool
	noCompilerWarnings bool
	noWShadow          bool
	noBaseUnit         bool
}

func (e *EmitContext) fbsNativeContext(in ModuleCCInputs) fbsNativeContext {
	d := e.d

	return fbsNativeContext{
		serial:             e.frame.serial,
		compilerEnv:        d.cc.CompilerEnv,
		cFlags:             d.cFlags,
		cxxFlags:           d.cxxFlags,
		cOnlyFlags:         d.cOnlyFlags,
		clangWarnings:      d.clangWarnings,
		moduleScopeCFlags:  d.moduleScopeCFlags,
		perSourceCFlags:    in.PerSourceCFlags,
		noOptimize:         d.cc.NoOptimize,
		noCompilerWarnings: d.flags.NoCompilerWarnings,
		noWShadow:          d.flags.NoWShadow,
		noBaseUnit:         e.instance.Language == LangFbsJavaNative,
	}
}

func (e *EmitContext) touchFbsNativeInput(info *GeneratedFileInfo) {
	if recipe := info.fbsNative(); recipe != nil {
		recipe.local.serial = e.frame.serial
		recipe.local.env = &e.d.cc
	}
}

func (e *EmitContext) bindFbsNativeSource(src, out VFS, in ModuleCCInputs, reserved NodeRef) (NodeRef, VFS) {
	ctx := e.ctx
	info := e.codegen.lookup(out)

	if info == nil {
		info = e.register(GeneratedFileSpec{OutputPath: out, SourcePath: src, ProducerRef: reserved, FbsNative: &FbsNativeRecipe{}})
	}

	recipe := info.fbsNative()

	recipe.local = e.fbsNativeContext(in)

	if !recipe.owned {
		recipe.owned = true

		owner := e.frame.serial
		scanner, scanCtx := e.scanner, e.d.scanCtx

		info.OnUse = e.pendingEmit(func() {
			command := in

			command.IncludeView = scanner.walkClosure(src, scanCtx, scanDomainCC)

			if local := &recipe.local; local.serial != owner {
				env := *in.ModuleCompileEnv

				if local.env != nil {
					env.AddIncl = local.env.AddIncl
					env.PeerAddInclGlobal = local.env.PeerAddInclGlobal
				} else {
					env.CompilerEnv = local.compilerEnv
					env.CFlags = local.cFlags
					env.CXXFlags = local.cxxFlags
					env.COnlyFlags = local.cOnlyFlags
					env.ClangWarnings = local.clangWarnings
					env.NoOptimize = local.noOptimize
					env.ModuleScopeCFlags = local.moduleScopeCFlags
					env.Flags.NoCompilerWarnings = local.noCompilerWarnings
					env.Flags.NoWShadow = local.noWShadow
					env.NoBaseUnit = local.noBaseUnit
				}

				blocks := composeCCModuleArgBlocks(ctx.na, e.instance.Platform, &env)

				command.ModuleCompileEnv = &env
				command.CFlags = env.CFlags
				command.CCBlocks = &blocks
				command.PerSourceCFlags = local.perSourceCFlags
			}

			e.composeCCNodeAt(src, out, command, ctx.host, info.ProducerRef)
		})
	}

	return info.ProducerRef, out
}

func (ctx *GenCtx) flushFbsFlatcRecipes() bool {
	emitted := false

	for _, scanner := range []*IncludeScanner{ctx.scannerTarget, ctx.scannerHost, ctx.scannerTargetPIC} {
		if scanner == nil {
			continue
		}

		for _, recipe := range scanner.codegen.attributes.fbsFlatc.values() {
			if recipe.used && recipe.emit.fn != nil {
				emitted = true

				recipe.emit.fire()
			}
		}
	}

	return emitted
}
