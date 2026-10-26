package main

import (
	"fmt"
	"path"
	"strings"
)

var (
	javaTagsJarBase = []string{"JAVA", "JAVA_PROTO", "JAVA_PROTO_FROM_SCHEMA", "JAVA_FBS", "JAVA_IDL", "DLL", "JAR_COMPILATION", "__EMPTY__", "RESOURCE_LIB", "JUNIT6_JAR_COMPILATION", "JUNIT5_JAR_COMPILATION", "JTEST_JAR_COMPILATION", "JTEST_FOR_JAR_COMPILATION", "JAVA_TEST_LIBRARY"}
	javaTagsIJar    = []string{"IJAR"}
	javaTagsFull    = []string{"FULL_JAR_COMPILATION"}
	javaUnitContrib = &javaUnit{kind: juContrib, sub: KindJavaMain, tag: "JAVA", moduleType: "JAVA_CONTRIB", peerdirTags: javaTags(javaTagsJarBase, javaTagsFull), dmTagsExclude: javaTagsIJar, suffix: ".jar"}
	javaUnitProxy   = &javaUnit{kind: juProxy, sub: KindJavaMain, tag: "JAVA", moduleType: "JAVA_CONTRIB_PROXY", peerdirTags: javaTagsJarBase}
	javaUnitJar     = &javaUnit{kind: juJar, sub: KindJavaMain, tag: "JAVA", moduleType: "JAVA_LIBRARY", peerdirTags: javaTags(javaTagsJarBase, javaTagsIJar), dmTagsExclude: javaTagsFull, suffix: ".jar", compileView: true}
	javaUnitTestLib = &javaUnit{kind: juJar, sub: KindJavaMain, tag: "JAVA_TEST_LIBRARY", moduleType: "JAVA_TEST_LIBRARY", peerdirTags: javaTags(javaTagsJarBase, javaTagsIJar, []string{"JAVA_TEST_LIBRARY"}), dmTagsExclude: javaTagsFull, suffix: ".jar", compileView: true}
	javaUnitFull    = &javaUnit{kind: juJar, sub: KindJavaFull, tag: "FULL_JAR_COMPILATION", moduleType: "JAVA_LIBRARY", peerdirTags: javaTags(javaTagsJarBase, javaTagsIJar), dmTagsExclude: javaTagsFull, suffix: ".jar", compileView: true}
	javaUnitIJar    = &javaUnit{kind: juIJar, sub: KindJavaIJar, tag: "IJAR", moduleType: "JAVA_LIBRARY", peerdirTags: javaTagsJarBase, suffix: "-interface.jar", transparent: true, ignorePeerdir: true, selfPeers: []ModuleKind{KindJavaFull}}
	javaUnitExt     = &javaUnit{kind: juExternal, sub: KindJavaMain, tag: "JAVA", moduleType: "EXTERNAL_JAVA_LIBRARY", peerdirTags: []string{"JAVA", "JAVA_PROTO", "JAVA_PROTO_FROM_SCHEMA", "JAVA_FBS", "JAVA_IDL", "PACKAGE_UNION", "IJAR"}, dmTagsExclude: javaTagsFull, nameGen: javaNameThreeDirs, suffix: ".jar"}
	javaUnitProto   = &javaUnit{kind: juExternal, sub: KindJavaMain, tag: "JAVA_PROTO", moduleType: "PROTO_LIBRARY", peerdirTags: []string{"JAVA", "IJAR", "JAVA_PROTO"}, dmTagsExclude: javaTagsFull, nameGen: javaNameThreeDirs, suffix: ".jar", proto: true}
	javaUnitFbs     = &javaUnit{kind: juJar, sub: KindJavaMain, tag: "JAVA_FBS", moduleType: "FBS_LIBRARY", peerdirTags: javaTags(javaTagsJarBase, javaTagsIJar), dmTagsExclude: javaTagsFull, suffix: ".jar", fbs: true}
	javaUnitDllJar  = &javaUnit{kind: juJar, sub: KindJavaMain, tag: "JAR_COMPILATION", moduleType: "JAR_LIBRARY", peerdirTags: javaTags(javaTagsJarBase, javaTagsIJar), dmTagsExclude: javaTagsFull, suffix: ".jar", compileView: true, jni: true}
)

