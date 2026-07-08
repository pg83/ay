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
			return rc.useAsmlib && !rc.pic && !rc.noPlatform &&
				rc.osLinux && rc.archX8664 && !rc.muslOn && !rc.sanitizer && rc.useSSE4
		},
	},
	{
		name: "asmlib",
		peer: "contrib/libs/asmlib",
		predicate: func(rc ImplicitPeerCtx) bool {
			glibcasm := rc.osLinux && rc.archX8664 && !rc.muslOn && !rc.sanitizer && rc.useSSE4

			return rc.useAsmlib && !rc.pic && !rc.noPlatform && rc.archX8664 && !glibcasm
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

var archDependentPeerAddInclPrefixes = []string{
	"contrib/libs/musl/arch/",
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
}

func isAncestorPath(srcDir, instancePath string) bool {
	if srcDir == instancePath {
		return true
	}

	return strings.HasPrefix(instancePath, srcDir+"/")
}

func isRuntimeAncestor(path string) bool {
	return runtimeAncestorPaths[path]
}

func suppressMallocAPIDefault(defaults []string, allocatorName ANY) []string {
	if allocatorName != strFAKE.any() {
		return defaults
	}

	out := make([]string, 0, len(defaults))

	for _, p := range defaults {
		if p == "library/cpp/malloc/api" {
			continue
		}

		out = append(out, p)
	}

	return out
}

func (e *EmitContext) defaultPeerdirsForModule() []string {
	_, instance, d := e.ctx, e.instance, e.d
	inst := instance

	if instance.Language == LangPy && d.usePython3 &&
		d.moduleStmt.Name == tokProtoLibrary && moduleExcludesTag(d, "CPP_PROTO") {
		inst.Language = LangCPP
	}

	return e.defaultPeerdirsForWithState(inst)
}

func (e *EmitContext) defaultPeerdirsForWithState(instance ModuleInstance) []string {
	ctx, d := e.ctx, e.d
	flags := d.flags
	noPlatform := effectiveNoPlatform(flags)

	addLinuxHeaders := instance.Path.relString() != "contrib/libs/linux-headers" &&
		!strings.HasPrefix(instance.Path.relString(), "contrib/libs/linux-headers/")

	if d.moduleStmt != nil && isGoModuleType(d.moduleStmt.Name) {
		if addLinuxHeaders {
			return []string{"contrib/libs/linux-headers"}
		}

		return nil
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

	peers := make([]string, 0, 16)

	if addLinuxHeaders {
		peers = append(peers, "contrib/libs/linux-headers")
	}

	if !flags.NoRuntime && !noPlatform {
		if instance.Path.relString() != "contrib/libs/cxxsupp/libcxx" && !strings.HasPrefix(instance.Path.relString(), "contrib/libs/cxxsupp/libcxx/") {
			peers = append(peers, "contrib/libs/cxxsupp/libcxx")
		}

		if instance.Path.relString() != "contrib/libs/cxxsupp/libcxxrt" {
			peers = append(peers, "contrib/libs/cxxsupp/libcxxrt")
		}

		if instance.Path.relString() != "contrib/libs/libunwind" {
			peers = append(peers, "contrib/libs/libunwind")
		}
	}

	if !flags.NoUtil && !noPlatform {
		if instance.Path.relString() != "util" && !strings.HasPrefix(instance.Path.relString(), "util/") {
			peers = append(peers, "util")
		}
	}

	peers = appendImplicitPeers(peers, unitImplicitPeers, rc)

	if !flags.NoRuntime && !noPlatform && useArcadiaCompilerRuntime(ctx, instance) && instance.Path.relString() != "library/cpp/sanitizer/include" {
		peers = append(peers, "library/cpp/sanitizer/include")
	}

	if !strings.HasPrefix(instance.Path.relString(), "build/platform/") {
		peers = append(peers,
			"build/platform/clang",
			"build/platform/clang/clang-format",
			"build/platform/lld",
			"build/platform/python/ymake_python3",
		)

		if instance.Platform.UsesSDKRoot {
			peers = append(peers, "build/platform/linux_sdk")
		}
	}

	return peers
}

func useArcadiaCompilerRuntime(ctx *GenCtx, instance ModuleInstance) bool {
	if instance.Platform != nil {
		if v := instance.Platform.Flags[envUSE_ARCADIA_COMPILER_RUNTIME]; v != 0 {
			return v != strNo
		}
	}

	if ctx == nil {
		return false
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

func rebasePerArchPeerAddIncl(hostPeerAddIncl []VFS, from, to ISA) []VFS {
	out := make([]VFS, len(hostPeerAddIncl))

	for i, p := range hostPeerAddIncl {
		out[i] = p

		for _, prefix := range archDependentPeerAddInclPrefixes {
			if p == source(prefix, string(from)) {
				out[i] = source(prefix, string(to))

				break
			}
		}
	}

	return out
}

func (e *EmitContext) defaultProgramPeerdirsForModule(postUser bool) []string {
	return e.defaultProgramPeerdirsForWithState(postUser)
}

func (e *EmitContext) defaultProgramPeerdirsForWithState(postUser bool) []string {
	_, instance, d := e.ctx, e.instance, e.d

	if instance.Language != LangCPP {
		return nil
	}

	if d.moduleStmt != nil && d.moduleStmt.Name == tokGoProgram {
		if postUser {
			return nil
		}

		return []string{"build/platform/lld", "build/cow/on"}
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
		archX8664:     env.bool(envARCH_X86_64),
		pic:           instance.Platform.PIC,
		sanitizer:     instance.Platform.BuildSanitized,
		useSSE4:       env.bool(envUSE_SSE4),
		useAsmlib:     d.useAsmlib,
		hadAllocator:  d.hadAllocator,
		allocatorName: allocatorName.string(),
	}

	var peers []string

	if !postUser {
		if d.useArcadiaLibm && instance.Path.relString() != "contrib/libs/libm" &&
			!strings.HasPrefix(instance.Path.relString(), "contrib/libs/libm/") {
			peers = append(peers, "contrib/libs/libm")
		}

		peers = append(peers, "build/cow/on")
		peers = appendImplicitPeers(peers, programAllocatorDefaults, rc)
	} else {
		peers = appendImplicitPeers(peers, programImplicitPeers, rc)

		if env.bool(envARCH_X86_64) && !flags.NoUtil && allocatorName.string() != "FAKE" {
			peers = append(peers, "library/cpp/cpuid_check")
		}
	}

	return peers
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
