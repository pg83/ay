package main

import (
	"slices"
	"testing"
)

func TestFortranToolchainPreparation(t *testing.T) {
	e := &EmitContext{ctx: &GenCtx{na: newNodeArenas()}, d: &ModuleData{}, peers: &PeerContext{}, instance: ModuleInstance{Platform: &Platform{UsesSDKRoot: true, ifEnv: newEnvironment()}}}
	testThrows(t, "Fortran compiler resource missing", func() { e.prepareFortran() })
	e.peers.ResourceGlobals = []ResourceDecl{
		{GlobalVar: internStr("OTHER_RESOURCE_GLOBAL")},
		{GlobalVar: internStr("FLANG_RESOURCE_GLOBAL"), Name: internStr("FLANG"), Value: build("flang")},
	}
	e.instance.Platform.ifEnv.setString(internEnv("C_FLAGS_PLATFORM"), "-DTEST")
	e.prepareFortran()
	testEqual(t, anyStrs(e.d.cc.FortranHead), []string{"$(B)/flang/bin/flang-new", "-DTEST"})
	testEqual(t, slices.Contains(e.d.cc.FortranResources, internStr(resourcePatternOSSDKRoot)), true)
	e.peers.ResourceGlobals = nil
	e.prepareFortran()
	testEqual(t, anyStrs(e.d.cc.FortranHead), []string{"$(B)/flang/bin/flang-new", "-DTEST"})
}

func TestPchToolchainOverrides(t *testing.T) {
	e := &EmitContext{d: &ModuleData{}, peers: &PeerContext{}, instance: ModuleInstance{Platform: &Platform{Flags: map[ENV]STR{envLlvmToolsRoot: internStr("/platform/llvm")}}}}
	testEqual(t, e.pchLLVMRoot().string(), "/platform/llvm")
	e.d.setVars = map[STR]STR{strLlvmToolsRoot: internStr("/module/llvm")}
	testEqual(t, e.pchLLVMRoot().string(), "/module/llvm")
}

func TestDockerImageValidation(t *testing.T) {
	ctx := &GenCtx{}
	testThrows(t, "separated by '='", func() { ctx.emitDockerImage("docker://image") })
	testThrows(t, "invalid Docker digest", func() { ctx.emitDockerImage("tag=docker://image") })
}

func TestStyleConfigurationErrors(t *testing.T) {
	fs := ContractFS{files: map[string]string{"configs/defaults.json": "{}"}}
	e := &EmitContext{d: &ModuleData{styleChecks: []StyleCheck{{Name: "black", Python: true, ConfigType: "pyproject.toml", ConfigDir: "configs"}}}, ctx: &GenCtx{fs: fs, testMode: true, parsers: newIncludeParserManagerFS(fs, newSharedParseCache())}}
	e.pySrcsReg = []PySrc{{Kind: pySourcePlain, Path: source("app/main.py")}}
	testThrows(t, "missing config configs/pyproject.toml", func() { e.styleSuites(nil) })
	e.d.styleChecks = []StyleCheck{{Name: "black", ConfigMap: "configs/defaults.json"}}
	testThrows(t, "missing black config", func() { e.styleSuites(nil) })
}

func TestPerlSdkLayouts(t *testing.T) {
	for _, tc := range []struct{ sdk, version, prefix string }{{"ubuntu-18", "5.26", "/perl${PERL_VERSION}"}, {"ubuntu-20", "5.30", "/usr"}} {
		version, arch, prefix := perlSDKLayout(tc.sdk)
		testEqual(t, version, tc.version)
		testEqual(t, arch, "/x86_64-linux-gnu")
		testEqual(t, prefix, tc.prefix)
	}
	testEqual(t, perlInputMask(envLocalPerlLibs), uint16(perlLibsField))
}

func TestDllExportsOption(t *testing.T) {
	stmt := &ModuleStmt{Name: tokDll, Args: internAnys([]string{"library", "EXPORTS", "public.exports"})}
	testEqual(t, dllOutputName(ModuleInstance{Path: source("app")}, stmt), "liblibrary.so")
	testEqual(t, dllExportsArg(stmt).string(), "public.exports")
}

func TestFakeModuleOverrides(t *testing.T) {
	e := &EmitContext{d: &ModuleData{setVars: map[STR]STR{strFakeModule: strYes}}}
	testEqual(t, e.moduleIsFake(&ModuleEmitResult{ARRef: 1}), true)
	e.d.setVars = map[STR]STR{strNeedAddFakeSrc: strNo}
	testEqual(t, e.moduleIsFake(&ModuleEmitResult{}), false)
	testEqual(t, e.collectModuleGlobalSource(internStr("header.h").any()), false)
}

func TestIdSetRepeatedSplice(t *testing.T) {
	var set IdSet
	file := source("idset/file.h")
	set.reset(file.strID() + 1)
	out := make([]VFS, 2)
	k := set.spliceOne(file, out, 0)
	testEqual(t, set.spliceOne(file, out, k), 1)
	testEqual(t, out[:k], []VFS{file})
}
