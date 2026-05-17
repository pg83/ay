package main

// emitOwnLDPlugins emits one CP node per `LD_PLUGIN(name.py)` entry.
// CP src = `$(S)/<modulePath>/<name>`, dst =
// `$(B)/<modulePath>/<name>.pyplugin` (verified against REF for
// `contrib/libs/musl/include`'s `musl.py`). Returns parallel ref + path
// slices in declaration order.
//
// The CP NodeRef is cached on `genCtx.ldPluginCPCache` keyed by output
// path: REF emits each CP node once (on the target platform) and shares
// its UID across target and host LD deps; without dedup the host walk
// re-fires `emitOwnLDPlugins` on the same plugin and produces a
// duplicate on `default-linux-x86_64` (Platform is part of the
// canonical hash). First-emit wins — the seed runs target-first, so
// the cached entry carries the target platform per REF.
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
