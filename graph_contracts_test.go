package main

import (
	"slices"
	"testing"
)

func TestCodegenProducerRegistration(t *testing.T) {
	r := newCodegenRegistry(newNodeArenas())
	out := build("pkg/generated.h")
	owner := source("pkg")

	testThrows(t, "register of a source path", func() { r.register(GeneratedFileInfo{OutputPath: source("pkg/header.h")}) })
	testEqual(t, r.moduleType(source("pkg")), TOK(0))
	testEqual(t, r.moduleType(out), TOK(0))
	testEqual(t, r.moduleInputs(out), (*ModuleInputInfo)(nil))
	testEqual(t, r.lookupSplit(build("pkg"), ANY(0)), (*GeneratedFileInfo)(nil))
	r.setModuleInputs(out, tokLibrary, nil, nil, nil, true)
	testEqual(t, r.lookupSTR(out.rel()), (*GeneratedFileInfo)(nil))
	testEqual(t, r.lookupSplit(build("pkg"), internStr("generated.h").any()), (*GeneratedFileInfo)(nil))
	registered := r.register(GeneratedFileInfo{OutputPath: out, OwnerModule: owner, OwnerSerial: 1, ProducerRef: 1})
	testEqual(t, r.lookupSplit(build("pkg"), internStr("generated.h").any()), registered)
	testThrows(t, "duplicate producer", func() {
		r.register(GeneratedFileInfo{OutputPath: out, OwnerModule: owner, OwnerSerial: 1, ProducerRef: 2})
	})
	other := r.register(GeneratedFileInfo{OutputPath: out, OwnerModule: owner, OwnerSerial: 2, ProducerRef: 3})
	testEqual(t, other.ProducerRef, NodeRef(3))
	testEqual(t, r.lookup(out).ProducerRef, NodeRef(1))

	// An unrelated producer must not overwrite inherited parsing metadata.
	inherited := ParsedIncludeSet{parsedIncludesAll: {{target: includeTarget(internStr("keep.h").any())}}}
	r.inheritParsedIncludes(out, 2, inherited)
	testEqual(t, len(registered.ParsedIncludes[parsedIncludesAll]), 0)
	r.inheritParsedIncludes(out, 1, inherited)
	testEqual(t, registered.ParsedIncludes[parsedIncludesAll], inherited[parsedIncludesAll])
}

func TestPendingEmitterRunsOnce(t *testing.T) {
	calls := 0
	frame := &ModuleFrame{pending: 1}
	p := &PendingEmit{frame: frame}
	p.fn = func() {
		calls++
		p.fire()
	}

	p.fire()
	p.fire()
	testEqual(t, calls, 1)
	testEqual(t, frame.pending, 0)
}

func TestJoinedArchitectureIncludes(t *testing.T) {
	own := []VFS{source("own")}
	peer := []VFS{source("contrib/libs/musl/arch/x86_64"), source("independent")}
	ctx := &ScanContext{ccOwn: own, ccPeer: peer, joinFrom: ISAX8664, joinTo: ISAAArch64}
	a, b := ctx.modulePaths(scanDomainJoinTarget)
	testEqual(t, a, own)
	testEqual(t, b, []VFS{source("contrib/libs/musl/arch/aarch64"), source("independent")})
	testEqual(t, peer[0], source("contrib/libs/musl/arch/x86_64"))
}

func TestCachedScannerDiagnostics(t *testing.T) {
	var got []Warn
	s := &IncludeScanner{onWarn: func(w Warn) { got = append(got, w) }}
	warning := Warn{Kind: WarnModuleFailed, Message: "missing include"}

	s.reportDiagnostics(nil)
	testEqual(t, len(got), 0)
	s.reportDiagnostics(&IncludeDiagnostics{Entries: []IncludeDiagnostic{{Warning: warning}}})
	testEqual(t, got, []Warn{warning})
}

