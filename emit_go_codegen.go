package main

import (
	"crypto/md5"
	"fmt"
	"path"
	"strings"
)

var (
	goMockgenTool   = "vendor/go.uber.org/mock/mockgen"
	goReflectorTool = "library/go/mockgen/reflector"
	strGoModRel     = "go.mod"
	strGoModulesTxt = "vendor/modules.txt"
	mergeSdcmetaVFS = source(mergeSdcmetaRel)
)

var goCodegenKVs = [...]KV{
	goCodegenReflect: {P: pkGR, PC: pcBlue},
	goCodegenMocks:   {P: pkGM, PC: pcBlue},
	goCodegenSource:  {P: pkGS, PC: pcBlue},
	goCodegenOapi:    {P: pkGC, PC: pcBlue},
}

var goOapiCodegenTool = map[TOK]string{
	tokGoOapiCodegen:     "vendor/github.com/deepmap/oapi-codegen/cmd/oapi-codegen",
	tokGoOapiCodegenV2:   "vendor/github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen",
	tokGoOapiCodegenTaxi: "taxi/infra/go/platform/tools/pkg/deepmap/oapi-codegen/cmd/oapi-codegen",
}

const (
	goGatewayV1Tool    = "vendor/github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway"
	goGatewayV1Swagger = "vendor/github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger"
	goGatewayV1Options = "vendor/github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger/options"
	goGatewayV2Tool    = "vendor/github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway"
	goGatewayV2Openapi = "vendor/github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2"
	goGatewayV2Options = "vendor/github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2/options"
)

const (
	goCodegenReflect = iota
	goCodegenMocks
	goCodegenSource
	goCodegenOapi
	goCodegenGateway
	goCodegenYson
)

type GoMockgenVars struct {
	from      string
	module    string
	types     string
	exclude   string
	pkg       string
	pkgIsSet  bool
	reflected bool
}

type GoCodegenStmt struct {
	kind      int
	seq       int
	plugins   []GoProtoOpt
	outs      []string
	tool      string
	file      string
	in        string
	generate  string
	pkg       string
	out       string
	args      []ANY
	inNoParse []ANY
	files     []ANY
}

func goMacroLists(args []ANY, keywords ...string) (positional []ANY, lists map[string][]ANY) {
	lists = map[string][]ANY{}

	current := ""

	for _, a := range args {
		if s := a.string(); s == keywords[0] || len(keywords) > 1 && s == keywords[1] {
			current = s

			continue
		}

		if current == "" {
			positional = append(positional, a)
		} else {
			lists[current] = append(lists[current], a)
		}
	}

	return positional, lists
}

func applyGoCodegenStmt(modulePath string, v UnknownStmt, d *ModuleData, env Environment) {
	std := env.string(envGOSTD)
	m := &d.mockgen

	switch v.Name {
	case tokGoMockgenFrom:
		m.from = v.Args[0].string()
		m.module = goArcPrefix + m.from
	case tokGoMockgenContribFrom:
		m.from = goVendorPrefix + "/" + v.Args[0].string()
		m.module = v.Args[0].string()
	case tokGoMockgenTypes:
		m.types = strings.Join(anyStrings(v.Args), ",")
	case tokGoMockgenExcludeIfaces:
		m.exclude = strings.Join(anyStrings(v.Args), ",")
	case tokGoMockgenPackage:
		m.pkg = v.Args[0].string()
	case tokGoMockgenReflect:
		for _, p := range []string{std + "/encoding/gob", std + "/flag", std + "/fmt", std + "/os", std + "/path", std + "/reflect", "vendor/go.uber.org/mock/mockgen/model", m.from} {
			d.peerdirs = append(d.peerdirs, internStr(p).any())
		}

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{seq: d.nextDeclSeq(), kind: goCodegenReflect, tool: goReflectorTool, out: "main.go"})
	case tokGoMockgenMocks:
		for _, p := range []string{std + "/reflect", "vendor/go.uber.org/mock/gomock"} {
			d.peerdirs = append(d.peerdirs, internStr(p).any())
		}

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{seq: d.nextDeclSeq(), kind: goCodegenMocks, tool: modulePath + "/gen", out: "main.go"})
	case tokGoMockgenSource:
		positional, lists := goMacroLists(v.Args, "ARGS", "IN_NOPARSE")
		file := positional[0].string()

		for _, p := range []string{std + "/reflect", std + "/context", "vendor/go.uber.org/mock/gomock"} {
			d.peerdirs = append(d.peerdirs, internStr(p).any())
		}

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{seq: d.nextDeclSeq(),
			kind: goCodegenSource, tool: goMockgenTool, file: m.from + "/" + file,
			out: strings.TrimSuffix(file, path.Ext(file)) + ".source.gen.go", args: lists["ARGS"], inNoParse: lists["IN_NOPARSE"],
		})
	default:
		positional, lists := goMacroLists(v.Args, "IN_NOPARSE", "OUT_SUFFIX")
		suffix := ""

		if s := lists["OUT_SUFFIX"]; len(s) > 0 {
			suffix = s[0].string()
		}

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{seq: d.nextDeclSeq(),
			kind: goCodegenOapi, tool: goOapiCodegenTool[v.Name],
			generate: positional[0].string(), pkg: positional[1].string(), in: positional[2].string(),
			out: positional[0].string() + suffix + ".gen.go", args: positional[3:], inNoParse: lists["IN_NOPARSE"],
		})
	}
}