var javaModuleUnits = map[TOK][]*javaUnit{
	tokJavaContrib:                    {javaUnitContrib},
	tokJavaContribProxy:               {javaUnitProxy},
	tokJavaLibraryNonSplit:            {javaUnitJar},
	tokJarLibrary:                     {javaUnitJar},
	tokJavaTestLibrary:                {javaUnitTestLib},
	tokJavaLibrary:                    {javaUnitFull, javaUnitIJar},
	tokJavaLibrarySplit:               {javaUnitFull, javaUnitIJar},
	tokExternalJavaLibrary:            {javaUnitExt},
	tokJavaProgram:                    {javaCompilation("JAVA_PROGRAM"), javaRunnable("JAVA_PROGRAM", false, javaTagsIJar)},
	tokJavaAnnotationProcessor:        {javaCompilation("JAVA_ANNOTATION_PROCESSOR"), javaRunnable("JAVA_ANNOTATION_PROCESSOR", false, nil)},
	tokJavaContribProgram:             {javaContribCompilation("JAVA_PROGRAM"), javaRunnable("JAVA_PROGRAM", true, nil)},
	tokJavaContribAnnotationProcessor: {javaContribCompilation("JAVA_ANNOTATION_PROCESSOR"), javaRunnable("JAVA_ANNOTATION_PROCESSOR", true, nil)},
	tokJunit5:                         javaTestUnits("JUNIT5", "JUNIT5_JAR_COMPILATION", "junit5", []string{"JUNIT5_JAR_COMPILATION", "JAVA_TEST_LIBRARY"}),
	tokJunit6:                         javaTestUnits("JUNIT6", "JUNIT6_JAR_COMPILATION", "junit6", []string{"JUNIT6_JAR_COMPILATION", "JAVA_TEST_LIBRARY"}),
	tokJtest:                          javaTestUnits("JTEST", "JTEST_JAR_COMPILATION", "junit4", []string{"JTEST_JAR_COMPILATION", "JAVA_TEST_LIBRARY"}),
	tokJtestFor:                       javaTestUnits("JTEST_FOR", "JTEST_FOR_JAR_COMPILATION", "junit4", []string{"JTEST_FOR_JAR_COMPILATION", "JTEST_JAR_COMPILATION", "JAVA_TEST_LIBRARY"}),
	tokDllJava:                        {javaUnitDllJar},
}

const (
	KindJavaMain ModuleKind = iota + 2
	KindJavaFull
	KindJavaIJar
	KindJavaRunnable
	KindJavaCompilation
	KindJavaTestable
)

const (
	juContrib javaUnitKind = iota + 1
	juProxy
	juJar
	juIJar
	juRunnable
	juTest
	juExternal
)

const (
	javaNameTwoDirs javaNameGen = iota
	javaNameThreeDirs
	javaNameFullPath
)

type javaUnitKind uint8

type javaNameGen uint8

type javaUnit struct {
	kind                 javaUnitKind
	sub                  ModuleKind
	tag                  string
	moduleType           string
	peerdirTags          []string
	dmTagsExclude        []string
	nameGen              javaNameGen
	suffix               string
	transparent          bool
	consumeNonManageable bool
	ignorePeerdir        bool
	selfPeers            []ModuleKind
	compileView          bool
	proto                bool
	fbs                  bool
	contribRunnable      bool
	testRunner           string
	jni                  bool
}

func javaTags(groups ...[]string) []string {
	var out []string

	for _, g := range groups {
		out = append(out, g...)
	}

	return out
}

func javaTagIn(tag string, tags []string) bool {
	for _, t := range tags {
		if t == tag || t == "__EMPTY__" && tag == "" {
			return true
		}
	}

	return false
}

func javaRunnable(moduleType string, contrib bool, dmExclude []string) *javaUnit {
	tags := javaTags(javaTagsJarBase, javaTagsFull)

	if contrib {
		tags = javaTagsJarBase
	}

	return &javaUnit{kind: juRunnable, sub: KindJavaRunnable, tag: "JAR_RUNNABLE", moduleType: moduleType, peerdirTags: tags, dmTagsExclude: dmExclude, suffix: ".run.cp.jar", consumeNonManageable: true, selfPeers: []ModuleKind{KindJavaCompilation}, contribRunnable: contrib}
}

