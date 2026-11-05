package main

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/zeebo/xxh3"
)

var (
	javaUnitContrib = &javaUnit{kind: juContrib, sub: KindJavaMain, suffix: ".jar"}
	javaUnitProxy   = &javaUnit{kind: juProxy, sub: KindJavaMain}
	javaUnitJar     = &javaUnit{kind: juJar, sub: KindJavaMain, suffix: ".jar", compileView: true}
	javaUnitTestLib = &javaUnit{kind: juJar, sub: KindJavaMain, suffix: ".jar", compileView: true}
	javaUnitFull    = &javaUnit{kind: juJar, sub: KindJavaFull, suffix: ".jar", compileView: true}
	javaUnitIJar    = &javaUnit{kind: juIJar, sub: KindJavaIJar, suffix: "-interface.jar", transparent: true, ignorePeerdir: true, selfPeers: []ModuleKind{KindJavaFull}}
	javaUnitExt     = &javaUnit{kind: juExternal, sub: KindJavaMain, nameGen: javaNameThreeDirs, suffix: ".jar"}
	javaUnitProto   = &javaUnit{kind: juExternal, sub: KindJavaMain, nameGen: javaNameThreeDirs, suffix: ".jar", proto: true}
	javaUnitFbs     = &javaUnit{kind: juJar, sub: KindJavaMain, suffix: ".jar", fbs: true}
	javaUnitDllJar  = &javaUnit{kind: juJar, sub: KindJavaMain, suffix: ".jar", compileView: true, jni: true}
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
	tokJavaProgram:                    {javaCompilation(), javaRunnable(false)},
	tokJavaAnnotationProcessor:        {javaCompilation(), javaRunnable(false)},
	tokJavaContribProgram:             {javaContribCompilation(), javaRunnable(true)},
	tokJavaContribAnnotationProcessor: {javaContribCompilation(), javaRunnable(true)},
	tokJunit5:                         javaTestUnits("junit5"),
	tokJunit6:                         javaTestUnits("junit6"),
	tokJtest:                          javaTestUnits("junit4"),
	tokJtestFor:                       javaTestUnits("junit4"),
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

func (u *javaUnit) confTag(conf *Conf, name TOK) string {
	if sub := multimoduleSub(name.str(), u.sub, LangJava, false); sub != 0 {
		for _, variant := range conf.peerVariants(name.str()) {
			if variant.sub == sub {
				return variant.tag.string()
			}
		}
	}

	return conf.moduleTag(name.str()).string()
}

func javaTags(conf *Conf, env Environment, key ENV) []ANY {
	if raw := env.stringID(key); raw != 0 {
		return conf.expandArgs(env, raw)
	}

	return nil
}

func javaTagIn(tag STR, tags []ANY) bool {
	for _, t := range tags {
		if t == tag.any() || t == strEmptyTag.any() && tag.string() == "" {
			return true
		}
	}

	return false
}

func javaRunnable(contrib bool) *javaUnit {
	return &javaUnit{kind: juRunnable, sub: KindJavaRunnable, suffix: ".run.cp.jar", consumeNonManageable: true, selfPeers: []ModuleKind{KindJavaCompilation}, contribRunnable: contrib}
}

func javaCompilation() *javaUnit {
	return &javaUnit{kind: juJar, sub: KindJavaCompilation, suffix: ".jar", compileView: true}
}

func javaContribCompilation() *javaUnit {
	u := *javaUnitContrib

	u.sub = KindJavaCompilation

	return &u
}

func javaTestUnits(runner string) []*javaUnit {
	testable := &javaUnit{kind: juTest, sub: KindJavaTestable, nameGen: javaNameFullPath, suffix: ".test.cp.jar", consumeNonManageable: true, selfPeers: []ModuleKind{KindJavaCompilation}, testRunner: runner}
	comp := &javaUnit{kind: juJar, sub: KindJavaCompilation, nameGen: javaNameFullPath, suffix: ".jar", compileView: true, testRunner: runner}

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

func (d *JavaData) nextDeclSeq() int {
	d.declSeq++

	return d.declSeq
}

type JavaData struct {
	declSeq                int
	setVars                SetVars
	confModule             *confModule
	confMacroDepth         int
	inModuleBody           bool
	peerdirTags            []ANY
	dmTagsExclude          []ANY
	moduleTag              STR
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
	dm                     []ANY
	excludes               []ANY
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
	rawBindings            []confMacroBinding
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
	commandExts            []string
	commandPeers           []string
	modver                 string
	varPeers               []ANY
	validateData           bool
	fakeModule             bool
	moduleType             string
	sbrUIDExt              string
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
	parallelWorkers  string
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
	id                  uint32
	self                uint32
	lib                 uint32
	file                uint32
	dir                 string
	path                VFS
	instance            ModuleInstance
	unit                *javaUnit
	stmt                TOK
	name                string
	target              VFS
	data                *JavaData
	contribWithVer      bool
	passPeers           bool
	proxyFor            *JavaModule
	implPeers           map[string]bool
	managedPeers        []*JavaModule
	unmanageable        []javaForeignPeer
	incDirs             javaIncDirs
	selfPeers           []*JavaModule
	direct              []javaResolvedPeer
	closure             javaClosure
	closureReady        bool
	rules               *javaDMRules
	managed             javaManaged
	managedDone         bool
	unmanageableDart    []javaForeignPeer
	unmanageableDone    bool
	included            *ModuleIncludedDeps
	incState            javaIncludedState
	incInputs           bool
	incRecipe           *javaIncludedRecipe
	globals             []ResourceDecl
	resourceGlobals     []ResourceDecl
	globalsByName       map[string]VFS
	resourceGlobalsDone bool
	result              *ModuleEmitResult
	outRef              NodeRef
	protoInclude        []VFS
	toolRefs            []NodeRef
	gentars             []VFS
	jsrcs               []VFS
	macroOuts           []VFS
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

	ctx.jc.register(jm)

	jm.data = collectJavaData(ctx, jm, stmts, env)
	jm.rules = ctx.javaDMRulesFor(jm)

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

	ctx.recordConfiguration(instance, nil, jm.data.test.depends)

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

	if unit.transparent {
		jm.direct = make([]javaResolvedPeer, 0, len(jm.managedPeers))

		for _, p := range jm.managedPeers {
			jm.direct = append(jm.direct, javaResolvedPeer{m: p, resolution: jprUnversioned, impl: jm.implPeers[p.dir]})
		}

		ctx.jc.begin()

		for _, d := range jm.direct {
			ctx.jc.merge(d.m, nil)
		}

		jm.closure = ctx.jc.commit()
	} else {
		jm.direct = ctx.javaManageLocalPeers(jm, jm.managedPeers, jm.rules)
		jm.closure = ctx.javaMergeClosure(jm)
	}

	jm.closureReady = true
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

func (ctx *GenCtx) javaDMRulesFor(jm *JavaModule) *javaDMRules {
	d := jm.data

	var dm, excludes []ANY

	if !jm.unit.transparent {
		dm, excludes = d.dm, d.excludes
	}

	r := ctx.javaDMRulesOf(dm, excludes)

	ctx.javaDMBuf, ctx.javaExclBuf = d.dm[:0], d.excludes[:0]
	d.dm, d.excludes = nil, nil

	return r
}

func javaAnyListHash(list []ANY) uint64 {
	if len(list) == 0 {
		return 0
	}

	return xxh3.Hash(sliceBytes(list))
}

func (ctx *GenCtx) javaDMRulesOf(dm, excludes []ANY) *javaDMRules {
	key := javaAnyListHash(dm) ^ mix64(javaAnyListHash(excludes)+uint64(len(dm)))

	if key == 0 {
		key = 1
	}

	same, _ := ctx.javaRules.cell(key)

	for _, r := range *same {
		if slices.Equal(r.dm, dm) && slices.Equal(r.excludes, excludes) {
			return r
		}
	}

	r := &javaDMRules{jc: &ctx.jc, lib2Ver: make(map[uint32]uint32, len(dm)), dm: slices.Clone(dm), excludes: slices.Clone(excludes), excluded: map[uint32]struct{}{}}

	for _, item := range dm {
		r.addDependencyManagement(ctx.jc.anyPathID(item))
	}

	for _, e := range excludes {
		r.addExclude(e.string())
	}

	*same = append(*same, r)

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

		res := ctx.javaPeerResult(jm, internStr(dir), explicit)

		if res == nil {
			return
		}

		if res.java != nil {
			jm.managedPeers = append(jm.managedPeers, res.java)
		} else {
			jm.unmanageable = append(jm.unmanageable, javaForeignPeer{dir: dir, res: res})
		}
	}

	for _, p := range jm.data.varPeers {
		if p.string() != jm.dir {
			add(p.string(), false)
		}
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

func (jm *JavaModule) peerdirTags() []ANY {
	return jm.data.peerdirTags
}

func (ctx *GenCtx) javaPeerResult(parent *JavaModule, dir STR, explicit bool) *ModuleEmitResult {
	if !ctx.fs.isFileID(dir, strYaMake) {
		ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: PEERDIR to directory without ya.make: %s", parent.dir, dir.string())})

		return nil
	}

	inst := ModuleInstance{Path: dir.source(), Language: LangJava, Platform: parent.instance.Platform, Demand: demandSelf}
	name := ctx.moduleNameVFS(inst.Path)
	tags := parent.peerdirTags()

	switch units := javaModuleUnits[name]; {
	case len(units) > 1:
		picked := false

		for _, variant := range inst.Platform.conf.peerVariants(name.str()) {
			if javaTagIn(variant.tag, tags) {
				inst.Kind, picked = multimoduleShapes[name.str()][variant.sub].kind, true

				break
			}
		}

		if !picked {
			ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: PEERDIR to %s is prohibited by incompatible tags and skipped", parent.dir, dir.string())})

			return nil
		}
	case len(units) == 1:
		if explicit && !javaTagIn(inst.Platform.conf.moduleTag(name.str()), tags) {
			ctx.onWarn(Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: PEERDIR to %s is prohibited by incompatible tags and skipped", parent.dir, dir.string())})

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
	dirID := internStr(dir)

	if !ctx.fs.isFileID(dirID, strYaMake) {
		return nil
	}

	res := ctx.javaPeerResult(parent, dirID, false)

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

	if p.res.GlobalPath != nil && p.res.GlobalRef != nil {
		return *p.res.GlobalPath
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

	for _, check := range d.styleChecks {
		if !check.Python && check.Peer != "" {
			out = append(out, check.Peer)
		}
	}

	out = append(out, d.commandPeers...)

	for _, l := range d.customLints {
		out = append(out, l.linter.resourcePeer)
	}

	for _, check := range d.styleChecks {
		if check.Python {
			out = append(out, check.Peer)
		}
	}

	return out
}