func (e *EmitContext) goToolEnv() EnvVars {
	toolsRoot := strGoToolsRoot.string()
	bindir := build(e.instance.Path.relString())

	return EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()},
		{Name: envGOCACHE, Value: internV(bindir.string(), "/.gocache").any()},
		{Name: envGOROOT, Value: strGoToolsRoot.any()},
		{Name: envHOME, Value: internV(bindir.string(), "/.home").any()},
		{Name: cudaPathEnv, Value: internV(toolsRoot, "/bin").any()},
	}
}

func (e *EmitContext) emitGoCodegenStmt(st GoCodegenStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d

	if st.kind == goCodegenYson {
		e.emitYtOrmProtoYson(st)

		return
	}

	if st.kind == goCodegenGateway {
		protoRel := e.goProtoRel(internStr(st.file).any())
		outs := make([]VFS, 0, len(st.outs))

		for _, name := range st.outputNames() {
			outs = append(outs, build(instance.Path.relString(), "/", name))
		}

		e.emitGoProtoCmd(protoRel, nil, st.plugins, outs, st.seq)

		return
	}

	na := ctx.na
	dir := instance.Path.relString()
	bindir := build(dir)
	out := build(dir, "/", st.out)
	toolRef, tool := ctx.tool(internArg(st.tool))
	inputs := []VFS{tool}
	toolRefs := []NodeRef{toolRef}
	pkg := d.mockgen.pkg

	if pkg == "" {
		pkg = "mocks"
	}

	var cmds []Cmd

	env := envVarsVCS

	switch st.kind {
	case goCodegenReflect:
		cmds = []Cmd{{CmdArgs: na.chunkList(na.anyList(tool.any(), internStr(d.mockgen.module).any(), internStr(d.mockgen.types).any())), Cwd: bindir, Env: envVarsVCS, Stdout: out}}
	case goCodegenMocks:
		mockgenRef, mockgen := ctx.tool(internArg(goMockgenTool))

		env = e.goToolEnv()
		inputs = append(inputs, mockgen)
		toolRefs = append(toolRefs, mockgenRef)
		cmds = []Cmd{
			{CmdArgs: na.chunkList(na.anyList(tool.any(), strMockgenOutput.any(), strMockgenGob.any())), Cwd: bindir, Env: envVarsVCS},
			{CmdArgs: na.chunkList(na.anyList(mockgen.any(), strMockgenPkg.any(), internStr(pkg).any(), strMockgenModel.any(), strMockgenGob.any())), Cwd: bindir, Env: env, Stdout: out},
		}
	case goCodegenSource:
		arcadia := build(dir, "/.arcadia")
		fsTools := na.anyList(d.tc.Python3.any(), copyFsToolsVFS.any())
		src := source(st.file)

		env = e.goToolEnv()

		inputs = append(inputs, src)
		inputs = append(inputs, ctx.scripts[copyFsToolsVFS.rel()]...)
		inputs = append(inputs, source(strGoModRel), source(strGoModulesTxt))

		mockgenArgs := []ANY{tool.any(), strMockgenPkg.any(), internStr(pkg).any(), strMockgenSource.any(), internStr(st.file).any()}

		if d.mockgen.exclude != "" {
			mockgenArgs = append(mockgenArgs, internV("-exclude_interfaces=", d.mockgen.exclude).any())
		}

		mockgenArgs = append(mockgenArgs, st.args...)

		for _, f := range st.inNoParse {
			inputs = append(inputs, resolveSourceVFS(ctx, instance, f.string(), d.srcDirs))
		}

		cmds = []Cmd{
			{CmdArgs: na.chunkList(fsTools, na.anyList(strFsToolsMd.any(), build(dir, "/.arcadia/vendor").any())), Env: envVarsVCS},
			{CmdArgs: na.chunkList(fsTools, na.anyList(strFsToolsMd.any(), build(dir, "/.arcadia/", d.mockgen.from).any())), Env: envVarsVCS},
			{CmdArgs: na.chunkList(fsTools, na.anyList(strFsToolsCopy.any(), source(strGoModRel).any(), build(dir, "/.arcadia/go.mod").any())), Env: envVarsVCS},
			{CmdArgs: na.chunkList(fsTools, na.anyList(strFsToolsCopy.any(), source(strGoModulesTxt).any(), build(dir, "/.arcadia/vendor/modules.txt").any())), Env: envVarsVCS},
			{CmdArgs: na.chunkList(fsTools, na.anyList(strFsToolsCopy.any(), src.any(), build(dir, "/.arcadia/", st.file).any())), Env: envVarsVCS},
			{CmdArgs: na.chunkList(mockgenArgs), Cwd: arcadia, Env: env, Stdout: out},
		}
	case goCodegenOapi:
		in := resolveSourceVFS(ctx, instance, st.in, d.srcDirs)
		args := []ANY{tool.any(), strGenerate.any(), internStr(st.generate).any(), strPackage.any(), internStr(st.pkg).any()}

		args = append(args, st.args...)
		args = append(args, in.any())

		env = e.goToolEnv()
		inputs = append(inputs, in)

		for _, f := range st.inNoParse {
			inputs = append(inputs, resolveSourceVFS(ctx, instance, f.string(), d.srcDirs))
		}

		cmds = []Cmd{{CmdArgs: na.chunkList(args), Cwd: bldRootDirVFS, Env: env, Stdout: out}}
	}

	ref := e.emitNode(Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmds...),
		Env:            env,
		Inputs:         na.inputList(inputs),
		KV:             &goCodegenKVs[st.kind],
		Outputs:        na.vfsList(out),
		ForeignDepRefs: toolRefs,
		Resources:      usesPython3,
	})

	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})
	e.collectGoAutoSource(out, st.seq)
}

