package main

import (
	"slices"
	"strings"
)

var runtimeAncestorCxxConsumers = map[string]bool{
	"library/cpp/malloc/api": true,
}

func (e *EmitContext) confSelfPeerInstances() []ModuleInstance {
	d, instance := e.d, e.instance

	if d.confModule == nil || len(d.confModule.selfPeers) == 0 {
		return nil
	}

	shapes := multimoduleShapes[d.confModule.owner]
	ignored := strings.Fields(Environment{s: &e.frame.ifEnv}.string(envIgnorePeerdirSelf))
	variants := instance.Platform.conf.peerVariants(d.confModule.owner)

	var out []ModuleInstance

	for _, sub := range d.confModule.selfPeers {
		at := slices.IndexFunc(variants, func(v confPeerVariant) bool { return v.sub == sub })

		if at < 0 || slices.Contains(ignored, variants[at].tag.string()) {
			continue
		}

		shape, ok := shapes[sub]

		if !ok {
			throwFmt("gen: %s: .PEERDIRSELF names %s of %s, which ay does not model", instance.Path.relString(), sub.string(), d.confModule.owner.string())
		}

		self := instance

		self.Kind, self.Language, self.YqlCurrent = shape.kind, shape.language, shape.yqlCurrent
		out = append(out, self)
	}

	return out
}

func (e *EmitContext) confSelfPeers() []ResolvedPeer {
	var out []ResolvedPeer

	for _, self := range e.confSelfPeerInstances() {
		if result := e.genPeerModule(self); !result.excluded {
			out = append(out, ResolvedPeer{path: e.instance.Path.relString(), result: result, kind: peerKindImplicit})
		}
	}

	return out
}

func effectiveNoPlatform(f FlagSet) bool {
	if f.NoPlatform {
		return true
	}

	return f.NoLibc && f.NoRuntime && f.NoUtil
}

func peerYaMakeExists(fs FS, peerDir VFS) bool {
	return fs.isFile(peerDir.rel(), "ya.make")
}
