package main

import (
	"slices"
	"strings"
)

var runtimeAncestorPaths = map[string]bool{
	"contrib/libs/musl":                    true,
	"contrib/libs/libc_compat":             true,
	"contrib/libs/linuxvdso":               true,
	"contrib/libs/linuxvdso/original":      true,
	"contrib/libs/cxxsupp/builtins":        true,
	"contrib/libs/cxxsupp/libcxx":          true,
	"contrib/libs/cxxsupp/libcxxrt":        true,
	"contrib/libs/cxxsupp/libcxxabi":       true,
	"contrib/libs/cxxsupp/libcxxabi-parts": true,
	"contrib/libs/libunwind":               true,
	"library/cpp/malloc/api":               true,
	"library/cpp/sanitizer/include":        true,
	"util":                                 true,
}

var runtimeAncestorCxxConsumers = map[string]bool{
	"library/cpp/malloc/api": true,
}

func isRuntimeAncestor(path string) bool {
	return runtimeAncestorPaths[path]
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
	flags := d.flags
	noPlatform := effectiveNoPlatform(flags)

	addLinuxHeaders := instance.Platform.OS == OSLinux && d.setVars.get(strNeedPlatformPeerdirs) != strNo &&
		instance.Path.relString() != "contrib/libs/linux-headers" &&
		!strings.HasPrefix(instance.Path.relString(), "contrib/libs/linux-headers/")

	if d.moduleStmt != nil && isGoModuleType(d.moduleStmt.Name) {
		peers := []string{"build/platform/python/ymake_python3"}

		if instance.Platform.UsesSDKRoot {
			peers = append(peers, "build/platform/linux_sdk")
		}

		if addLinuxHeaders {
			peers = append(peers, "contrib/libs/linux-headers")
		}

		if !flags.NoRuntime && !noPlatform {
			peers = append(peers, "contrib/libs/cxxsupp")
		}

		if !flags.NoUtil && !noPlatform {
			peers = append(peers, "util")
		}

		return append(peers, "build/platform/clang")
	}

	if instance.Language != LangCPP {
		if addLinuxHeaders {
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

func (e *EmitContext) goProgramPeerdirs() []string {
	if e.instance.Platform.ifEnv.bool(envAutocheck) && e.instance.Platform.OS == OSLinux {
		return append([]string{"build/platform/lld", "contrib/libs/libeatmydata"}, e.cowPeerdirs()...)
	}

	return append([]string{"build/platform/lld"}, e.cowPeerdirs()...)
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
