package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
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

const (
	resourceGlobalSuffix = "_RESOURCE_GLOBAL"
	platformDefaultArch  = "x86_64"
)

type ResourceDecl struct {
	Name      STR
	URI       STR
	GlobalVar STR
	Value     VFS
	Token     STR
}

func makeResourceDecl(name, uri string) ResourceDecl {
	value := "$(B)/resources/" + name
	globalVar := name + resourceGlobalSuffix

	return ResourceDecl{
		Name:      internStr(name),
		URI:       internStr(uri),
		GlobalVar: internStr(globalVar),
		Value:     build("resources/", name),
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
	want := canonizePlatformKey(hostPlatformKey(host))
	keys := make([]string, 0, len(bundle))

	for k := range bundle {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		if canonizePlatformKey(k) == want {
			return makeResourceDecl(name, bundle[k])
		}
	}

	throwFmt("gen: %s: resource %q has no entry for host platform %q", modulePath, name, hostPlatformKey(host))

	return ResourceDecl{}
}

func sortedResourceGlobals(in []ResourceDecl) []ResourceDecl {
	out := append([]ResourceDecl(nil), in...)

	slices.SortFunc(out, func(a, b ResourceDecl) int {
		return strings.Compare(a.GlobalVar.string(), b.GlobalVar.string())
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

func (e *EmitContext) bindResourceGlobalVars(env Environment) bool {
	ctx, instance, d := e.ctx, e.instance, e.d
	bound := false

	for _, stmt := range d.resourceDeclStmts {
		for _, decl := range resolveResourceDecls(ctx.fs, ctx.host, instance.Path.relString(), stmt) {
			env.setVFS(internEnvSTR(decl.GlobalVar), decl.Value)
			bound = true
		}
	}

	return bound
}

type ModuleToolchain struct {
	ClangResource STR
	ClangRoot     VFS
	CC            VFS
	CXX           VFS
	AR            VFS
	Objcopy       VFS
	Strip         VFS
	LLDRoot       VFS
	ARCmdHead     []ANY
	LLD           VFS
	Python3       VFS
}

func isClangToolResourceName(name, clangVer string) bool {
	return len(name) == len(resourcePatternClangTool)+len(clangVer) &&
		strings.HasPrefix(name, resourcePatternClangTool) &&
		name[len(resourcePatternClangTool):] == clangVer
}

type ToolchainKey struct {
	clangVer string
	bits     uint8
}

func resolveModuleToolchain(ctx *GenCtx, globals []ResourceDecl, clangVer string) ModuleToolchain {
	key := ToolchainKey{clangVer: clangVer}

	for _, decl := range globals {
		switch {
		case isClangToolResourceName(decl.Name.string(), clangVer):
			key.bits |= 1
		case decl.Name == strLLDRootName:
			key.bits |= 2
		case decl.Name == strYMakePython3Name:
			key.bits |= 4
		}
	}

	if tc, ok := ctx.tcMemo[key]; ok {
		return tc
	}

	clangRes := resourcePatternClangTool + clangVer
	clangResID := internStr(clangRes)

	var tc ModuleToolchain

	for _, decl := range globals {
		switch decl.Name {
		case clangResID:
			const pfx = "resources/"

			tc.ClangResource = clangResID
			tc.ClangRoot = build(pfx, clangRes)
			tc.CC = build(pfx, clangRes, "/bin/clang")
			tc.CXX = build(pfx, clangRes, "/bin/clang++")
			tc.AR = build(pfx, clangRes, "/bin/llvm-ar")
			tc.Objcopy = build(pfx, clangRes, "/bin/llvm-objcopy")
			tc.Strip = build(pfx, clangRes, "/bin/llvm-strip")
		case strLLDRootName:
			const pfx = "resources/"

			tc.LLDRoot = build(pfx, resourcePatternLLDRoot)
			tc.LLD = build(pfx, resourcePatternLLDRoot, "/bin/ld.lld")
		case strYMakePython3Name:
			tc.Python3 = build("resources/", resourcePatternYMakePython3, "/bin/python3")
		}
	}

	tc.ARCmdHead = []ANY{
		tc.Python3.any(),
		buildScriptsLinkLibPy.any(),
		tc.AR.any(),
		internStr(arTypeLLVM).any(),
		internStr(arFormatGNU).any(),
		argB.any(),
		argNone.any(),
		arg2.any(),
	}

	ctx.tcMemo[key] = tc

	if ownershipOn {
		registerOwnedSlice(tc.ARCmdHead)
	}

	return tc
}

func (e *EmitContext) genResourcesLibrary() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d

	var globals []ResourceDecl
	var decls []ResourceDecl

	for _, stmt := range d.resourceDeclStmts {
		decls = append(decls, resolveResourceDecls(ctx.fs, ctx.host, instance.Path.relString(), stmt)...)
	}

	dedupers.with(func(deduper *DeDuper) {
		for _, decl := range decls {
			if deduper.add(decl.GlobalVar.strID()) {
				globals = append(globals, decl)
			}
		}
	})

	for _, decl := range globals {
		emitResourceFetch(ctx, decl)
	}

	var sbomRef *NodeRef
	var sbomPath *VFS

	if sbomActive(ctx, instance) && d.toolchainName != "" {
		if instance.Path.relString() != pythonToolchainInfoRel {
			pythonToolchainSbomComponent(ctx, instance.Platform)
		}

		sbomRef, sbomPath = e.emitSbomToolchainComponent(d.toolchainName, d.modver)
	}

	result := &ModuleEmitResult{
		ModuleStmtName:        d.moduleStmt.Name,
		ResourceGlobalClosure: globals,
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

func (e *EmitContext) genPrebuiltProgram() *ModuleEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	var fetchRef NodeRef
	var globals []ResourceDecl
	var decls []ResourceDecl

	for _, stmt := range d.resourceDeclStmts {
		decls = append(decls, resolveResourceDecls(ctx.fs, ctx.host, instance.Path.relString(), stmt)...)
	}

	dedupers.with(func(deduper *DeDuper) {
		for _, decl := range decls {
			if deduper.add(decl.Name.strID()) {
				globals = append(globals, decl)
			}
		}
	})

	for _, decl := range globals {
		fetchRef = emitResourceFetch(ctx, decl)
	}

	if d.primaryOutput == "" || len(globals) == 0 {
		throwFmt("gen: %s: PREBUILT_PROGRAM has no PRIMARY_OUTPUT/resource", instance.Path.relString())
	}

	if strings.Contains(d.primaryOutput, "${") {
		throwFmt("gen: %s: PREBUILT_PROGRAM PRIMARY_OUTPUT %q has an unresolved reference", instance.Path.relString(), d.primaryOutput)
	}

	srcVFS := build(strings.TrimPrefix(d.primaryOutput, "$(B)/"))
	dst := lDOutputPath(instance, programBinaryName(instance, d.moduleStmt))
	env := envVarsVCS

	var ownSbomRef *NodeRef
	var ownSbomPath *VFS

	if sbomActive(ctx, instance) && sbomQualifies(d) {
		ownSbomRef, ownSbomPath = e.emitSbomComponent(programBinaryName(instance, d.moduleStmt))
	}

	pyRef, pyPath := (*NodeRef)(nil), (*VFS)(nil)

	if sbomActive(ctx, instance) && instance.Platform.BuildRelease {
		pyRef, pyPath = pythonToolchainSbomComponent(ctx, instance.Platform)
	}

	inputs := na.inputs.alloc(3)[:0]
	depRefs := na.noderefs.alloc(3)[:0]

	inputs = append(inputs, ctx.scripts[copyFsToolsVFS.rel()])
	depRefs = append(depRefs, fetchRef)

	if pyRef != nil {
		inputs = append(inputs, na.vfsList(*pyPath))
		depRefs = append(depRefs, *pyRef)
	}

	if ownSbomRef != nil && instance.Platform.BuildRelease {
		inputs = append(inputs, na.vfsList(*ownSbomPath, source(sbomGenScriptRel)))
		depRefs = append(depRefs, *ownSbomRef)
	}

	na.inputs.commit(len(inputs))
	na.noderefs.commit(len(depRefs))

	inputsChunks := InputChunks(inputs[:len(inputs):len(inputs)])

	depRefs = depRefs[:len(depRefs):len(depRefs)]

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
			wrapccPython3STR.any(),
			copyFsToolsVFS.any(),
			argCopy.any(),
			srcVFS.any(),
			dst.any())), Env: env}),
		Env:          env,
		Inputs:       inputsChunks,
		KV:           &resourcesKV,
		Outputs:      na.vfsList(dst),
		DepRefs:      depRefs,
		Resources:    usesPython3,
	}

	ref := e.emitNode(node)

	result := &ModuleEmitResult{
		ModuleStmtName: d.moduleStmt.Name,
		ARRef:          ref,
		ARPath:         &dst,
		isPROGRAM:      true,
		LDRef:          ref,
		LDPath:         &dst,

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
