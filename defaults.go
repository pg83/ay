package main

import (
	"sort"
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

func isAncestorPath(srcDir, instancePath string) bool {
	if srcDir == instancePath {
		return true
	}

	return strings.HasPrefix(instancePath, srcDir+"/")
}

func isRuntimeAncestor(path string) bool {
	return runtimeAncestorPaths[path]
}

var runtimeStackAddInclPaths = map[VFS]int{
	Intern("$(S)/contrib/libs/cxxsupp/libcxx/include"):    0,
	Intern("$(S)/contrib/libs/cxxsupp/libcxxrt/include"):  1,
	Intern("$(S)/contrib/libs/cxxsupp/libcxxabi/include"): 2,
	Intern("$(S)/contrib/libs/libunwind/include"):         3,
}

var bundledAddInclPaths = map[VFS]bool{
	Intern("$(S)/contrib/libs/linux-headers"):     true,
	Intern("$(S)/contrib/libs/linux-headers/_nf"): true,
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

func hoistRuntimeStackAddIncl(paths []VFS) []VFS {
	if len(paths) == 0 {
		return paths
	}

	hoisted := make([]VFS, 0, len(paths))
	rest := make([]VFS, 0, len(paths))

	for _, p := range paths {
		if _, ok := runtimeStackAddInclPaths[p]; ok {
			hoisted = append(hoisted, p)
		} else {
			rest = append(rest, p)
		}
	}

	if len(hoisted) == 0 {
		return paths
	}

	sort.SliceStable(hoisted, func(i, j int) bool {
		return runtimeStackAddInclPaths[hoisted[i]] < runtimeStackAddInclPaths[hoisted[j]]
	})

	out := make([]VFS, 0, len(paths))
	out = append(out, hoisted...)
	out = append(out, rest...)

	return out
}

func defaultPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData) []string {
	inst := instance

	if instance.Language == LangPy && d.usePython3 && d.moduleStmt != nil &&
		d.moduleStmt.Name == "PROTO_LIBRARY" && moduleExcludesTag(d, "CPP_PROTO") {
		inst.Language = LangCPP
	}
	return defaultPeerdirsForWithState(ctx, inst, d.flags, d.muslEnabled)
}

func defaultPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, flags FlagSet, muslOn bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	noPlatform := effectiveNoPlatform(flags)

	rc := implicitPeerCtx{
		flags:             flags,
		noPlatform:        noPlatform,
		isRuntimeAncestor: isRuntimeAncestor(instance.Path),
		muslOn:            muslOn && !noPlatform,
	}

	if rc.isRuntimeAncestor {
		var only []string

		only = appendImplicitPeers(only, unitImplicitPeers, rc)

		if runtimeAncestorCxxConsumers[instance.Path] && useArcadiaCompilerRuntime(ctx, instance) && instance.Path != "library/cpp/sanitizer/include" {
			only = append(only, "library/cpp/sanitizer/include")
		}

		return only
	}

	var peers []string

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

	return peers
}

func useArcadiaCompilerRuntime(ctx *genCtx, instance ModuleInstance) bool {
	if instance.Platform != nil {
		if v := instance.Platform.Flags["USE_ARCADIA_COMPILER_RUNTIME"]; v != "" {
			return v != "no"
		}
	}

	if ctx == nil {
		return false
	}

	if v := ctx.target.Flags["USE_ARCADIA_COMPILER_RUNTIME"]; v != "" {
		return v != "no"
	}

	return true
}

type implicitPeerCtx struct {
	flags             FlagSet
	noPlatform        bool
	isRuntimeAncestor bool
	muslOn            bool
	muslLite          bool
	osLinux           bool
	archX8664         bool
	hadAllocator      bool
	allocatorName     string
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

func defaultProgramPeerdirsForModule(ctx *genCtx, instance ModuleInstance, d *moduleData, postUser bool) []string {
	return defaultProgramPeerdirsForWithState(ctx, instance, d.flags, d.hadAllocator, d.allocatorName, d.muslLite, postUser, d.muslEnabled)
}

func defaultProgramPeerdirsForWithState(ctx *genCtx, instance ModuleInstance, flags FlagSet, hadAllocator bool, allocatorName string, muslLiteOverride bool, postUser bool, muslOn bool) []string {
	if instance.Language != LangCPP {
		return nil
	}

	env := buildIfEnv(instance)
	muslLite := env.Bool("MUSL_LITE") || muslLiteOverride

	rc := implicitPeerCtx{
		flags:         flags,
		muslOn:        muslOn && !effectiveNoPlatform(flags),
		muslLite:      muslLite,
		osLinux:       env.Bool("OS_LINUX"),
		archX8664:     env.Bool("ARCH_X86_64"),
		hadAllocator:  hadAllocator,
		allocatorName: allocatorName,
	}

	var peers []string

	if !postUser {

		peers = append(peers, "build/cow/on")

		peers = appendImplicitPeers(peers, programAllocatorDefaults, rc)
	} else {

		peers = appendImplicitPeers(peers, programImplicitPeers, rc)

		if env.Bool("ARCH_X86_64") && !flags.NoUtil && allocatorName != "FAKE" {
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

func peerYaMakeExists(fs *FS, peerPath string) bool {
	return fs.IsFile(peerPath + "/ya.make")
}
