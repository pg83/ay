package main

type ldPluginsResult struct {
	Refs  []NodeRef
	Paths []VFS
}

func emitOwnLDPlugins(ctx *genCtx, instance ModuleInstance, plugins []string) *ldPluginsResult {
	if len(plugins) == 0 {
		return nil
	}

	res := &ldPluginsResult{
		Refs:  make([]NodeRef, 0, len(plugins)),
		Paths: make([]VFS, 0, len(plugins)),
	}

	for _, name := range plugins {
		src := Source(instance.Path + "/" + name)
		dst := Build(instance.Path + "/" + name + ".pyplugin")

		ref, ok := ctx.ldPluginCPCache[dst]

		if !ok {
			ref = EmitCP(instance, src, dst, ctx.scripts, ctx.emit)
			ctx.ldPluginCPCache[dst] = ref
		}

		res.Refs = append(res.Refs, ref)
		res.Paths = append(res.Paths, dst)
	}

	return res
}
