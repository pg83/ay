package main

type fbsNativeBinding struct {
	ref     NodeRef
	owner   *EmitContext
	local   *EmitContext
	localIn ModuleCCInputs
	emit    *PendingEmit
}

type fbsFlatcBinding struct {
	ref   NodeRef
	flags []ANY
	emit  func()
	used  bool
}

type fbsFileKey struct {
	path     VFS
	platform *Platform
}

type fbsContextBindings struct {
	native      map[fbsFileKey]*fbsNativeBinding
	nativeOrder []*fbsNativeBinding
	flatc       map[fbsFileKey]*fbsFlatcBinding
	flatcOrder  []*fbsFlatcBinding
	pins        []*PendingEmit
}

func (e *EmitContext) bindFbsNativeSource(src VFS, in ModuleCCInputs, reserved NodeRef) (NodeRef, VFS) {
	ctx := e.ctx
	out, _ := composeCCPaths(e.instance, trimModulePrefix(src.relString(), e.instance.Path.relString()), src, in, ccObjectSuffix(e.instance, in))
	bindings := &ctx.fbsBindings

	if bindings.native == nil {
		bindings.native = make(map[fbsFileKey]*fbsNativeBinding)
	}

	key := fbsFileKey{out, e.instance.Platform}
	binding := bindings.native[key]

	if binding == nil {
		if reserved == 0 {
			reserved = ctx.emit.reserve()
		}

		binding = &fbsNativeBinding{ref: reserved}

		bindings.native[key] = binding
		bindings.nativeOrder = append(bindings.nativeOrder, binding)
	}

	binding.local = e
	binding.localIn = in

	bindings.pins = append(bindings.pins, e.pendingEmit(func() {}))

	if e.instance.Language != LangFbsJavaNative && binding.owner == nil {
		binding.owner = e

		binding.emit = e.pendingEmit(func() {
			command := in

			if binding.local != binding.owner {
				local := binding.local.d
				env := *in.ModuleCompileEnv

				env.CompilerEnv = local.cc.CompilerEnv
				env.CFlags = local.cFlags
				env.CXXFlags = local.cxxFlags
				env.COnlyFlags = local.cOnlyFlags
				env.ClangWarnings = local.clangWarnings
				env.NoOptimize = local.cc.NoOptimize
				env.ModuleScopeCFlags = local.moduleScopeCFlags
				env.Flags.NoCompilerWarnings = local.flags.NoCompilerWarnings
				env.Flags.NoWShadow = local.flags.NoWShadow
				env.NoBaseUnit = binding.local.instance.Language == LangFbsJavaNative

				blocks := composeCCModuleArgBlocks(ctx.na, e.instance.Platform, &env)

				command.ModuleCompileEnv = &env
				command.CFlags = env.CFlags
				command.CCBlocks = &blocks
				command.PerSourceCFlags = binding.localIn.PerSourceCFlags
			}

			e.composeCCNodeAt(src, command, ctx.host, binding.ref)
		})
	}

	return binding.ref, out
}

func (ctx *GenCtx) fbsFlatcBinding(platform *Platform, out VFS) *fbsFlatcBinding {
	bindings := &ctx.fbsBindings

	if bindings.flatc == nil {
		bindings.flatc = make(map[fbsFileKey]*fbsFlatcBinding)
	}

	key := fbsFileKey{out, platform}
	binding := bindings.flatc[key]

	if binding == nil {
		binding = &fbsFlatcBinding{ref: ctx.emit.reserve()}

		bindings.flatc[key] = binding
		bindings.flatcOrder = append(bindings.flatcOrder, binding)
	}

	return binding
}

func (ctx *GenCtx) flushFbsContextBindings() {
	for _, binding := range ctx.fbsBindings.nativeOrder {
		binding.emit.fire()
	}

	for _, binding := range ctx.fbsBindings.flatcOrder {
		if binding.used {
			binding.emit()
		}
	}

	for _, pin := range ctx.fbsBindings.pins {
		pin.fire()
	}
}
