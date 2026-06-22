package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// resolveModuleToolchain derives the tool paths from the module's resource-global
	// closure. Tool paths come from peers (build/platform/*), not ambient platform flags.
	strLLDRootName      = internStr(resourcePatternLLDRoot)
	strYMakePython3Name = internStr(resourcePatternYMakePython3)
	// Shared Resources vectors — every emitter that needs one of these
	// fixed sets references the same slice instead of building it per node.
	usesPython3        = []STR{strYMakePython3Name}
	usesPython3JDK17   = []STR{strYMakePython3Name, internStr(resourcePatternJDK17)}
	usesPython3Clang16 = []STR{strYMakePython3Name, internStr(resourcePatternClang16)}
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

// platformDefaultArch is the architecture ya treats as implicit in a by_platform key
// (NYa::TCanonizedPlatform::DEFAULT_ARCH): "linux-x86_64" canonizes to "linux".
const platformDefaultArch = "x86_64"

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

// resourceJSONPlatformKey is the SET_RESOURCE_URI_FROM_JSON by_platform key for the
// instance's platform — ymake's canonized platform name where x86_64 is the implicit
// default (no suffix) and win is "win32": "linux"/"linux-aarch64"/"darwin"/
// "darwin-arm64"/"win32". Distinct from hostPlatformKey ("linux-x86_64"), which the
// DECLARE_*_BUNDLE bundles use.
func resourceJSONPlatformKey(env Environment) string {
	switch {
	case env.bool(envOS_DARWIN):
		if env.bool(envARCH_ARM64) {
			return "darwin-arm64"
		}

		return "darwin"
	case env.bool(envOS_WINDOWS):
		return "win32"
	default:
		if env.bool(envARCH_AARCH64) {
			return "linux-aarch64"
		}

		return "linux"
	}
}

// canonizePlatformKey mirrors NYa::TCanonizedPlatform::AsString (yaplatform): a
// by_platform key is lower-cased <os>[-<arch>] with the default arch (x86_64) dropped, so
// "linux" and "linux-x86_64" both collapse to "linux". SET_RESOURCE_URI_FROM_JSON bundles
// use either spelling (protoc: "linux"; py3cc/slow: "linux-x86_64"); canonizing both sides
// makes the lookup hit regardless.
func canonizePlatformKey(key string) string {
	key = strings.ToLower(key)

	os, arch, found := strings.Cut(key, "-")
	if !found || arch == "" || arch == platformDefaultArch {
		return os
	}

	return os + "-" + arch
}