func javaCompilation(moduleType string) *javaUnit {
	return &javaUnit{kind: juJar, sub: KindJavaCompilation, tag: "JAR_COMPILATION", moduleType: moduleType, peerdirTags: javaTags(javaTagsJarBase, javaTagsIJar), dmTagsExclude: javaTagsFull, suffix: ".jar", compileView: true}
}

func javaContribCompilation(moduleType string) *javaUnit {
	u := *javaUnitContrib

	u.sub = KindJavaCompilation
	u.tag = "JAR_COMPILATION"
	u.moduleType = moduleType

	return &u
}

func javaTestUnits(moduleType, compTag, runner string, extraCompTags []string) []*javaUnit {
	testable := &javaUnit{kind: juTest, sub: KindJavaTestable, tag: "JAR_TESTABLE", moduleType: moduleType, peerdirTags: javaTags(javaTagsJarBase, []string{"JAR_TESTABLE", "JAVA_TEST_LIBRARY", "FULL_JAR_COMPILATION"}), dmTagsExclude: javaTagsIJar, nameGen: javaNameFullPath, suffix: ".test.cp.jar", consumeNonManageable: true, selfPeers: []ModuleKind{KindJavaCompilation}, testRunner: runner}
	comp := &javaUnit{kind: juJar, sub: KindJavaCompilation, tag: compTag, moduleType: moduleType, peerdirTags: javaTags(javaTagsJarBase, javaTagsIJar, extraCompTags), dmTagsExclude: javaTagsFull, nameGen: javaNameFullPath, suffix: ".jar", compileView: true, testRunner: runner}

	return []*javaUnit{comp, testable}
}

func isJavaModuleType(name TOK) bool {
	_, ok := javaModuleUnits[name]

	return ok
}

func javaUnitFor(name TOK, sub ModuleKind) *javaUnit {
	if name == tokProtoLibrary {
		return javaUnitProto
	}

	if name == tokFbsLibrary {
		return javaUnitFbs
	}

	for _, u := range javaModuleUnits[name] {
		if u.sub == sub {
			return u
		}
	}

	return nil
}

type JavaClosureEntry struct {
	path    VFS
	java    *JavaModule
	foreign *ModuleEmitResult
}

type javaForeignPeer struct {
	dir string
	res *ModuleEmitResult
}

type javaIncDirs struct {
	localUserGlobal      []VFS
	userGlobal           []VFS
	global               []VFS
	userGlobalPropagated []VFS
	globalPropagated     []VFS
}

func javaPushUniq(dst []VFS, vs ...VFS) []VFS {
	for _, v := range vs {
		found := false

		for _, d := range dst {
			if d == v {
				found = true

				break
			}
		}

		if !found {
			dst = append(dst, v)
		}
	}

	return dst
}

func (inc *javaIncDirs) add(dir VFS, global bool) {
	inc.localUserGlobal = javaPushUniq(inc.localUserGlobal, dir)

	if global {
		inc.userGlobal = javaPushUniq(inc.userGlobal, dir)
		inc.global = javaPushUniq(inc.global, dir)
	}
}

func (inc *javaIncDirs) propagateTo(other *javaIncDirs) {
	other.userGlobalPropagated = javaPushUniq(other.userGlobalPropagated, inc.userGlobal...)
	other.userGlobalPropagated = javaPushUniq(other.userGlobalPropagated, inc.globalPropagated...)
	other.globalPropagated = javaPushUniq(other.globalPropagated, inc.global...)
	other.globalPropagated = javaPushUniq(other.globalPropagated, inc.globalPropagated...)
}

func (inc *javaIncDirs) get() []VFS {
	return append(append([]VFS(nil), inc.localUserGlobal...), inc.userGlobalPropagated...)
}

type javaAddIncl struct {
	dir    VFS
	global bool
}

type JavaSrcsSpec struct {
	resources       bool
	srcDir          string
	packagePrefix   string
	excludes        []string
	files           []string
	globs           []string
	skipCheckSrcdir bool
	varSrcDir       bool
	salt            string
}

type JavaPeerSpec struct {
	dir      string
	explicit bool
}

