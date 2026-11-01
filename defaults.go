package main

import (
	"slices"
	"strings"
)

var runtimeAncestorCxxConsumers = map[string]bool{
	"library/cpp/malloc/api": true,
}

func (e *EmitContext) defaultPeerdirsForModule() []string {
	_, instance, d := e.ctx, e.instance, e.d

	if isRustModule(d.moduleStmt.Name) {
		return []string{"build/platform/python/ymake_python3"}
	}

	inst := instance

	if instance.Language == LangPy && d.usePython3 &&
		d.moduleStmt.Name == tokProtoLibrary && moduleExcludesTag(d, protoModuleTag(LangCPP, d.moduleStmt.Schema)) {
		inst.Language = LangCPP
	}

	if instance.Language == LangPy2 || instance.Language == LangPy && d.multimodule == tokFbsLibrary {
		inst.Language = LangCPP
	}

	return e.defaultPeerdirsForWithState(inst)
}

func (e *EmitContext) defaultPeerdirsForWithState(instance ModuleInstance) []string {
	d := e.d

	if !isGoModuleType(d.moduleStmt.Name) && instance.Language != LangCPP {
		if instance.Platform.OS == OSLinux && d.setVars.get(strNeedPlatformPeerdirs) != strNo &&
			instance.Path.relString() != "contrib/libs/linux-headers" && !strings.HasPrefix(instance.Path.relString(), "contrib/libs/linux-headers/") {
			return []string{"contrib/libs/linux-headers"}
		}

		return nil
	}

	return e.confPeerdirs(instance)
}

func (e *EmitContext) confPeerdirs(instance ModuleInstance) []string {
	peers := e.peerScratch[:0]

	defer func() { e.peerScratch = retainMaxLen(e.peerScratch, peers) }()

	self := instance.Path.relString()

	for peer := range strings.FieldsSeq((Environment{s: &e.frame.ifEnv}).expand("$PEERDIR")) {
		if peer != self && !slices.Contains(peers, peer) {
			peers = append(peers, peer)
		}
	}

	return peers
}

func (e *EmitContext) appendPlatformPeerdirs(peers []string, instance ModuleInstance) []string {
	d := e.d

	if strings.HasPrefix(instance.Path.relString(), "build/platform/") {
		return peers
	}

	if d.setVars.get(strNeedPlatformPeerdirs) == strNo {
		return append(peers, "build/platform/python/ymake_python3")
	}

	peers = append(peers, "build/platform/clang")

	if isLinkingModuleType(d.unit.Type) && instance.Platform.ifEnv.bool(envCLANG) {
		switch linker := instance.Platform.ifEnv.string(envDefaultLinkerId); linker {
		case "lld", "gold", "mold":
			peers = append(peers, "build/platform/"+linker)
		}
	}

	peers = append(peers, "build/platform/python/ymake_python3")

	return append(peers, instance.Platform.CompilerPlatform...)
}

func (e *EmitContext) defaultCommonLinkPeerdirs() []string {
	instance, d := e.instance, e.d

	if d.setVars.get(strCommonLinkSettings) == strNo {
		return nil
	}

	var peers []string

	if instance.Platform.OS == OSLinux && instance.Platform.ifEnv.bool(envAutocheck) {
		peers = append(peers, "contrib/libs/libeatmydata")
	}

	if d.useArcadiaLibm && instance.Path.relString() != "contrib/libs/libm" && !strings.HasPrefix(instance.Path.relString(), "contrib/libs/libm/") {
		peers = append(peers, "contrib/libs/libm")
	}

	return append(peers, e.cowPeerdirs()...)
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