func TestDllForInvalidTargets(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "expects a source directory"},
		{[]string{"$(B)/generated"}, "expects a source directory"},
		{[]string{"app"}, "current directory"},
		{[]string{"recursive"}, "recursive module inclusion"},
	} {
		ctx := &GenCtx{dllForStack: []string{"recursive"}}
		testThrows(t, tc.want, func() {
			withDllForStmts(ctx, "app", &UnknownStmt{Args: internAnys(tc.args)}, func([]Stmt) { t.Fatal("invalid target was visited") })
		})
	}
}

func TestConditionalModuleCopies(t *testing.T) {
	original := &ModuleStmt{Name: tokLibrary}
	conditional := &IfStmt{Then: []Stmt{original}, Else: []Stmt{original}}
	got := appendDllForModule(nil, []Stmt{conditional}, internAnys([]string{"dynamic"}))[0].(*IfStmt)
	testEqual(t, got.Then[0].(*ModuleStmt).Name, tokDll)
	testEqual(t, got.Else[0].(*ModuleStmt).Name, tokDll)
	testEqual(t, original.Name, tokLibrary)
	goTest := appendGoTestForModule(nil, []Stmt{conditional})[0].(*IfStmt)
	testEqual(t, len(goTest.Then), len(goTest.Else))
	testEqual(t, original.Name, tokLibrary)
}

func TestFortranGeneratedSources(t *testing.T) {
	out := internStr("generated.f").any()
	for _, d := range []*ModuleData{
		{runPython: []*RunPythonStmt{{OUTFiles: []ANY{out}}}},
		{fromArchives: []FromArchiveEntry{{ArchiveExtraction: ArchiveExtraction{OUTFiles: []ANY{out}}}}},
		{runPrograms: []*RunProgramStmt{{StdoutFile: &out}}},
	} {
		testEqual(t, hasFortranGeneratedSource(d), true)
	}

	testEqual(t, hasFortranGeneratedSource(&ModuleData{runPrograms: []*RunProgramStmt{{StdoutFile: &out, StdoutNoAuto: true}}}), false)
}

func TestYqlPrebuiltVariants(t *testing.T) {
	for sanitizer, variant := range map[string]string{"": "DEFAULT", "address": "ASAN", "memory": "MSAN", "thread": "TSAN", "undefined": "UBSAN"} {
		env := newEnvironment()
		env.setString(envSANITIZER_TYPE, sanitizer)
		env.setString(internEnv("TOOL_RESOURCE_"+variant), "42")
		d := &ModuleData{moduleStmt: &ModuleStmt{Name: tokYqlPrebuiltProgram, Args: internAnys([]string{"tool"})}, peerdirs: internAnys([]string{"dep"})}
		finishYqlModule("app", env, d)
		testEqual(t, d.yqlPrebuiltResource, "42")
		testEqual(t, len(d.peerdirs), 0)
		testEqual(t, d.flags.NoLibc && d.flags.NoPlatform && d.flags.NoRuntime && d.flags.NoUtil, true)
	}

	for _, resource := range []string{"", "None"} {
		env := newEnvironment()
		env.setString(internEnv("APP_RESOURCE_DEFAULT"), resource)
		d := &ModuleData{moduleStmt: &ModuleStmt{Name: tokYqlPrebuiltProgram}}
		finishYqlModule("app", env, d)
		testEqual(t, d.yqlPrebuiltResource, "")
	}

	d := &ModuleData{}
	env := newEnvironment()
	applyYqlABI(d, env, false)
	applyYqlABI(d, env, true)
	testEqual(t, slices.Contains(d.peerdirs, strYqlEssentialsPublicUdfAbiConflict.any()), true)
	e := &EmitContext{d: &ModuleData{}, instance: ModuleInstance{Path: source("app")}}
	testThrows(t, "requires a resolved PRIMARY_OUTPUT", func() { e.genYqlPrebuiltTool() })
}