type JavaRunProgram struct {
	args []string
	raw  []string
}

type JavaData struct {
	autoincluded           bool
	recurses               []string
	name                   string
	prefix                 string
	jdkReal                string
	hasPerModuleJDK        bool
	withKotlin             bool
	withKapt               bool
	kaptOpts               []string
	kaptProcessors         []string
	kaptAPClasspath        []string
	kaptAPOptions          []string
	allopen                bool
	lombok                 bool
	noarg                  bool
	serialization          bool
	detekt                 bool
	kotlincOpts            javaCondVar
	javacOpts              javaCondVar
	abiJar                 bool
	errorProne             bool
	uberjar                bool
	uberjarOpts            []string
	javaSrcs               []JavaSrcsSpec
	peerStmts              []JavaPeerSpec
	dm                     []string
	excludes               []string
	jarResource            string
	srcResource            string
	localJar               string
	localSourcesJar        string
	protoSrcs              []string
	flatcFlags             []ANY
	evSrcs                 []string
	copyFiles              []CopyFileEntry
	runPrograms            []*RunProgramStmt
	bundles                []BundleEntry
	runPython              []*RunPythonStmt
	fromSandboxes          []*FromSandboxStmt
	files                  []string
	srcDirs                []VFS
	srcs                   []string
	kotlinProto            bool
	javaProtobufPeers      []string
	javaProtoCommonVersion string
	javaProtoArgs          []JavaProtoPlugin
	protocFlags            []string
	protoNamespace         string
	protoAddIncl           []javaAddIncl
	excludeTags            map[string]bool
	onlyTags               bool
	commonGoogleAPIs       bool
	lintLevel              string
	noLint                 string
	rawVars                map[string]string
	annotationProcs        []string
	jarMainClass           string
	jvmArgs                []string
	runJavaPrograms        []JavaRunProgram
	generateScripts        [][]string
	withJDK                bool
	externalDeps           []string
	addDLLs                bool
	disableScriptgen       bool
	enablePreview          bool
	unittestDir            string
	unittestMod            string
	implPeers              []string
	collectKotlinLint      []string
	customLints            []javaCustomLint
	styleChecks            []StyleCheck
	ktlintOld              bool
	ktlintSkipSourceDirs   bool
	checkJavaDeps          string
	testEnv                []string
	test                   javaTestData
	sourcesJar             bool
	embedSbom              bool
	needSbomInfo           bool
	sbomComponent          bool
	modver                 string
	noYmakePython3         bool
	env                    Environment
}

type javaTestData struct {
	dockerImages     []string
	recipes          []string
	depends          []string
	size             string
	timeout          string
	tags             []string
	requirements     []string
	forkMode         string
	splitFactor      string
	experimentalFork bool
	systemProperties []string
	classpathCmdType string
	data             []string
	cwd              string

	ignoreClasspathClash []string
}

type JavaProtoPlugin struct {
	name       string
	tool       string
	kotlinGrpc bool
}

type JavaModule struct {
	dir              string
	path             VFS
	instance         ModuleInstance
	unit             *javaUnit
	stmt             TOK
	name             string
	target           VFS
	data             *JavaData
	contribWithVer   bool
	passPeers        bool
	proxyFor         *JavaModule
	implPeers        map[string]bool
	managedPeers     []*JavaModule
	unmanageable     []javaForeignPeer
	incDirs          javaIncDirs
	selfPeers        []*JavaModule
	direct           []javaResolvedPeer
	closure          *javaPeersClosure
	rules            *javaDMRules
	managed          []JavaClosureEntry
	managedDone      bool
	unmanageableDart []javaForeignPeer
	unmanageableDone bool
	included         *ModuleIncludedDeps
	globals          []ResourceDecl
	result           *ModuleEmitResult
	outRef           NodeRef
	protoInclude     []VFS
	toolRefs         []NodeRef
	gentars          []VFS
	jsrcs            []VFS
	macroOuts        []VFS
}

func (jm *JavaModule) fileName() string {
	return jm.target.relString()
}

