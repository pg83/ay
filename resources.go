package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

var (
	strLLDRootName      = internStr(resourcePatternLLDRoot)
	strYMakePython3Name = internStr(resourcePatternYMakePython3)
	usesPython3         = []STR{strYMakePython3Name}
	usesPython3JDK17    = []STR{strYMakePython3Name, internStr(resourcePatternJDK17)}
	usesPython3Clang16  = []STR{strYMakePython3Name, internStr(resourcePatternClang16)}
	resourcesKV         = KV{P: pkld, PC: pcLightBlue, ShowOut: true}
)

type ResourceDecl struct {
	Name      STR
	URI       STR
	GlobalVar STR
	Value     STR
	Token     STR
}

const resourceGlobalSuffix = "_RESOURCE_GLOBAL"

const platformDefaultArch = "x86_64"

func makeResourceDecl(name, uri string) ResourceDecl {
	value := "$(B)/resources/" + name
	globalVar := name + resourceGlobalSuffix

	return ResourceDecl{
		Name:      internStr(name),
		URI:       internStr(uri),
		GlobalVar: internStr(globalVar),
		Value:     internStr(value),
		Token:     internV(globalVar, "::", value),
	}
}

func hostPlatformKey(host *Platform) string {
	return string(host.OS) + "-" + isaPlatformKey(host.ISA)
}

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

func canonizePlatformKey(key string) string {
	key = strings.ToLower(key)

	os, arch, found := strings.Cut(key, "-")

	if !found || arch == "" || arch == platformDefaultArch {
		return os
	}

	return os + "-" + arch
}

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

func stripSbrPrefix(uri string) string {
	return strings.TrimPrefix(uri, "sbr:")
}

func resolveResourceDecls(fs FS, host *Platform, modulePath string, stmt *DeclareResourceStmt) []ResourceDecl {
	switch stmt.Macro {
	case tokDeclareExternalResource:

		out := make([]ResourceDecl, 0, len(stmt.Args)/2)

		for i := 0; i+1 < len(stmt.Args); i += 2 {
			out = append(out, makeResourceDecl(stmt.Args[i].string(), stmt.Args[i+1].string()))
		}

		return out
	case tokDeclareExternalHostResourcesBundle:

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

func sortedResourceGlobals(in []ResourceDecl) []ResourceDecl {
	out := append([]ResourceDecl(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].GlobalVar.string() < out[j].GlobalVar.string()
	})

	return out
}

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

type ModuleToolchain struct {
	ClangResource STR
	ClangRoot     STR
	CC            STR
	CXX           STR
	AR            STR
	Objcopy       STR
	Strip         STR
	LLDRoot       STR
	ARCmdHead     []STR
	LLD           STR
	Python3       STR
}

func resolveModuleToolchain(globals []ResourceDecl, clangVer string) ModuleToolchain {
	var tc ModuleToolchain

	clangRes := resourcePatternClangTool + clangVer

	clangResID := internStr(clangRes)

	for _, decl := range globals {
		switch decl.Name {
		case clangResID:
			root := "$(B)/resources/" + clangRes
			tc.ClangResource = clangResID
			tc.ClangRoot = internStr(root)
			tc.CC = internV(root, "/bin/clang")
			tc.CXX = internV(root, "/bin/clang++")
			tc.AR = internV(root, "/bin/llvm-ar")
			tc.Objcopy = internV(root, "/bin/llvm-objcopy")
			tc.Strip = internV(root, "/bin/llvm-strip")
		case strLLDRootName:
			root := "$(B)/resources/" + resourcePatternLLDRoot
			tc.LLDRoot = internStr(root)
			tc.LLD = internV(root, "/bin/ld.lld")
		case strYMakePython3Name:
			tc.Python3 = internV("$(B)/resources/", resourcePatternYMakePython3, "/bin/python3")
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

	var sbomRef *NodeRef
	var sbomPath *VFS

	if sbomActive(ctx, instance) && d.toolchainName != "" {
		if instance.Path.rel() != pythonToolchainInfoRel {
			pythonToolchainSbomComponent(ctx, instance.Platform)
		}

		sbomRef, sbomPath = emitSbomToolchainComponent(ctx, instance, d.toolchainName, d.modver)
	}

	result := &ModuleEmitResult{
		ModuleStmtName:        d.moduleStmt.Name,
		ResourceGlobalClosure: globals,
		Peerdirs:              d.peerdirs,
		RPathFlagsGlobal:      d.rpathFlagsGlobal,
		LDFlagsGlobal:         d.ldFlags,
		CFlagsGlobal:          d.cFlagsGlobal,
		CXXFlagsGlobal:        d.cxxFlagsGlobal,
		COnlyFlagsGlobal:      d.cOnlyFlagsGlobal,
		ObjAddLibsGlobal:      d.objAddLibsGlobal,
		AddInclGlobal:         d.addInclGlobal,
		OwnAddInclGlobal:      d.addInclGlobal,
		AddInclUserGlobal:     d.addInclUserGlobal,
		AddInclOneLevel:       d.addInclOneLevel,
		SbomComponentRef:      sbomRef,
		SbomComponentPath:     sbomPath,
	}
	ctx.memo.put(ctx.instanceKey(instance), result)

	return result
}

func prebuiltModuleSuffix(p *Platform) string {
	if p.OS == OSWindows {
		return ".exe"
	}

	return ""
}

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

	srcVFS := build(strings.TrimPrefix(d.primaryOutput, "$(B)/"))
	dst := lDOutputPath(instance, programBinaryName(instance, d.moduleStmt))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) {
		ownSbomRef, ownSbomPath = emitSbomComponent(ctx, instance, d, programBinaryName(instance, d.moduleStmt))
	}

	inputs := InputChunks{ctx.scripts[copyFsToolsVFS]}
	depRefs := []NodeRef{fetchRef}

	if sbomActive(ctx, instance) && instance.Platform.BuildRelease {
		if pyRef, pyPath := pythonToolchainSbomComponent(ctx, instance.Platform); pyRef != nil {
			inputs = append(inputs, []VFS{*pyPath})
			depRefs = append(depRefs, *pyRef)
		}
	}

	if ownSbomRef != nil && instance.Platform.BuildRelease {
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
		Env:          env,
		Inputs:       inputs,
		KV:           &resourcesKV,
		Outputs:      na.vfsList(dst),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      depRefs,
		Resources:    usesPython3,
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

func readResourceBundleJSON(fs FS, rel string) map[string]string {
	var data ResourceBundleJSON
	throw(json.Unmarshal(fs.read(rel), &data))

	out := make(map[string]string, len(data.ByPlatform))

	for k, v := range data.ByPlatform {
		out[k] = v.URI
	}

	return out
}
