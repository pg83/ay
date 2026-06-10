package main

type ldPluginsResult struct {
	Refs  []NodeRef
	Paths []VFS
}

func emitOwnLDPlugins(ctx *genCtx, instance ModuleInstance, plugins []string, tc moduleToolchain) *ldPluginsResult {
	if len(plugins) == 0 {
		return nil
	}

	res := &ldPluginsResult{
		Refs:  make([]NodeRef, 0, len(plugins)),
		Paths: make([]VFS, 0, len(plugins)),
	}

	for _, name := range plugins {
		src := Source(instance.Path.Rel() + "/" + name)
		dst := Build(instance.Path.Rel() + "/" + name + ".pyplugin")

		ref, ok := ctx.ldPluginCPCache[dst]

		if !ok {
			ref = EmitCP(instance, src, dst, tc, ctx.scripts, ctx.emit)
			ctx.ldPluginCPCache[dst] = ref
		}

		res.Refs = append(res.Refs, ref)
		res.Paths = append(res.Paths, dst)
	}

	return res
}
