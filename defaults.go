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

var unitImplicitPeers = []implicitPeerRule{
	{
		name: "musl/include",
		peer: "contrib/libs/musl/include",
		predicate: func(rc implicitPeerCtx) bool {
			return rc.muslOn
		},
	},
}

var programImplicitPeers = []implicitPeerRule{
	{
		name: "musl/full",
		peer: "contrib/libs/musl/full",
		predicate: func(rc implicitPeerCtx) bool {
			return rc.muslOn && !rc.muslLite
		},
	},
	{
		name: "musl",
		peer: "contrib/libs/musl",
		predicate: func(rc implicitPeerCtx) bool {
			return rc.muslOn && rc.muslLite
		},
	},
}

var archDependentPeerAddInclPrefixes = []string{
	"contrib/libs/musl/arch/",
}

var programAllocatorDefaults = []implicitPeerRule{
	{
		name: "tcmalloc (TCMALLOC_TC default)",
		peer: "library/cpp/malloc/tcmalloc",
		predicate: func(rc implicitPeerCtx) bool {
			return !rc.hadAllocator && rc.osLinux && (rc.muslOn || rc.archX8664)
		},
	},
	{
		name: "tcmalloc/no_percpu_cache (TCMALLOC_TC default)",
		peer: "contrib/libs/tcmalloc/no_percpu_cache",
		predicate: func(rc implicitPeerCtx) bool {
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

func suppressMallocAPIDefault(defaults []string, allocatorName string) []string {
	if allocatorName != "FAKE" {
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

func defaultPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	inst := instance

	if instance.Language == LangPy && d.usePython3 && d.moduleStmt != nil &&
		d.moduleStmt.Name == tokProtoLibrary && moduleExcludesTag(d, "CPP_PROTO") {
		inst.Language = LangCPP
	}

	return defaultPeerdirsForWithState(ctx, inst, d)
}

func defaultPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	flags := d.flags
	noPlatform := effectiveNoPlatform(flags)

	// contrib/libs/linux-headers is a header-only platform PEERDIR (upstream
	// _BASE_UNIT: when OS_LINUX && NEED_PLATFORM_PEERDIRS), independent of module
	// language — every C/C++/ASM compile on Linux receives its GLOBAL ADDINCL. The
	// gate is NEED_PLATFORM_PEERDIRS, which NO_PLATFORM()/NO_LIBC()/NO_RUNTIME() do
	// NOT clear (only _BARE_MODULE/PACKAGE/UNION fakes do, and those do not
	// compile), so it is NOT gated on noPlatform. It leads the platform peerdirs
	// (before cxxsupp/util/musl), contributes no link objects (header-only), and
	// must not peer itself.
	addLinuxHeaders := instance.Path != "contrib/libs/linux-headers" &&
		!strings.HasPrefix(instance.Path, "contrib/libs/linux-headers/")

	// The cxxsupp/libcxx/util language defaults below are C++-only; linux-headers
	// is not, so it is decided ahead of the language gate.
	if instance.Language != LangCPP {
		if addLinuxHeaders {
			return []string{"contrib/libs/linux-headers"}
		}

		return nil
	}

	rc := implicitPeerCtx{
		flags:      flags,
		noPlatform: noPlatform,
		muslOn:     d.muslEnabled && !noPlatform,
	}

	var peers []string

	if addLinuxHeaders {
		peers = append(peers, "contrib/libs/linux-headers")
	}

	if !flags.NoRuntime && !noPlatform {
		if instance.Path != "contrib/libs/cxxsupp/libcxx" && !strings.HasPrefix(instance.Path, "contrib/libs/cxxsupp/libcxx/") {
			peers = append(peers, "contrib/libs/cxxsupp/libcxx")
		}

		if instance.Path != "contrib/libs/cxxsupp/libcxxrt" {
			peers = append(peers, "contrib/libs/cxxsupp/libcxxrt")
		}

		if instance.Path != "contrib/libs/libunwind" {
			peers = append(peers, "contrib/libs/libunwind")
		}
	}

	if !flags.NoUtil && !noPlatform {
		if instance.Path != "util" && !strings.HasPrefix(instance.Path, "util/") {
			peers = append(peers, "util")
		}
	}

	peers = appendImplicitPeers(peers, unitImplicitPeers, rc)

	if !flags.NoRuntime && !noPlatform && useArcadiaCompilerRuntime(ctx, instance) && instance.Path != "library/cpp/sanitizer/include" {
		peers = append(peers, "library/cpp/sanitizer/include")
	}

	// Toolchain resources: upstream _BASE_UNIT peers build/platform/clang under
	// NEED_LLVM_TOOLS_PEERDIR && NEED_PLATFORM_PEERDIRS (ymake.core.conf:842) and
	// build/platform/${LINKER} under the linker selection. The gate is
	// NEED_PLATFORM_PEERDIRS — like contrib/libs/linux-headers above, NO_PLATFORM /
	// NO_LIBC / NO_RUNTIME do NOT clear it (only _BARE_UNIT/PACKAGE/UNION fakes do,
	// and those do not compile) — so it is NOT gated on noPlatform. The modules are
	// RESOURCES_LIBRARYs (inert: no link inputs), contributing their resource globals
	// and (lld) the propagated linker LDFLAGS to the closure.
	if !strings.HasPrefix(instance.Path, "build/platform/") {
		peers = append(peers,
			"build/platform/clang",
			"build/platform/clang/clang-format",
			"build/platform/lld",
			"build/platform/python/ymake_python3",
		)
	}

	return peers
}

func useArcadiaCompilerRuntime(ctx *genCtx, instance ModuleInstance) bool {
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

type implicitPeerCtx struct {
	flags         FlagSet
	noPlatform    bool
	muslOn        bool
	muslLite      bool
	osLinux       bool
	archX8664     bool
	hadAllocator  bool
	allocatorName string
}

type implicitPeerRule struct {
	name      string
	peer      string
	predicate func(implicitPeerCtx) bool
}

func appendImplicitPeers(dst []string, rules []implicitPeerRule, rc implicitPeerCtx) []string {
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
			if p == Source(prefix+string(from)) {
				out[i] = Source(prefix + string(to))
				break
			}
		}
	}

	return out
}

func defaultProgramPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData, postUser bool) []string {
	return defaultProgramPeerdirsForWithState(ctx, instance, d, postUser)
}

func defaultProgramPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, d *moduleData, postUser bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	flags := d.flags
	allocatorName := d.allocatorName
	env := buildIfEnv(instance)
	muslLite := env.Bool(envMUSL_LITE) || d.muslLite

	rc := implicitPeerCtx{
		flags:         flags,
		muslOn:        d.muslEnabled && !effectiveNoPlatform(flags),
		muslLite:      muslLite,
		osLinux:       env.Bool(envOS_LINUX),
		archX8664:     env.Bool(envARCH_X86_64),
		hadAllocator:  d.hadAllocator,
		allocatorName: allocatorName,
	}

	var peers []string

	if !postUser {
		peers = append(peers, "build/cow/on")

		peers = appendImplicitPeers(peers, programAllocatorDefaults, rc)
	} else {
		peers = appendImplicitPeers(peers, programImplicitPeers, rc)

		if env.Bool(envARCH_X86_64) && !flags.NoUtil && allocatorName != "FAKE" {
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

func peerYaMakeExists(fs FS, peerPath string) bool {
	return fs.IsFile(dirKey(peerPath), "ya.make")
}