// resolveResourceURIFromBundle returns the bundle URI for env's platform, matching keys
// by their canonical form (see canonizePlatformKey). Keys are scanned in sorted order for
// deterministic selection.
func resolveResourceURIFromBundle(bundle map[string]string, env Environment) (string, bool) {
	want := resourceJSONPlatformKey(env)

	keys := make([]string, 0, len(bundle))
	for k := range bundle {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		if canonizePlatformKey(k) == want {
			return bundle[k], true
		}
	}

	return "", false
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
// name in the consuming node's Resources. A field stays 0 when its resource is
// absent from the closure (the consuming emitter then has no peer to take the tool
// from — caught at use, never silently defaulted).
type ModuleToolchain struct {
	// ClangResource is the versioned clang resource the compiler/llvm tools come
	// from (e.g. "CLANG20"), selected by the platform's ClangVer. Consumers list it in
	// their node's Resources so they depend on that specific FETCH node — version-
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

func resolveModuleToolchain(globals []ResourceDecl, clangVer string) ModuleToolchain {
	var tc ModuleToolchain

	// The compiler/llvm tools come from the version-specific CLANG<ver> resource
	// (e.g. CLANG20), not the version-independent bare CLANG: $(B)/resources/CLANG20
	// is the FETCH node's output dir, depended on by listing tc.ClangResource in the
	// consuming node's Resources.
	clangRes := resourcePatternClangTool + clangVer
	// Decl names are compared in id space: one intern probe per call instead of
	// a string view per decl (the LLD/python ids are package-level constants).
	clangResID := internStr(clangRes)

	for _, decl := range globals {
		switch decl.Name {
		case clangResID:
			root := "$(B)/resources/" + clangRes
			tc.ClangResource = clangResID
			tc.ClangRoot = internStr(root)
			tc.CC = internStr(root + "/bin/clang")
			tc.CXX = internStr(root + "/bin/clang++")
			tc.AR = internStr(root + "/bin/llvm-ar")
			tc.Objcopy = internStr(root + "/bin/llvm-objcopy")
			tc.Strip = internStr(root + "/bin/llvm-strip")
		case strLLDRootName:
			root := "$(B)/resources/" + resourcePatternLLDRoot
			tc.LLDRoot = internStr(root)
			tc.LLD = internStr(root + "/bin/ld.lld")
		case strYMakePython3Name:
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
	var sbomRef *NodeRef
	var sbomPath *VFS

	if sbomActive(ctx, instance) && d.toolchainName != "" {
		// The toolchain SBOM node runs $(B)/resources/YMAKE_PYTHON3/bin/python3.
		// Upstream reaches that resource via the YMAKE_PYTHON3_PEERDIR that
		// _BARE_UNIT injects into every unit (RESOURCES_LIBRARY: _BARE_UNIT,
		// ymake.core.conf:2064/576); this RESOURCES_LIBRARY path otherwise resolves
		// no peers, so resolve that one universal python peer explicitly. The effect
		// we need is registering the YMAKE_PYTHON3 FETCH into ctx.fetchRefs BEFORE
		// this node, so its by-name resource dep is a real edge at build time too,
		// not just in -G where the fetch map is complete only at finalize. genModule
		// is memoized (no new nodes; -G is topo-sorted so byte output is unchanged),
		// and we discard the python toolchain's own SBOM component — it is not this
		// toolchain's. ymake_python3 declares YMAKE_PYTHON3 itself, so it self-peers
		// nothing; skip it to avoid a pointless re-entry.
		if instance.Path.rel() != pythonToolchainInfoRel {
			pythonToolchainSbomComponent(ctx, instance.Platform)
		}

		sbomRef, sbomPath = emitSbomToolchainComponent(ctx, instance, d.toolchainName, d.modver)
	}

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
		SbomComponentRef:      sbomRef,
		SbomComponentPath:     sbomPath,
	}
	ctx.memo.put(ctx.instanceKey(instance), result)

	return result
}

// prebuiltModuleSuffix is the PROGRAM MODULE_SUFFIX for the platform (ymake.core.conf:
// the _LINK_UNIT/PROGRAM default is empty, .exe under WIN32). PREBUILT_PROGRAM's
// PRIMARY_OUTPUT splices it after the binary name.
func prebuiltModuleSuffix(p *Platform) string {
	if p.OS == OSWindows {
		return ".exe"
	}

	return ""
}

// genPrebuiltProgram emits a PREBUILT_PROGRAM: rather than compiling sources, it
// fetches a sandbox-built binary (DECLARE_EXTERNAL_RESOURCE) and copies it to the
// module's program output with fs_tools.py, mirroring upstream _PREBUILT_PROGRAM_CMD
// ($COPY_CMD $_PRIMARY_OUTPUT_VALUE ${TARGET}, kv p=ld pc=light-blue show_out). The
// USE_PREBUILT_TOOLS contour (internal only) routes protoc/… here, so the tool's
// from-source object closure (the host-PIC protobuf/abseil/grpc compiles) never enters
// the graph. The result is a PROGRAM (LDRef/LDPath) so tool consumers take its binary.
func genPrebuiltProgram(ctx *GenCtx, instance ModuleInstance, d *ModuleData) *ModuleEmitResult {
	na := ctx.na

	var fetchRef NodeRef
	var globals []ResourceDecl
	deduper.reset()

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.rel(), stmt) {
			if deduper.add(VFS(decl.Name)) {
				globals = append(globals, decl)
				fetchRef = emitResourceFetch(ctx, decl)
			}
		}
	}

	if d.primaryOutput == "" || len(globals) == 0 {
		throwFmt("gen: %s: PREBUILT_PROGRAM has no PRIMARY_OUTPUT/resource", instance.Path.rel())
	}

	if strings.Contains(d.primaryOutput, "${") {
		throwFmt("gen: %s: PREBUILT_PROGRAM PRIMARY_OUTPUT %q has an unresolved reference", instance.Path.rel(), d.primaryOutput)
	}

	// primaryOutput is "$(B)/resources/<NAME>/<bin>" (the fetch node's output dir); the
	// copy reads it as an input and depends on the fetch. dst is the module's program
	// output, $(B)/<dir>/<name> — what ${TARGET} expands to and tool consumers reference.
	srcVFS := build(strings.TrimPrefix(d.primaryOutput, "$(B)/"))
	dst := lDOutputPath(instance, programBinaryName(instance, d.moduleStmt))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// The prebuilt copy is an agnostic "bin"; like any licensed module it gets a
	// _GEN_SBOM_COMPONENT, and (when EMBED is on for this RELEASE host build) the
	// copy node itself collects its own .component.sbom — ref lists it as an input.
	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) {
		ownSbomRef, ownSbomPath = emitSbomComponent(ctx, instance, d, programBinaryName(instance, d.moduleStmt))
	}

	// _PREBUILT_PROGRAM_CMD (ymake.core.conf:5443) is `GENERATE_MF && COPY_CMD
	// $_PRIMARY_OUTPUT_VALUE ${TARGET}` — unlike _DLL_PROXY_LIBRARY_CMD it does NOT
	// wrap the primary output in ${input}, so the fetched binary is a resource
	// (tracked via fetchRef), not a content input.
	inputs := InputChunks{ctx.scripts[copyFsToolsVFS]}
	depRefs := []NodeRef{fetchRef}

	// Every module descends from _BARE_UNIT, which PEERDIR+=$YMAKE_PYTHON3_PEERDIR
	// (ymake.core.conf:574). A bare-link PREBUILT_PROGRAM resolves no other SBOM
	// peers (NO_PLATFORM/NO_RUNTIME), so that universal python toolchain component is
	// the whole peer SBOM closure it collects at the link.
	if sbomActive(ctx, instance) && instance.Platform.BuildRelease {
		if pyRef, pyPath := pythonToolchainSbomComponent(ctx, instance.Platform); pyRef != nil {
			inputs = append(inputs, []VFS{*pyPath})
			depRefs = append(depRefs, *pyRef)
		}
	}

	if ownSbomRef != nil && instance.Platform.BuildRelease {
		// _GEN_SBOM_COMPONENT (sbom.conf:42, run via _CONTRIB_MODULE_HOOKS on LICENSE)
		// declares ${input:gen_sbom.py}; that module input lands on the module's
		// primary node — for a bare PREBUILT_PROGRAM the single copy node — alongside
		// its own .component.sbom global (a separate DX node also produces the latter).
		inputs = append(inputs, []VFS{*ownSbomPath, source(sbomGenScriptRel)})
		depRefs = append(depRefs, *ownSbomRef)
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList([]STR{
			wrapccPython3STR,
			copyFsToolsVFS.str(),
			argCopy.str(),
			srcVFS.str(),
			dst.str(),
		}), Env: env}),
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkld, PC: pcLightBlue, ShowOut: true},
		Outputs:          na.vfsList(dst),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleLang: mlAgnostic, ModuleType: mtBin},
		DepRefs:          depRefs,
		Resources:        usesPython3,
	}

	ref := ctx.emit.emit(node)

	result := &ModuleEmitResult{
		ModuleStmtName: d.moduleStmt.Name,
		ARRef:          ref,
		ARPath:         &dst,
		isPROGRAM:      true,
		LDRef:          ref,
		LDPath:         &dst,
		Peerdirs:       d.peerdirs,
		// A prebuilt codegen tool keeps its INDUCED_DEPS (ya.make.induced_deps,
		// e.g. grpc_cpp's grpcpp service headers) so a generated file's consumer
		// pulls them via GeneratorRefs — exactly as the from-source build does.
		InducedDeps: d.inducedDeps,
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