func shortenFullPath(dir string) string {
	limit := max(98-len(dir), 0)

	if len(dir) <= limit {
		return dir
	}

	pos := strings.IndexByte(dir[len(dir)-limit-1:], '/')

	if pos >= 0 {
		pos += len(dir) - limit - 1
	} else {
		pos = strings.LastIndexByte(dir, '/')
	}

	if pos < 0 {
		return dir
	}

	return dir[pos+1:]
}

func javaRealPrjName(dir string, gen javaNameGen) string {
	parts := strings.Split(dir, "/")

	switch gen {
	case javaNameFullPath:
		return strings.ReplaceAll(shortenFullPath(dir), "/", "-")
	case javaNameThreeDirs:
		if len(parts) > 3 {
			parts = parts[len(parts)-3:]
		}
	default:
		if len(parts) > 2 {
			parts = parts[len(parts)-2:]
		}
	}

	return strings.Join(parts, "-")
}

func (ctx *GenCtx) javaDefaultJDK() string {
	return "17"
}

func genJavaModule(ctx *GenCtx, instance ModuleInstance, moduleName TOK, stmts []Stmt, env Environment) *ModuleEmitResult {
	unit := javaUnitFor(moduleName, instance.Kind)

	if unit == nil {
		throwFmt("gen: %s: no Java submodule %d for %s", instance.Path.relString(), instance.Kind, moduleName.string())
	}

	dir := instance.Path.relString()

	jm := &JavaModule{
		dir:            dir,
		path:           instance.Path,
		instance:       instance,
		unit:           unit,
		stmt:           moduleName,
		contribWithVer: !unit.transparent && javaPathIsPrefixOf("contrib/java", dir),
		passPeers:      true,
	}

	jm.data = collectJavaData(ctx, jm, stmts, env)

	if unit.fbs {
		if jm.data.excludeTags["JAVA_FBS"] {
			return &ModuleEmitResult{ModuleStmtName: moduleName}
		}

		native := instance

		native.Language = LangFbsJavaNative
		native.Kind = KindLib
		native.Demand = demandSelf
		genModule(ctx, native)
	}

	if unit.proto && jm.data.excludeTags[env.string(envMODULE_TAG)] {
		r := &ModuleEmitResult{ModuleStmtName: moduleName}

		ctx.memoPut(instance, r)

		return r
	}

	ctx.recordConfiguration(instance, jm.data.recurses, jm.data.test.depends)

	jm.name = jm.data.name

	if jm.name == "" {
		jm.name = javaRealPrjName(dir, unit.nameGen)
	}

	jm.target = build(dir, "/", jm.data.prefix, jm.name, unit.suffix)

	if jm.data.uberjar {
		jm.passPeers = false
	}

	result := &ModuleEmitResult{ModuleStmtName: moduleName, java: jm}

	jm.result = result

	ctx.memoPut(instance, result)
	ctx.javaResolvePeers(jm)

	jm.rules = jm.dmRules()

	if unit.transparent {
		jm.direct = make([]javaResolvedPeer, 0, len(jm.managedPeers))

		for _, p := range jm.managedPeers {
			jm.direct = append(jm.direct, javaResolvedPeer{m: p, resolution: jprUnversioned, impl: jm.implPeers[p.dir]})
		}

		jm.closure = newJavaPeersClosure()

		for _, d := range jm.direct {
			jm.closure.merge(d.m, emptyJavaPeersClosure, 1)
		}
	} else {
		jm.direct = ctx.javaManageLocalPeers(jm, jm.managedPeers, jm.rules)
		jm.closure = jm.mergeClosure(jm.rules)
	}

	jm.globals = javaOwnGlobals(jm.unmanageable)

	jm.setupIncDirs()

	ctx.javaRegisterOutputs(jm, ctx.frames[ctx.frameDepth-1])

	if ctx.peerTests {
		for _, peer := range jm.direct {
			result.testPeers = append(result.testPeers, peer.m.result)
		}

		for _, peer := range jm.unmanageable {
			result.testPeers = append(result.testPeers, peer.res)
		}
	}

	return result
}

func (jm *JavaModule) dmRules() *javaDMRules {
	r := &javaDMRules{lib2Ver: map[string]string{}}

	if jm.unit.transparent {
		return r
	}

	for _, item := range jm.data.dm {
		r.addDependencyManagement(item)
	}

	r.excludes = append(r.excludes, jm.data.excludes...)

	return r
}