func applyGoGrpcGatewayStmt(v UnknownStmt, d *ModuleData, env Environment) {
	std := env.string(envGOSTD)
	files := v.Args
	noJSONNames := len(files) > 0 && files[0].string() == "NO_JSON_NAMES_FOR_FIELDS"

	if noJSONNames {
		files = files[1:]
	}

	peer := func(paths ...string) {
		for _, p := range paths {
			d.peerdirs = append(d.peerdirs, internStr(p).any())
		}
	}

	if !isGoModuleType(d.unit.Type) {
		switch v.Name {
		case tokGoGrpcGatewayV2OpenapiSrcs:
			peer(goGatewayV2Options)
		case tokGoGrpcGatewaySwagger:
			peer(goGatewayV1Options)
		}

		return
	}

	v2 := v.Name == tokGoGrpcGatewayV2OpenapiSrcs || v.Name == tokGoGrpcGatewayV2Srcs
	gateway := GoProtoOpt{plugin: "go_grpc_gw", tool: goGatewayV1Tool, opts: []string{"allow_repeated_fields_in_body=true"}}

	if v2 {
		gateway.tool = goGatewayV2Tool
	}

	plugins := []GoProtoOpt{gateway}

	var outs []string

	switch v.Name {
	case tokGoGrpcGatewayV2OpenapiSrcs:
		openapi := GoProtoOpt{plugin: "openapiv2", tool: goGatewayV2Openapi, opts: []string{"logtostderr=true"}}

		if noJSONNames {
			openapi.opts = []string{"json_names_for_fields=false", "logtostderr=true"}
		}

		plugins = append(plugins, openapi)
		outs = append(outs, ".swagger.json")
	case tokGoGrpcGatewaySwagger:
		plugins = append(plugins, GoProtoOpt{plugin: "swagger", tool: goGatewayV1Swagger, opts: []string{"logtostderr=true", "allow_repeated_fields_in_body=true"}})
	}

	outs = append(outs, ".pb.gw.go")

	if v.Name == tokGoGrpcGatewaySwagger {
		outs = append(outs, ".swagger.json")
	}

	for _, f := range files {
		if strings.HasSuffix(f.string(), ".proto") {
			d.goCodegens = append(d.goCodegens, GoCodegenStmt{seq: d.nextDeclSeq(), kind: goCodegenGateway, file: f.string(), plugins: plugins, outs: outs})
		}
	}

	if v2 {
		peer(std+"/context", std+"/errors", std+"/io", std+"/net/http",
			"vendor/github.com/golang/protobuf/descriptor",
			"vendor/github.com/grpc-ecosystem/grpc-gateway/v2/runtime",
			"vendor/github.com/grpc-ecosystem/grpc-gateway/v2/utilities",
			"vendor/google.golang.org/grpc/metadata",
			"vendor/google.golang.org/protobuf/proto")
	} else {
		peer(std+"/context", std+"/io", std+"/net/http",
			"vendor/github.com/golang/protobuf/descriptor",
			"vendor/github.com/grpc-ecosystem/grpc-gateway/runtime",
			"vendor/github.com/grpc-ecosystem/grpc-gateway/utilities",
			"vendor/google.golang.org/grpc/metadata")
	}

	switch v.Name {
	case tokGoGrpcGatewayV2OpenapiSrcs:
		peer(goGatewayV2Options)
	case tokGoGrpcGatewaySwagger:
		peer(goGatewayV1Options)
	}
}

