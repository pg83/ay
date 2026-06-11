package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// External-resource model. A RESOURCES_LIBRARY (build/platform/clang, …) declares
// external resources via DECLARE_EXTERNAL_RESOURCE /
// DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE[_BY_JSON]. Each declaration yields:
//   - a <Name>_RESOURCE_GLOBAL variable bound to the bare "$(<Name>)" resource ref,
//     which propagates transitively through the PEERDIR closure (ymake's
//     global_vars_collector mines every *_RESOURCE_GLOBAL var across the closure)
//     and is rendered into a test node's --global-resource list as
//     "<Name>_RESOURCE_GLOBAL::$(<Name>)";
//   - a fetch of the host-selected URI into the same bare $(<Name>) resource dir.
//
// The reference is the bare $(<Name>) the executor mounts mechanically (mountString);
// the sandbox-rotating "-<id>" suffix ymake carries on the var (NYa::ResourceVarName)
// has no place in our graph — it would not mount, and dump-normalize only strips it
// off the upstream reference. Every field is interned: the model carries STR end to
// end, the raw strings existing only transiently at the json/macro-argument boundary
// in makeResourceDecl.

// resourceDecl is one declared external resource after host-platform selection.
type ResourceDecl struct {
	Name      STR // resource base name, e.g. "CLANG16"
	URI       STR // host-selected uri, e.g. "sbr:6495238978" or an absolute path
	GlobalVar STR // propagated variable name, e.g. "CLANG16_RESOURCE_GLOBAL"
	Value     STR // variable value: the bare resource ref "$(CLANG16)"
	Token     STR // --global-resource arg "CLANG16_RESOURCE_GLOBAL::$(CLANG16)"
}

const resourceGlobalSuffix = "_RESOURCE_GLOBAL"

// makeResourceDecl interns one resource (the sole string boundary): it composes the
// bare resource ref, global-var name and --global-resource token, then carries them
// as STR. The uri is kept only to drive the fetch — it never enters the ref.
func makeResourceDecl(name, uri string) ResourceDecl {
	// Resource references resolve to the FETCH node's output dir, $(B)/resources/NAME,
	// so flags/env that splice ${NAME_RESOURCE_GLOBAL} (e.g. lld's --ld-path) point at
	// a real graph output the consumer depends on — not an executor-mounted $(NAME).
	// dump normalize folds $(B)/resources/NAME back to $(NAME) for the comparison.
	value := "$(B)/resources/" + name
	globalVar := name + resourceGlobalSuffix

	return ResourceDecl{
		Name:      internStr(name),
		URI:       internStr(uri),
		GlobalVar: internStr(globalVar),
		Value:     internStr(value),
		Token:     internStr(globalVar + "::" + value),
	}
}

// hostPlatformKey is the by_platform json key for the host (os-isa), e.g.
// "linux-x86_64". Resource bundles select the HOST entry — these are host tools.
func hostPlatformKey(host *Platform) string {
	return string(host.OS) + "-" + isaPlatformKey(host.ISA)
}

func isaPlatformKey(isa ISA) string {
	if isa == ISAAArch64 {
		return "aarch64"
	}

	return string(isa)
}

// stripSbrPrefix returns the bare sandbox id of an "sbr:<id>" uri (used by the
// fetch mapping), or the uri unchanged when it carries no sbr scheme.
func stripSbrPrefix(uri string) string {
	return strings.TrimPrefix(uri, "sbr:")
}

// resolveResourceDecls turns one DECLARE_EXTERNAL_RESOURCE /
// _HOST_RESOURCES_BUNDLE[_BY_JSON] call into host-selected resource declarations.
func resolveResourceDecls(fs FS, host *Platform, modulePath string, stmt *DeclareResourceStmt) []ResourceDecl {
	switch stmt.Macro {
	case tokDeclareExternalResource:
		// NAME uri [NAME2 uri2 ...] — direct, host-independent.
		out := make([]ResourceDecl, 0, len(stmt.Args)/2)

		for i := 0; i+1 < len(stmt.Args); i += 2 {
			out = append(out, makeResourceDecl(stmt.Args[i].string(), stmt.Args[i+1].string()))
		}

		return out
	case tokDeclareExternalHostResourcesBundle:
		// NAME uri FOR platform [uri2 FOR platform2 ...] — select the host entry.
		name := stmt.Args[0]
		bundle := map[string]string{}

		for i := 1; i+2 < len(stmt.Args); i += 3 {
			if stmt.Args[i+1].string() != "FOR" {
				throwFmt("gen: %s: malformed DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args %v", modulePath, stmt.Args)
			}

			bundle[stmt.Args[i+2].string()] = stmt.Args[i].string()
		}

		return []ResourceDecl{selectHostResourceDecl(host, modulePath, name.string(), bundle)}
	case tokDeclareExternalHostResourcesBundleByJson:
		// NAME file.json — read by_platform.<host>.uri.
		name, jsonRel := stmt.Args[0], stmt.Args[1]
		bundle := readResourceBundleJSON(fs, filepath.ToSlash(filepath.Join(modulePath, jsonRel.string())))

		return []ResourceDecl{selectHostResourceDecl(host, modulePath, name.string(), bundle)}
	}

	return nil
}