func javaOwnGlobals(peers []javaForeignPeer) []ResourceDecl {
	var out []ResourceDecl

	seen := map[STR]bool{}

	for _, p := range peers {
		for _, decl := range p.res.ResourceGlobalClosure {
			if !seen[decl.GlobalVar] {
				seen[decl.GlobalVar] = true
				out = append(out, decl)
			}
		}
	}

	return out
}

func (ctx *GenCtx) javaResolvePeers(jm *JavaModule) {
	if jm.unit.jni {
		dll := genModule(ctx, ModuleInstance{Path: jm.path, Kind: KindLib, Language: LangCPP, Platform: jm.instance.Platform, Demand: demandLinked})

		jm.unmanageable = append(jm.unmanageable, javaForeignPeer{dir: jm.dir, res: dll})
	}

	for _, sub := range jm.unit.selfPeers {
		self := jm.instance

		self.Kind = sub

		res := genModule(ctx, self)

		if res.java == nil {
			throwFmt("gen: %s: submodule %d of %s failed", jm.dir, sub, jm.stmt.string())
		}

		jm.selfPeers = append(jm.selfPeers, res.java)
		jm.managedPeers = append(jm.managedPeers, res.java)
	}

	seen := map[string]bool{}

	add := func(dir string, explicit bool) {
		dir = path.Clean(dir)

		if dir == "" || dir == "." || seen[dir] {
			return
		}

		seen[dir] = true

		res := ctx.javaPeerResult(jm, dir, explicit)

		if res == nil {
			return
		}

		if res.java != nil {
			jm.managedPeers = append(jm.managedPeers, res.java)
		} else {
			jm.unmanageable = append(jm.unmanageable, javaForeignPeer{dir: dir, res: res})
		}
	}

	for _, p := range jm.varPeers() {
		add(p, false)
	}

	if jm.unit.ignorePeerdir {
		return
	}

	if !jm.unit.jni {
		for _, p := range jm.data.peerStmts {
			add(p.dir, p.explicit)
		}
	}

	for _, p := range jm.macroPeers() {
		add(p, false)
	}

	if len(jm.data.implPeers) > 0 {
		jm.implPeers = map[string]bool{}

		for _, p := range jm.data.implPeers {
			jm.implPeers[path.Clean(p)] = true
		}
	}
}

func (jm *JavaModule) peerdirTags() []string {
	if jm.data.uberjar {
		return javaTags(javaTagsJarBase, javaTagsFull)
	}

	return jm.unit.peerdirTags
}

func (ctx *GenCtx) javaPeerResult(parent *JavaModule, dir string, explicit bool) *ModuleEmitResult {
	if !ctx.fs.isFile(internStr(dir), "ya.make") {
		ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: PEERDIR to directory without ya.make: %s", parent.dir, dir)})

		return nil
	}

	name := ctx.moduleName(dir)
	tags := parent.peerdirTags()
	inst := ModuleInstance{Path: source(dir), Language: LangJava, Platform: parent.instance.Platform, Demand: demandSelf}

	switch units := javaModuleUnits[name]; {
	case len(units) > 1:
		var picked *javaUnit

		for _, u := range units {
			if javaTagIn(u.tag, tags) {
				picked = u

				break
			}
		}

		if picked == nil {
			ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: PEERDIR to %s is prohibited by incompatible tags and skipped", parent.dir, dir)})

			return nil
		}

		inst.Kind = picked.sub
	case len(units) == 1:
		if explicit && !javaTagIn(units[0].tag, tags) {
			ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: PEERDIR to %s is prohibited by incompatible tags and skipped", parent.dir, dir)})

			return nil
		}

		inst.Kind = units[0].sub
	case name == tokProtoLibrary || name == tokFbsLibrary:
		inst.Kind = KindJavaMain
	default:
		inst.Language = LangCPP
		inst.Kind = KindLib
		inst.Demand = demandLinked
	}

	if explicit && !parent.instance.Platform.allowPeer(parent.instance.Path.rel(), inst.Path.rel(), ctx.onWarn) {
		return nil
	}

	res := genModule(ctx, inst)

	if res.java == nil && inst.Language == LangJava {
		return nil
	}

	if units := javaModuleUnits[name]; len(units) > 1 {
		for _, u := range units {
			if u.sub != inst.Kind {
				sibling := inst

				sibling.Kind = u.sub
				genModule(ctx, sibling)
			}
		}
	}

	return res
}