func (st GoCodegenStmt) outputNames() []string {
	if st.kind == goCodegenYson {
		names := make([]string, len(st.files))

		for i, file := range st.files {
			names[i] = strings.TrimSuffix(path.Base(file.string()), ".proto") + ".yson.go"
		}

		return names
	}

	if st.kind != goCodegenGateway {
		return []string{st.out}
	}

	base := strings.TrimSuffix(path.Base(st.file), ".proto")
	names := make([]string, len(st.outs))

	for i, ext := range st.outs {
		names[i] = base + ext
	}

	return names
}

// module_confs.cpp: ParseModuleArgsBase followed by SetDirNameBasenameOrGoPackage,
// before the source module body runs. Keep the filename's header snapshot separate
// from REALPRJNAME, which SET may subsequently change for macros and generators.
func initGoModuleName(modulePath string, env Environment, d *ModuleData, onWarn func(Warn)) {
	stmt := *d.moduleStmt
	args := expandStmtTokens(stmt.Args, env)
	var tail []ANY

	for i := 0; i < len(args); i++ {
		if args[i] == kwPREFIX.any() {
			if i+1 < len(args) {
				env.setString(envMODULE_PREFIX, expandScalarVarRef(args[i+1].string(), env))
				tail = append(tail, args[i:i+2]...)
				i++
			} else {
				onWarn(Warn{Kind: WarnPlugin, Message: fmt.Sprintf("in %s/ya.make, prefix not specified after PREFIX keyword", modulePath)})
			}

			continue
		}

		if !env.hasBindingID(envREALPRJNAME) {
			env.setStringID(envREALPRJNAME, args[i].str())
		} else {
			tail = append(tail, args[i])
		}
	}

	if env.hasBindingID(envREALPRJNAME) {
		env.setString(envGO_PACKAGE_VALUE, expandScalarVarRef(env.string(envREALPRJNAME), env))
	} else {
		env.setString(envREALPRJNAME, path.Base(modulePath))
	}

	if d.setVars == nil {
		d.setVars = map[STR]STR{}
	}

	d.setVars[envREALPRJNAME.str()] = internStr(env.string(envREALPRJNAME))
	d.setVars[envGO_PACKAGE_VALUE.str()] = internStr(env.string(envGO_PACKAGE_VALUE))
	stmt.Args = append([]ANY{internStr(env.string(envREALPRJNAME)).any()}, tail...)
	stmt.Prefix = env.string(envMODULE_PREFIX)
	d.moduleStmt = &stmt
}

func goPackageName(_ string, d *ModuleData) string {
	if name := d.setVars[str_GO_PACKAGE_VALUE].string(); name != "" {
		return name
	}

	if d.moduleStmt.Name == tokGoTest {
		if name := d.setVars[envGO_PACKAGE_VALUE.str()].string(); name != "" {
			return name
		}
	}

	if imp := d.setVars[strGO_TEST_IMPORT_PATH].string(); imp != "" {
		return path.Base(path.Clean(imp))
	}

	if isProgramModuleType(d.moduleStmt.Name) {
		return "main"
	}

	return d.setVars[envREALPRJNAME.str()].string()
}