func selectHostResourceDecl(host *Platform, modulePath, name string, bundle map[string]string) ResourceDecl {
	uri, ok := bundle[hostPlatformKey(host)]

	if !ok {
		throwFmt("gen: %s: resource %q has no entry for host platform %q", modulePath, name, hostPlatformKey(host))
	}

	return makeResourceDecl(name, uri)
}

// sortedResourceGlobals returns the declarations ordered by global-var name,
// mirroring ymake's std::set<TString> ExternalResources collection — the order
// in which a test node's --global-resource arguments are emitted.
func sortedResourceGlobals(in []ResourceDecl) []ResourceDecl {
	out := append([]ResourceDecl(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].GlobalVar.string() < out[j].GlobalVar.string()
	})

	return out
}

// resolveResourceGlobalRef expands ymake's deferred CLANG_BC_ROOT=$CLANG16_RESOURCE_GLOBAL
// reference against the consuming module's resource-global closure (the transitive union
// of <NAME>_RESOURCE_GLOBAL declarations reached through PEERDIR — build/platform/clang
// declares CLANG16/18/20). "$CLANG16_RESOURCE_GLOBAL" / "${CLANG16_RESOURCE_GLOBAL}"
// resolves to the decl's value ("$(CLANG16-<id>)"); a non-reference string passes through.
// This mirrors ymake deferring the expansion until command generation, when the global
// is available from the closure rather than read eagerly at module-collection time.
func resolveResourceGlobalRef(s string, globals []ResourceDecl) string {
	name, ok := strings.CutPrefix(s, "$")

	if !ok {
		return s
	}

	name = strings.TrimPrefix(strings.TrimSuffix(name, "}"), "{")

	for _, d := range globals {
		if d.GlobalVar.string() == name {
			return d.Value.string()
		}
	}

	throwFmt("resources: %q references resource global not in the PEERDIR closure", s)

	return ""
}

// bindResourceGlobalVars resolves a RESOURCES_LIBRARY's DECLARE_* statements and
// binds each <NAME>_RESOURCE_GLOBAL env var to its "$(<VarName>)" value, mirroring
// ymake's ProcessExternalResource. Returns whether any var was bound (so the
// caller re-collects to expand references that textually precede the DECLARE).
func bindResourceGlobalVars(ctx *GenCtx, instance ModuleInstance, d *ModuleData, env Environment) bool {
	bound := false

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.rel(), stmt) {
			env.setStringID(internEnvSTR(decl.GlobalVar), decl.Value)
			bound = true
		}
	}

	return bound
}

// moduleToolchain holds a module's tool-invocation paths, derived from the
// external-resource globals reachable through its PEERDIR closure: the compiler/
// archiver/objcopy/strip live under $(B)/resources/CLANG<ver> (build/platform/clang),
// the linker under $(B)/resources/LLD_ROOT (build/platform/lld), and python under
// $(B)/resources/YMAKE_PYTHON3 (build/platform/python/ymake_python3) — each the
// output dir of that resource's FETCH node, taken as a dep by listing the resource
// name in the consuming node's usesResources. A field stays 0 when its resource is
// absent from the closure (the consuming emitter then has no peer to take the tool
// from — caught at use, never silently defaulted).
type ModuleToolchain struct {
	// ClangResource is the versioned clang resource the compiler/llvm tools come
	// from (e.g. "CLANG20"), selected by the platform's ClangVer. Consumers list it in
	// their node's usesResources so they depend on that specific FETCH node — version-
	// specific so several clang versions (CLANG16 for bitcode, CLANG20 to compile) coexist.
	ClangResource STR
	ClangRoot     STR
	CC            STR
	CXX           STR
	AR            STR
	Objcopy       STR
	Strip         STR
	LLDRoot       STR

	// ARCmdHead is the pre-built head of every AR command this toolchain
	// drives — [python3, link_lib.py, llvm-ar, llvm, gnu, $(B), None, --] —
	// referenced as a chunk by emitARNode, never copied. Built here because
	// it is a pure function of the toolchain.
	ARCmdHead []STR
	LLD       STR
	Python3   STR
}