func (ctx *GenCtx) javaContrib(parent *JavaModule, dir string) *JavaModule {
	if !ctx.fs.isFile(internStr(dir), "ya.make") {
		return nil
	}

	res := ctx.javaPeerResult(parent, dir, false)

	if res == nil {
		return nil
	}

	return res.java
}

func javaForeignFileName(p javaForeignPeer) VFS {
	if p.res.LDPath != nil {
		return *p.res.LDPath
	}

	if p.res.ModuleStmtName == tokResourcesLibrary {
		return build(p.dir, "/", javaRealPrjName(p.dir, javaNameThreeDirs), ".pkg.fake")
	}

	return p.res.ArchiveFile
}

func (u *javaUnit) finalTarget() bool {
	return u.sub != KindJavaCompilation && u.kind != juIJar && u.kind != juExternal && u.kind != juProxy
}

func (ctx *GenCtx) genJavaTarget(dir string, name TOK) {
	units := javaModuleUnits[name]

	for _, u := range units {
		if len(units) > 1 && !u.finalTarget() {
			continue
		}

		res := genModule(ctx, ModuleInstance{Path: source(dir), Kind: u.sub, Language: LangJava, Platform: ctx.target, Demand: demandSelf})

		if res.java == nil {
			continue
		}

		if ref := ctx.javaBuilder(res.java).use(res.java.target); ref != 0 {
			ctx.foreignTools.startRefs = append(ctx.foreignTools.startRefs, ref)
			ctx.emit.result(ref)
		}
	}
}

func (ctx *GenCtx) genJavaTests(dir string, name TOK) {
	if !ctx.testMode {
		return
	}

	for _, u := range javaModuleUnits[name] {
		res := genModule(ctx, ModuleInstance{Path: source(dir), Kind: u.sub, Language: LangJava, Platform: ctx.target, Demand: demandSelf})

		if res.java == nil {
			continue
		}

		ctx.testRoots = append(ctx.testRoots, res)
	}
}

func (jm *JavaModule) setupIncDirs() {
	d := jm.data

	if jm.unit.proto {
		if d.commonGoogleAPIs {
			jm.incDirs.add(contribLibsGoogleapisCommonProtos, true)
		}

		jm.incDirs.add(contribLibsProtobufSrc, false)
	}

	for _, a := range d.protoAddIncl {
		jm.incDirs.add(a.dir, a.global)
	}

	for _, p := range jm.managedPeers {
		p.incDirs.propagateTo(&jm.incDirs)
	}

	jm.protoInclude = jm.incDirs.get()
}

func (jm *JavaModule) macroPeers() []string {
	d := jm.data

	var out []string

	if jm.unit.kind == juJar && d.lintLevel != "none" && d.noLint != "none" && len(d.javaSrcs) > 0 {
		out = append(out, "build/platform/java/jdk/jdk"+javaConfGlobals["JDK_LATEST_VERSION"])
	}

	if jm.unit.kind == juJar && len(d.runJavaPrograms) > 0 {
		out = append(out, "build/platform/java/jdk", "build/platform/java/jdk/jdk"+d.jdkReal)
	}

	for _, check := range d.styleChecks {
		if check.Peer != "" {
			out = append(out, check.Peer)
		}
	}

	if jm.unit.proto && len(d.protoSrcs) > 0 {
		out = append(out, "build/platform/java/protoc")

		for _, src := range d.protoSrcs {
			if extIsGztproto(src) {
				out = append(out, strKernelGazetteerProto.string())

				break
			}
		}
	}

	for _, l := range d.customLints {
		out = append(out, l.linter.resourcePeer)
	}

	if jm.unit.jni && len(d.swigSrcs()) > 0 {
		out = append(out, strContribToolsSwigLibJava.string())

		if !d.env.bool(envUSE_SYSTEM_JDK) && !d.env.bool(envOS_ANDROID) {
			out = append(out, strContribLibsJdk.string())
		}
	}

	return out
}