func applyGoResource(fs FS, modulePath string, entries []ResourceEntry, line int, d *ModuleData) {
	hashArgs := make([]string, 0, 2*len(entries))
	args := make([]ANY, 0, 2*len(entries))

	var inputs []ANY

	for _, entry := range entries {
		key, commandKey := entry.Key, entry.Key

		if entry.SrcPath != "" {
			key = "resfs/src/" + entry.Key + "=${rootrel;context=TEXT;input=TEXT:\"" + entry.SrcPath + "\"}"

			input := copyFileInputVFS(fs, source(modulePath), entry.SrcPath)

			commandKey = "resfs/src/" + entry.Key + "=" + input.relString()
		}

		hashArgs = append(hashArgs, entry.Path, key)

		file := internStr(entry.Path).any()

		if entry.Path != "-" {
			inputs = append(inputs, file)
		}

		args = append(args, file, internStr("notafile"+renderResourceKvCmd(commandKey, modulePath)).any())
	}

	out := internStr(fmt.Sprintf("resource.%x.res.go", md5.Sum([]byte(strings.Join(hashArgs, "@"))))).any()

	rp := &RunProgramStmt{
		ToolPath: strLibraryGoCoreResourceCc.any(),
		Args:     append([]ANY{strMockgenPkg.any(), internStr(goPackageName(modulePath, d)).any(), strO.any(), out}, args...),
		INFiles:  inputs, OUTFiles: []ANY{out}, Line: line, DeclSeq: d.nextDeclSeq(),
	}

	d.runPrograms = append(d.runPrograms, rp)
	d.goPluginPeers = append(d.goPluginPeers, strLibraryGoCoreResource.any())
}

type GoEmbed struct {
	xtest   bool
	root    VFS
	pattern string
	files   []VFS
}

func newGoEmbed(fs FS, xtest bool, importPath, pattern string) GoEmbed {
	base := importPath + "/" + pattern
	em := GoEmbed{xtest: xtest, root: source(importPath), pattern: pattern}

	for _, rel := range concat(ymakeGlob(fs, base+"/**/*", base+"/**/_*", base+"/**/.*"), ymakeGlob(fs, base)) {
		em.files = append(em.files, source(rel))
	}

	return em
}

func newGoEmbedBindir(xtest bool, importPath string, args []ANY) GoEmbed {
	em := GoEmbed{xtest: xtest}

	var files []ANY

	for i := 0; i < len(args); i++ {
		switch args[i].string() {
		case "PREFIX":
			i++

			if i == len(args) {
				throwFmt("GO_EMBED_BINDIR: PREFIX requires a path")
			}

			importPath = args[i].string()
		case "FILES":
			files = args[i+1:]
			i = len(args)
		default:
			if em.pattern != "" {
				throwFmt("GO_EMBED_BINDIR expects one pattern")
			}

			em.pattern = args[i].string()
		}
	}

	if em.pattern == "" {
		throwFmt("GO_EMBED_BINDIR expects a pattern")
	}

	em.root = build(importPath)

	for _, file := range files {
		em.files = append(em.files, build(importPath, "/", file.string()))
	}

	return em
}

func newGoEmbedDir(fs FS, xtest bool, importPath, dir string, all bool) GoEmbed {
	base := importPath + "/" + dir
	em := GoEmbed{xtest: xtest, root: source(importPath), pattern: dir}

	var excludes []string

	if all {
		em.pattern = "all:" + dir
	} else {
		excludes = []string{base + "/**/_*", base + "/**/.*"}
	}

	for _, rel := range ymakeGlob(fs, base+"/**/*", excludes...) {
		em.files = append(em.files, source(rel))
	}

	return em
}

func goEmbedArgs(na *NodeArenas, embeds []GoEmbed) ([]ANY, []VFS) {
	nargs, ninputs := 0, 0

	for _, em := range embeds {
		nargs += 3 + len(em.files)
		ninputs += len(em.files)
	}

	args := na.anys.alloc(nargs)
	inputs := na.vfs.alloc(ninputs)
	ka, ki := 0, 0

	for _, em := range embeds {
		flag := "++embed"

		if em.xtest {
			flag = "++embed_xtest"
		}

		ka += copy(args[ka:], []ANY{internStr(flag).any(), em.root.any(), internStr(em.pattern).any()})

		for _, f := range em.files {
			args[ka] = f.any()
			ka++
		}

		ki += copy(inputs[ki:], em.files)
	}

	na.anys.commit(ka)
	na.vfs.commit(ki)

	return args[:ka:ka], inputs[:ki:ki]
}
