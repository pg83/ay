package main

import (
	"path"
	"slices"
	"strings"
)

var goProtoHead = []ANY{
	goProtoWrapperVFS.any(),
	internStr("--arcadia-prefix").any(), internStr(goArcPrefix).any(),
	internStr("--contrib-prefix").any(), internStr(goVendorPrefix + "/").any(),
	internStr("--namespace").any(),
}

var goCommonGoogleApis = []string{
	"api/annotations", "api/configchange", "api/distribution", "api/expr/v1alpha1", "api/expr/v1beta1",
	"api/httpbody", "api/label", "api/metric", "api/monitoredres", "api/serviceconfig",
}

func goProtobufImports(std *GoStd) []string {
	return []string{
		std.Prefix + "/sync",
		std.Prefix + "/reflect",
		std.Prefix + "/fmt",
		std.Prefix + "/math",
		"vendor/github.com/golang/protobuf/proto",
		"vendor/google.golang.org/protobuf/types/descriptorpb",
		"vendor/google.golang.org/protobuf/runtime/protoimpl",
		"vendor/google.golang.org/protobuf/runtime/protoiface",
		"vendor/google.golang.org/protobuf/reflect/protoreflect",
		"vendor/google.golang.org/protobuf/types/known/anypb",
		"vendor/google.golang.org/protobuf/types/known/apipb",
		"vendor/google.golang.org/protobuf/types/known/durationpb",
		"vendor/google.golang.org/protobuf/types/known/emptypb",
		"vendor/google.golang.org/protobuf/types/known/fieldmaskpb",
		"vendor/google.golang.org/protobuf/types/known/sourcecontextpb",
		"vendor/google.golang.org/protobuf/types/known/structpb",
		"vendor/google.golang.org/protobuf/types/known/timestamppb",
		"vendor/google.golang.org/protobuf/types/known/typepb",
		"vendor/google.golang.org/protobuf/types/known/wrapperspb",
	}
}

func goGrpcImports(std *GoStd) []string {
	return []string{
		std.Prefix + "/context",
		"vendor/google.golang.org/grpc",
		"vendor/google.golang.org/grpc/codes",
		"vendor/google.golang.org/grpc/grpclog",
		"vendor/google.golang.org/grpc/status",
	}
}

func goModuleHasProtos(d *ModuleData) bool {
	return slices.ContainsFunc(d.srcs, func(m SrcMeta) bool {
		return !m.Global && srcExtClassOf(m.Source.relOrSelf().any()) == srcExtProto
	})
}

func (e *EmitContext) emitGoProtoSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()
	protoRel := e.protoSourceRelPath(e.moduleSourceName(meta.Source))
	protocRef, protoc := ctx.tool(argContribToolsProtoc)
	genRef, gen := ctx.tool(argVendorProtocGenGo)
	ns := ""

	if d.protoNamespace != nil {
		ns = d.protoNamespace.string()
	}

	base := strings.TrimSuffix(path.Base(protoRel), ".proto")
	outs := []VFS{build(dir, "/", base, ".pb.go")}
	tools := []NodeRef{protocRef, genRef}
	toolPaths := []VFS{protoc, gen}
	plugins := ""

	if d.grpc {
		plugins = "plugins=grpc"
	}

	args := []ANY{internV("-I=./", ns).any(), internV("-I=$(S)/", ns).any(), argIB2.any(), argIS3.any()}

	for _, p := range e.peers.ProtoInclude {
		if token := internV("-I=", p.prefix(), p.relString()).any(); !slices.Contains(args, token) {
			args = append(args, token)
		}
	}

	if !slices.Contains(args, argISContribLibsProtobufSrc.any()) {
		args = append(args, argISContribLibsProtobufSrc.any())
	}

	args = append(args, argIB2.any(), argISContribLibsProtobufSrc.any())
	args = append(args, d.protocFlags...)

	args = append(args,
		internV("--plugin=protoc-gen-go=", gen.prefix(), gen.relString()).any(),
		internV("--go_opt=", plugins).any(),
		internV("--go_out=$(B)/", ns).any(),
	)

	for _, opt := range d.goProtoOpts {
		if opt.plugin == "" {
			args = append(args, opt.arg)

			continue
		}

		ref, tool := ctx.tool(internArg(opt.tool))

		tools = append(tools, ref)
		toolPaths = append(toolPaths, tool)
		outs = append(outs, build(dir, "/", base, opt.ext))

		args = append(args,
			internV("--plugin=protoc-gen-", opt.plugin, "=", tool.prefix(), tool.relString()).any(),
			internV("--", opt.plugin, "_out=$(B)/", ns).any(),
		)
	}

	cmd := na.chunkList(
		na.anyList(d.tc.Python3.any()),
		goProtoHead,
		na.anyList(internV("./", ns).any(), strProto.any(), internStr(protoRel).any(), arg2.any(), protoc.any()),
		na.anyList(args...),
	)

	protoSrc := source(protoRel)
	cwd := srcRootDirVFS

	if e.codegen.use(build(protoRel)) != nil {
		protoSrc = build(protoRel)
		cwd = bldRootDirVFS
	}

	closure := e.scanner.walkClosure(source(protoRel), d.scanCtx, scanDomainProto).bucketList()
	inputs := na.inputs.alloc(2 + len(closure))[:0]

	inputs = append(inputs, na.vfsList(toolPaths...), na.vfsList(goProtoWrapperVFS, protoSrc))
	inputs = append(inputs, closure...)
	na.inputs.commit(len(inputs))

	ref := e.emitNode(Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: cmd, Cwd: cwd, Env: envVarsVCS}),
		Env:            envVarsVCS,
		Inputs:         InputChunks(inputs[:len(inputs):len(inputs)]),
		Outputs:        na.vfsList(outs...),
		KV:             &pbKV,
		ForeignDepRefs: na.noderefs.list(tools...),
		Resources:      usesPython3,
	})

	for _, out := range outs {
		e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})
		e.collectGoAutoSource(out)
	}
}

type GoProtoOpt struct {
	arg    ANY
	plugin string
	ext    string
	tool   string
}

func applyGoCommonGoogleApis(args []ANY, d *ModuleData) {
	const genproto = "vendor/google.golang.org/genproto/googleapis/"

	if len(args) == 0 {
		for _, api := range goCommonGoogleApis {
			d.peerdirs = append(d.peerdirs, internStr(genproto+api).any())
		}

		for _, p := range []string{
			"vendor/cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb",
			"vendor/cloud.google.com/go/servicemanagement/apiv1/servicemanagementpb",
			"vendor/cloud.google.com/go/longrunning/autogen/longrunningpb",
		} {
			d.peerdirs = append(d.peerdirs, internStr(p).any())
		}

		for _, api := range []string{
			"iam/admin/v1", "iam/credentials/v1", "iam/v1", "iam/v1/logging", "logging/type", "logging/v2",
			"rpc/code", "rpc/errdetails", "rpc/status", "type/calendarperiod", "type/color", "type/date",
			"type/date_range", "type/datetime", "type/date_time_range", "type/dayofweek", "type/decimal",
			"type/expr", "type/fraction", "type/interval", "type/latlng", "type/localized_text", "type/money",
			"type/month", "type/phone_number", "type/postaladdress", "type/quaternion", "type/timeofday",
		} {
			d.peerdirs = append(d.peerdirs, internStr(genproto+api).any())
		}
	} else {
		for _, a := range args {
			d.peerdirs = append(d.peerdirs, internStr(genproto+a.string()).any())
		}
	}

	d.protoAddInclGlobal = append(d.protoAddInclGlobal, contribLibsGoogleapisCommonProtos)
}