// resolveModuleToolchain derives the tool paths from the module's resource-global
// closure. Tool paths come from peers (build/platform/*), not ambient platform flags.
func resolveModuleToolchain(globals []ResourceDecl, clangVer string) ModuleToolchain {
	var tc ModuleToolchain

	// The compiler/llvm tools come from the version-specific CLANG<ver> resource
	// (e.g. CLANG20), not the version-independent bare CLANG: $(B)/resources/CLANG20
	// is the FETCH node's output dir, depended on by listing tc.ClangResource in the
	// consuming node's usesResources.
	clangRes := resourcePatternClangTool + clangVer

	for _, decl := range globals {
		switch decl.Name.string() {
		case clangRes:
			root := "$(B)/resources/" + clangRes
			tc.ClangResource = internStr(clangRes)
			tc.ClangRoot = internStr(root)
			tc.CC = internStr(root + "/bin/clang")
			tc.CXX = internStr(root + "/bin/clang++")
			tc.AR = internStr(root + "/bin/llvm-ar")
			tc.Objcopy = internStr(root + "/bin/llvm-objcopy")
			tc.Strip = internStr(root + "/bin/llvm-strip")
		case resourcePatternLLDRoot:
			root := "$(B)/resources/" + resourcePatternLLDRoot
			tc.LLDRoot = internStr(root)
			tc.LLD = internStr(root + "/bin/ld.lld")
		case resourcePatternYMakePython3:
			tc.Python3 = internStr("$(B)/resources/" + resourcePatternYMakePython3 + "/bin/python3")
		}
	}

	tc.ARCmdHead = []STR{
		tc.Python3,
		(buildScriptsLinkLibPy).str(),
		tc.AR,
		internStr(arTypeLLVM),
		internStr(arFormatGNU),
		argB.str(),
		argNone.str(),
		arg2.str(),
	}

	return tc
}

// genResourcesLibrary emits a RESOURCES_LIBRARY: it produces no archive/objects
// (upstream RESOURCES_LIBRARY is a .pkg.fake IGNORED unit), only the external
// resource globals it declares, which propagate up the PEERDIR closure.
func genResourcesLibrary(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	var globals []ResourceDecl
	deduper.reset()

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.rel(), stmt) {
			if deduper.add(VFS(decl.GlobalVar)) {
				globals = append(globals, decl)
				emitResourceFetch(ctx, decl)
			}
		}
	}

	// A RESOURCES_LIBRARY has no PEERDIRs, so its GLOBAL contributions (the
	// .GLOBAL list: RPATH/LDFLAGS/USER_C*FLAGS/OBJADDE_LIB/ADDINCL) are its own,
	// un-merged, and propagate to consumers exactly as the general path would with
	// an empty peer set. This is the real toolchain mechanism: build/platform/lld
	// SET_APPEND(LDFLAGS_GLOBAL -fuse-ld=lld --ld-path=…) and build/platform/local_so
	// SET_APPEND(RPATH_GLOBAL -Wl,-rpath,$ORIGIN) reach every linking consumer here.
	// Duplicates of these flags that currently also come from the Platform (the
	// mine.go stopgap) are removed on the Platform side, not here.
	result := &ModuleEmitResult{
		ModuleStmtName:        d.moduleStmt.Name,
		ResourceGlobalClosure: globals,
		Peerdirs:              d.peerdirs,
		RPathFlagsGlobal:      dedupARG(d.rpathFlagsGlobal),
		LDFlagsGlobal:         dedupARG(d.ldFlags),
		CFlagsGlobal:          dedupARG(d.cFlagsGlobal),
		CXXFlagsGlobal:        dedupARG(d.cxxFlagsGlobal),
		COnlyFlagsGlobal:      dedupARG(d.cOnlyFlagsGlobal),
		ObjAddLibsGlobal:      dedupARG(d.objAddLibsGlobal),
		AddInclGlobal:         dedupVFS(d.addInclGlobal, nil),
		OwnAddInclGlobal:      d.addInclGlobal,
		AddInclUserGlobal:     d.addInclUserGlobal,
		AddInclOneLevel:       d.addInclOneLevel,
	}
	ctx.memo.put(ctx.instanceKey(instance), result)

	return result
}

type ResourceBundleJSON struct {
	ByPlatform map[string]struct {
		URI string `json:"uri"`
	} `json:"by_platform"`
}

// readResourceBundleJSON parses a build/platform/*/*.json bundle into a
// platform-key -> uri map (the by_platform table feeding DECLARE_*_BY_JSON).
func readResourceBundleJSON(fs FS, rel string) map[string]string {
	var data ResourceBundleJSON
	throw(json.Unmarshal(fs.read(rel), &data))

	out := make(map[string]string, len(data.ByPlatform))

	for k, v := range data.ByPlatform {
		out[k] = v.URI
	}

	return out
}
