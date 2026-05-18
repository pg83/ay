package main

// emitOwnLDPlugins emits one CP node per `LD_PLUGIN(name.py)` entry,
// copying `$(S)/<modulePath>/<name>` to `$(B)/<modulePath>/<name>.pyplugin`.
// Returns parallel ref + path slices in declaration order.
//
// CP NodeRefs are deduped via `genCtx.ldPluginCPCache` keyed by output
// path: without it the host walk re-fires on the same plugin and
// produces a duplicate node (Platform participates in the canonical
// hash). First-emit wins — the seed runs target-first, so the cached
// entry carries the target platform.
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
			ref = EmitCP(instance, src, dst, ctx.emit)
			ctx.ldPluginCPCache[dst] = ref
		}

		res.Refs = append(res.Refs, ref)
		res.Paths = append(res.Paths, dst)
	}

	return res
}
