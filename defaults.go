package main

import (
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

var unitImplicitPeers = []ImplicitPeerRule{
	{
		name: "musl/include",
		peer: "contrib/libs/musl/include",
		predicate: func(rc ImplicitPeerCtx) bool {
			return rc.muslOn
		},
	},
}

var programImplicitPeers = []ImplicitPeerRule{
	{
		name: "glibcasm",
		peer: "contrib/libs/glibcasm",
		predicate: func(rc ImplicitPeerCtx) bool {
			return rc.useAsmlib && !rc.msvc && !rc.pic && !rc.noPlatform &&
				rc.osLinux && rc.archX8664 && !rc.muslOn && !rc.sanitizer && rc.useSSE4
		},
	},
	{
		name: "asmlib",
		peer: "contrib/libs/asmlib",
		predicate: func(rc ImplicitPeerCtx) bool {
			glibcasm := rc.osLinux && rc.archX8664 && !rc.muslOn && !rc.sanitizer && rc.useSSE4

			return rc.useAsmlib && !rc.msvc && !rc.pic && !rc.noPlatform && rc.archX8664 && !glibcasm
		},
	},
	{
		name: "musl/full",
		peer: "contrib/libs/musl/full",
		predicate: func(rc ImplicitPeerCtx) bool {
			return rc.muslOn && !rc.muslLite
		},
	},
	{
		name: "musl",
		peer: "contrib/libs/musl",
		predicate: func(rc ImplicitPeerCtx) bool {
			return rc.muslOn && rc.muslLite
		},
	},
}

var programAllocatorDefaults = []ImplicitPeerRule{
	{
		name: "tcmalloc (TCMALLOC_TC default)",
		peer: "library/cpp/malloc/tcmalloc",
		predicate: func(rc ImplicitPeerCtx) bool {
			return !rc.hadAllocator && rc.osLinux && (rc.muslOn || rc.archX8664)
		},
	},
	{
		name: "tcmalloc/no_percpu_cache (TCMALLOC_TC default)",
		peer: "contrib/libs/tcmalloc/no_percpu_cache",
		predicate: func(rc ImplicitPeerCtx) bool {
			return !rc.hadAllocator && rc.osLinux && (rc.muslOn || rc.archX8664)
		},
	},
	{
		name: "system (J default on WIN32)",
		peer: "library/cpp/malloc/system",
		predicate: func(rc ImplicitPeerCtx) bool {
			return !rc.hadAllocator && rc.win32
		},
	},
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
	ctx, d := e.ctx, e.d
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

	rc := ImplicitPeerCtx{
		flags:      flags,
		noPlatform: noPlatform,
		muslOn:     d.muslEnabled && !noPlatform,
	}

	peers := e.peerScratch[:0]

	defer func() { e.peerScratch = retainMaxLen(e.peerScratch, peers) }()

	if addLinuxHeaders {
		peers = append(peers, "contrib/libs/linux-headers")
	}

	if !flags.NoRuntime && !noPlatform {
		if instance.Path.relString() != "contrib/libs/cxxsupp/libcxx" && !strings.HasPrefix(instance.Path.relString(), "contrib/libs/cxxsupp/libcxx/") {
			peers = append(peers, "contrib/libs/cxxsupp/libcxx")
		}

		if !instance.Platform.windows() {
			if instance.Path.relString() != "contrib/libs/cxxsupp/libcxxrt" {
				peers = append(peers, "contrib/libs/cxxsupp/libcxxrt")
			}

			if instance.Path.relString() != "contrib/libs/libunwind" {
				peers = append(peers, "contrib/libs/libunwind")
			}
		}
	}

	if !flags.NoUtil && !noPlatform {
		if instance.Path.relString() != "util" && !strings.HasPrefix(instance.Path.relString(), "util/") {
			peers = append(peers, "util")
		}
	}

	peers = appendImplicitPeers(peers, unitImplicitPeers, rc)

	if !flags.NoRuntime && !noPlatform && !instance.Platform.windows() && useArcadiaCompilerRuntime(ctx, instance) && instance.Path.relString() != "library/cpp/sanitizer/include" {
		peers = append(peers, "library/cpp/sanitizer/include")
	}

	peers = e.appendPlatformPeerdirs(peers, instance)

	if (Environment{s: &e.frame.ifEnv}).bool(envCommonLinkSettings) && !isProgramModuleType(d.moduleStmt.Name) && !isDllModuleType(d.moduleStmt.Name) && !noPlatform {
		peers = append(peers, e.defaultCommonLinkPeerdirs()...)
	}

	return peers
}

func useArcadiaCompilerRuntime(ctx *GenCtx, instance ModuleInstance) bool {
	if v := instance.Platform.Flags[envUSE_ARCADIA_COMPILER_RUNTIME]; v != 0 {
		return v != strNo
	}

	if v := ctx.target.Flags[envUSE_ARCADIA_COMPILER_RUNTIME]; v != 0 {
		return v != strNo
	}

	return true
}

type ImplicitPeerCtx struct {
	flags         FlagSet
	noPlatform    bool
	muslOn        bool
	muslLite      bool
	osLinux       bool
	win32         bool
	msvc          bool
	archX8664     bool
	pic           bool
	sanitizer     bool
	useSSE4       bool
	useAsmlib     bool
	hadAllocator  bool
	allocatorName string
}

type ImplicitPeerRule struct {
	name      string
	peer      string
	predicate func(ImplicitPeerCtx) bool
}

func appendImplicitPeers(dst []string, rules []ImplicitPeerRule, rc ImplicitPeerCtx) []string {
	for _, r := range rules {
		if r.predicate(rc) {
			dst = append(dst, r.peer)
		}
	}

	return dst
}

func (e *EmitContext) defaultProgramPeerdirsForModule(postUser bool) []string {
	return e.defaultProgramPeerdirsForWithState(postUser)
}

func (e *EmitContext) defaultProgramPeerdirsForWithState(postUser bool) []string {
	_, instance, d := e.ctx, e.instance, e.d

	if d.moduleStmt != nil && isGoExeModuleType(d.moduleStmt.Name) {
		if postUser {
			return nil
		}

		if instance.Platform.ifEnv.bool(envAutocheck) && instance.Platform.OS == OSLinux {
			return append([]string{"build/platform/lld", "contrib/libs/libeatmydata"}, e.cowPeerdirs()...)
		}

		return append([]string{"build/platform/lld"}, e.cowPeerdirs()...)
	}

	flags := d.flags
	allocatorName := d.allocatorName
	env := buildIfEnvInto(&e.ctx.ifEnvScratch, instance)
	muslLite := env.bool(envMUSL_LITE) || d.muslLite

	rc := ImplicitPeerCtx{
		flags:         flags,
		noPlatform:    effectiveNoPlatform(flags),
		muslOn:        d.muslEnabled && !effectiveNoPlatform(flags),
		muslLite:      muslLite,
		osLinux:       env.bool(envOS_LINUX),
		win32:         env.bool(envWIN32),
		msvc:          instance.Platform.MSVC,
		archX8664:     env.bool(envARCH_X86_64),
		pic:           instance.Platform.PIC,
		sanitizer:     instance.Platform.BuildSanitized,
		useSSE4:       d.useSSE4,
		useAsmlib:     d.useAsmlib,
		hadAllocator:  d.hadAllocator,
		allocatorName: allocatorName.string(),
	}

	var peers []string

	if !postUser {
		peers = append(peers, e.defaultCommonLinkPeerdirs()...)
		peers = appendImplicitPeers(peers, programAllocatorDefaults, rc)
	} else {
		if !isDllModuleType(d.moduleStmt.Name) {
			peers = appendImplicitPeers(peers, programImplicitPeers, rc)

			if d.cpuCheck {
				peers = append(peers, "library/cpp/cpuid_check")
			}
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
