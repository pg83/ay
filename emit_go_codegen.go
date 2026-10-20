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

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{kind: goCodegenReflect, tool: goReflectorTool, out: "main.go"})
	case tokGoMockgenMocks:
		for _, p := range []string{std + "/reflect", "vendor/go.uber.org/mock/gomock"} {
			d.peerdirs = append(d.peerdirs, internStr(p).any())
		}

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{kind: goCodegenMocks, tool: modulePath + "/gen", out: "main.go"})
	case tokGoMockgenSource:
		positional, lists := goMacroLists(v.Args, "ARGS", "IN_NOPARSE")
		file := positional[0].string()

		for _, p := range []string{std + "/reflect", std + "/context", "vendor/go.uber.org/mock/gomock"} {
			d.peerdirs = append(d.peerdirs, internStr(p).any())
		}

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{
			kind: goCodegenSource, tool: goMockgenTool, file: m.from + "/" + file,
			out: strings.TrimSuffix(file, path.Ext(file)) + ".source.gen.go", args: lists["ARGS"], inNoParse: lists["IN_NOPARSE"],
		})
	default:
		positional, lists := goMacroLists(v.Args, "IN_NOPARSE", "OUT_SUFFIX")
		suffix := ""

		if s := lists["OUT_SUFFIX"]; len(s) > 0 {
			suffix = s[0].string()
		}

		d.goCodegens = append(d.goCodegens, GoCodegenStmt{
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

	if st.kind == goCodegenGateway {
		protoRel := e.goProtoRel(internStr(st.file).any())
		outs := make([]VFS, 0, len(st.outs))

		for _, name := range st.outputNames() {
			outs = append(outs, build(instance.Path.relString(), "/", name))
		}

		e.emitGoProtoCmd(protoRel, nil, st.plugins, outs)

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
	e.collectGoAutoSource(out)
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
			d.goCodegens = append(d.goCodegens, GoCodegenStmt{kind: goCodegenGateway, file: f.string(), plugins: plugins, outs: outs})
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

func goPackageName(modulePath string, d *ModuleData) string {
	if name := d.setVars[str_GO_PACKAGE_VALUE]; name != 0 {
		return name.string()
	}

	if imp := d.setVars[strGO_TEST_IMPORT_PATH]; imp != 0 {
		return path.Base(path.Clean(imp.string()))
	}

	if isProgramModuleType(d.moduleStmt.Name) {
		return "main"
	}

	if len(d.moduleStmt.Args) > 0 {
		return d.moduleStmt.Args[0].string()
	}

	return path.Base(modulePath)
}

func applyGoResource(modulePath string, v *ResourceStmt, d *ModuleData) {
	args := make([]string, 0, 2*len(v.Pairs))

	for _, p := range v.Pairs {
		args = append(args, p.Path, p.Key)
	}

	out := internStr(fmt.Sprintf("resource.%x.res.go", md5.Sum([]byte(strings.Join(args, "@"))))).any()

	rp := &RunProgramStmt{
		ToolPath: strLibraryGoCoreResourceCc.any(),
		Args:     []ANY{strMockgenPkg.any(), internStr(goPackageName(modulePath, d)).any(), strO.any(), out},
		OUTFiles: []ANY{out},
		Line:     v.Line,
		DeclSeq:  d.nextDeclSeq(),
	}

	for _, p := range v.Pairs {
		rp.Args = append(rp.Args, internStr(p.Path).any(), internStr("notafile"+p.Key).any())

		if p.Path != "-" {
			rp.INFiles = append(rp.INFiles, internStr(p.Path).any())
		}
	}

	d.runPrograms = append(d.runPrograms, rp)
	d.peerdirs = append(d.peerdirs, strLibraryGoCoreResource.any())
}
