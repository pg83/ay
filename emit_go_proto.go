package main

import (
	"math"
	"path"
	"slices"
	"strings"
)

var goProtoHead = []ANY{
	goProtoWrapperVFS.any(),
	strArcadiaPrefix.any(), internStr(goArcPrefix).any(),
	internStr("--contrib-prefix").any(), internStr(goVendorPrefix + "/").any(),
	internStr("--namespace").any(),
}

var goProtobufInclude = []VFS{contribLibsProtobufSrc}

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

func goGrpcImports(std string) []string {
	return []string{
		std + "/context",
		"vendor/google.golang.org/grpc",
		"vendor/google.golang.org/grpc/codes",
		"vendor/google.golang.org/grpc/grpclog",
		"vendor/google.golang.org/grpc/status",
	}
}

func goModuleHasProtos(d *ModuleData) bool {
	return slices.ContainsFunc(d.srcs, func(m SrcMeta) bool {
		c := srcExtClassOf(m.Source.relOrSelf().any())

		return c == srcExtProto || c == srcExtEv
	})
}

func (e *EmitContext) goProtoRel(src ANY) STR {
	if v := src.vfs(); v != 0 && v.isSource() {
		return v.rel()
	}

	return e.protoSourceRel(e.moduleSourceNameANY(src))
}

func (e *EmitContext) goProtoNamespace() string {
	if e.d.protoNamespace != nil {
		return e.d.protoNamespace.string()
	}

	return ""
}

func (e *EmitContext) emitGoProtoCmd(protoRel STR, goOpts *string, plugins []GoProtoOpt, outs []VFS, seq int) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	protocRef, protoc := e.tool(argContribToolsProtoc)
	ns := e.goProtoNamespace()
	tools := []NodeRef{protocRef}
	toolPaths := []VFS{protoc}
	env := envVarsVCS
	args := []ANY{internV("-I=./", ns).any(), internV("-I=$(S)/", ns).any(), argIB2.any(), argIS3.any()}

	for _, p := range concat(dedup(d.protoAddInclGlobal, d.protoAddIncl, goProtobufInclude), e.peers.ProtoInclude) {
		args = append(args, internV("-I=", p.prefix(), p.relString()).any())
	}

	args = append(args, argIB2.any(), argISContribLibsProtobufSrc.any())
	args = append(args, d.protocFlags...)

	if goOpts != nil {
		ref, gen := e.tool(argVendorProtocGenGo)

		tools = append(tools, ref)
		toolPaths = append(toolPaths, gen)

		args = append(args,
			internV("--plugin=protoc-gen-go=", gen.prefix(), gen.relString()).any(),
			internV("--go_opt=", *goOpts).any(),
			internV("--go_out=$(B)/", ns).any(),
		)
	}

	for _, opt := range plugins {
		if opt.plugin == "" {
			args = append(args, opt.arg)

			continue
		}

		if len(env) == len(envVarsVCS) && (strings.HasSuffix(opt.plugin, "mdb-validators") || strings.HasSuffix(opt.plugin, "proto_validation") || strings.HasSuffix(opt.plugin, "mdb-dynforms")) {
			env = e.goToolEnv(false)
		}

		ref, tool := e.tool(internArg(opt.tool))

		tools = append(tools, ref)
		toolPaths = append(toolPaths, tool)

		args = append(args,
			internV("--plugin=protoc-gen-", opt.plugin, "=", tool.prefix(), tool.relString()).any(),
			internV("--", opt.plugin, "_out=$(B)/", ns).any(),
		)

		for _, o := range opt.opts {
			args = append(args, internV("--", opt.plugin, "_opt=", o).any())
		}
	}

	cmd := na.chunkList(
		na.anyList(d.tc.Python3),
		goProtoHead,
		na.anyList(internV("./", ns).any(), strProto.any(), protoRel.any(), arg2.any(), protoc.any()),
		na.anyList(args...),
	)

	protoSrc := e.rootSourceOrGeneratedID(protoRel)
	cwd := srcRootDirVFS

	if protoSrc.isBuild() {
		cwd = bldRootDirVFS
	}

	view := e.scanner.walkClosure(protoSrc, d.scanCtx, scanDomainProto)
	closure := view.bucketList()
	inputs := na.inputs.alloc(3 + len(closure))[:0]

	inputs = append(inputs, na.vfsList(toolPaths...), na.vfsList(goProtoWrapperVFS), na.vfsList(protoSrc))
	inputs = append(inputs, closure...)
	na.inputs.commit(len(inputs))

	ref := e.emitNode(NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmd, Cwd: cwd.cwd(), Env: env}),
		Env:  env, Inputs: InputChunks(inputs[:len(inputs):len(inputs)]), Outputs: na.vfsList(outs...),
		KV:             &pbKV,
		ForeignDepRefs: na.noderefs.list(tools...),
		Resources:      usesPython3}, input: InputSpec{roots: InputChunks(inputs[1:len(inputs):len(inputs)])},
	})

	for _, out := range outs {
		e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, ProducerMainOut: outs[0]})

		if srcExtClassOf(out.any()) == srcExtGo {
			e.collectGoAutoSource(out, seq)
		}
	}
}

func (e *EmitContext) emitGoEvSource(meta SrcMeta) {
	name := e.moduleSourceName(meta.Source)
	dst := build(e.instance.Path.relString(), "/", name, ".proto")

	e.emitGoCopyWithContext(e.sourceForEmission(meta), dst, scanDomainProto)
	e.emitGoProtoSource(SrcMeta{Source: dst.any()})
}

func (e *EmitContext) emitGoProtoSource(meta SrcMeta) {
	d := e.d
	dir := e.instance.Path.relString()
	protoRel := e.goProtoRel(meta.Source)
	base := strings.TrimSuffix(path.Base(protoRel.string()), ".proto")
	outs := []VFS{build(dir, "/", base, ".pb.go")}
	goOpts := ""

	if d.grpc {
		goOpts = "plugins=grpc"
	}

	for _, opt := range d.goProtoOpts {
		if opt.plugin != "" {
			outs = append(outs, build(dir, "/", base, opt.ext))
		}
	}

	e.emitGoProtoCmd(protoRel, &goOpts, d.goProtoOpts, outs, math.MaxInt)
}

type GoProtoOpt struct {
	arg    ANY
	plugin string
	ext    string
	tool   string
	opts   []string
}
