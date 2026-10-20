package main

import (
	"slices"
	"strings"
)

var (
	userverGrpcKV    = KV{P: pkUG, PC: pcYellow}
	userverStructsKV = KV{P: pkRP}
)

const userverStructsTool = "taxi/uservices/userver-arc-utils/scripts/proto_structs"

type UserverProtoFile struct {
	path     string
	includes []ANY
}

type UserverProtoBatch struct {
	mode             string
	structs          bool
	namespace        string
	descriptor       string
	descriptorTarget string
	optsFile         string
	origins          []ANY
	files            []UserverProtoFile
}

func applyUserverProto(v UnknownStmt, d *ModuleData) {
	batch := UserverProtoBatch{}

	if d.protoNamespace != nil {
		batch.namespace = d.protoNamespace.string()
	}

	if v.Name != tokUsrvGenProtoStructs {
		batch.mode = "service"
		batch.structs = v.Name == tokUsrvGenGrpcClientV2Structs || v.Name == tokUsrvGenGrpcServiceV2Structs

		if v.Name == tokUsrvGenGrpcClientV2 || v.Name == tokUsrvGenGrpcClientV2Structs {
			batch.mode = "client"
		}

		for i := 0; i < len(v.Args); i++ {
			if v.Args[i].string() == "FROM" {
				i++

				if i < len(v.Args) {
					d.peerdirs = append(d.peerdirs, v.Args[i])
				}
			} else {
				batch.files = append(batch.files, UserverProtoFile{path: v.Args[i].string()})
			}
		}
	} else {
		mode := ""

		for i := 0; i < len(v.Args); i++ {
			arg := v.Args[i].string()

			switch arg {
			case "OPTS_FILE", "PROTODESC", "PROTODESC_NAME", "FILE":
				i++

				if i == len(v.Args) {
					throwFmt("USRV_GEN_PROTO_STRUCTS: %s requires a value", arg)
				}

				value := v.Args[i].string()

				switch arg {
				case "OPTS_FILE":
					batch.optsFile = value
				case "PROTODESC", "PROTODESC_NAME":
					if batch.descriptor != "" {
						throwFmt("USRV_GEN_PROTO_STRUCTS: PROTODESC and PROTODESC_NAME are mutually exclusive")
					}

					batch.descriptor = value

					if arg == "PROTODESC" {
						batch.descriptorTarget = value
						batch.descriptor = "uproto-structs-" + moddirHash(value) + ".protodesc"
					}
				case "FILE":
					batch.files = append(batch.files, UserverProtoFile{path: value})
				}

				mode = ""
			case "ORIGINS", "FILES", "IMPORTS", "OUTPUT_INCLUDES":
				mode = arg
			default:
				switch mode {
				case "ORIGINS":
					batch.origins = append(batch.origins, v.Args[i])
				case "FILES":
					batch.files = append(batch.files, UserverProtoFile{path: strings.TrimPrefix(arg, "$S/")})
				case "OUTPUT_INCLUDES":
					if len(batch.files) == 0 {
						throwFmt("USRV_GEN_PROTO_STRUCTS: OUTPUT_INCLUDES requires FILE")
					}

					file := &batch.files[len(batch.files)-1]

					file.includes = append(file.includes, v.Args[i])
				case "IMPORTS":
				default:
					throwFmt("USRV_GEN_PROTO_STRUCTS: unknown argument %s", arg)
				}
			}
		}

		if batch.descriptor == "" {
			throwFmt("USRV_GEN_PROTO_STRUCTS: PROTODESC or PROTODESC_NAME is required")
		}
	}

	if len(batch.origins) == 0 {
		batch.addModuleDeps(d)
	}

	d.userverProtos = append(d.userverProtos, batch)
}

func (e *EmitContext) prepareUserverProtos() {
	d := e.d

	for i := range d.userverProtos {
		batch := &d.userverProtos[i]

		if len(batch.origins) == 0 {
			continue
		}

		batch.files = nil

		for _, peer := range d.peerdirs {
			if !slices.Contains(batch.origins, peer) {
				continue
			}

			genModule(e.ctx, e.derivePeerInstance(peer.string()))

			for _, file := range e.codegen.moduleSourceInputs(source(peer.string()), ".proto") {
				batch.files = append(batch.files, UserverProtoFile{path: file.relString()})
			}
		}

		batch.addModuleDeps(d)
	}
}

func (batch UserverProtoBatch) addModuleDeps(d *ModuleData) {
	if len(batch.files) == 0 {
		return
	}

	peer := "taxi/uservices/userver/grpc"

	if batch.mode == "" {
		peer = "taxi/uservices/userver/libraries/proto-structs"

		if batch.descriptorTarget != "" {
			d.bundles = append(d.bundles, BundleEntry{Target: batch.descriptorTarget, Name: batch.descriptor})
		}
	} else if batch.structs {
		peer = "taxi/uservices/userver/libraries/grpc-proto-structs"
	}

	d.peerdirs = append(d.peerdirs, internStr(peer).any())
}

func (batch UserverProtoBatch) outputs(file string) []VFS {
	base := strings.TrimSuffix(file, ".proto")

	if batch.mode == "" {
		return []VFS{build(base, ".structs.usrv.pb.hpp"), build(base, ".structs.usrv.pb.cpp")}
	}

	return []VFS{build(base, "_", batch.mode, ".usrv.pb.cpp"), build(base, "_", batch.mode, ".usrv.pb.hpp")}
}

func (e *EmitContext) emitUserverProtos(batch UserverProtoBatch) {
	if len(batch.files) == 0 {
		return
	}

	ctx := e.ctx
	na := ctx.na
	protocRef, protoc := ctx.tool(argContribToolsProtoc)
	toolPath := userverStructsTool

	if batch.mode != "" {
		toolPath = "taxi/uservices/userver-arc-utils/scripts/grpc/arc-generate-" + batch.mode
	}

	toolRef, tool := ctx.tool(internArg(toolPath))

	for _, file := range batch.files {
		file := file
		src := source(file.path)

		if e.codegen.use(build(file.path)) != nil {
			src = build(file.path)
		}

		outputs := na.vfsList(batch.outputs(file.path)...)
		includes := []IncludeDirective{{kind: includeQuoted, target: includeTarget(internStr(strings.TrimSuffix(file.path, ".proto") + ".pb.h").any())}}

		if batch.mode == "" {
			for _, inc := range ctx.parsers.sourceParsedBuckets(src, nil).bucket(parsedIncludesLocal) {
				includes = append(includes, IncludeDirective{kind: inc.kind,
					target: includeTarget(internV(strings.TrimSuffix(inc.target.string(), ".proto"), ".structs.usrv.pb.hpp").any())})
			}
		} else if batch.structs {
			side := "server"

			if batch.mode == "client" {
				side = "client"
			}

			for _, file := range []string{"codegen_declarations.hpp", "codegen_definitions.hpp"} {
				includes = append(includes, IncludeDirective{kind: includeQuoted,
					target: includeTarget(internV("userver/grpc-proto-structs/", side, "/impl/", file).any())})
			}

			includes = append(includes, IncludeDirective{kind: includeQuoted,
				target: includeTarget(internV(strings.TrimSuffix(file.path, ".proto"), ".structs.usrv.pb.hpp").any())})
		}

		for _, inc := range file.includes {
			includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(inc)})
		}

		parsed := ParsedIncludeSet{parsedIncludesLocal: na.dirList(includes...)}
		ref := ctx.emit.reserve()

		pending := e.pendingEmit(func() {
			e.emitUserverProtoNode(batch, file, src, outputs, ref, protocRef, protoc, toolRef, tool)
		})

		for _, out := range outputs {
			e.register(GeneratedFileInfo{
				OutputPath: out, PrimaryInput: src, ProducerRef: ref,
				ProducerMainOut: outputs[0],
				GeneratorRefs:   na.refList(protocRef, toolRef), ParsedIncludes: parsed, OnUse: pending,
			})

			if strings.HasSuffix(out.relString(), ".cpp") {
				e.enqueueSrc(SrcMeta{Source: out.any()})
			}
		}
	}
}

func (e *EmitContext) emitUserverProtoNode(batch UserverProtoBatch, file UserverProtoFile, src VFS,
	outputs []VFS, ref, protocRef NodeRef, protoc VFS, toolRef NodeRef, tool VFS) {
	na := e.ctx.na
	args := []ANY{protoc.any()}
	inputs := [][]VFS{na.vfsList(protoc, tool, src)}

	inputs = append(inputs, e.scanner.walkClosure(src, e.d.scanCtx, scanDomainProto).bucketList()...)

	kv := &userverGrpcKV
	cwd := bldRootDirVFS

	if batch.mode == "" {
		kv = &userverStructsKV
		cwd = 0

		args = append(args,
			internV("--plugin=protoc-gen-uproto-structs=", tool.string()).any(),
			internV("--uproto-structs_out=$(B)/", batch.namespace).any())

		if batch.optsFile != "" {
			args = append(args, internV("--uproto-structs_opt={\"opts_file\": \"$(S)/", batch.optsFile, "\"}").any())
			inputs = append(inputs, na.vfsList(source(batch.optsFile)))
		}

		desc := e.resolveModuleSourceVFS(internStr(batch.descriptor).any(), e.d.srcDirs)

		args = append(args, internV("--descriptor_set_in=", desc.string()).any(),
			internStr(strings.TrimPrefix(file.path, batch.namespace+"/")).any())

		inputs = append(inputs, na.vfsList(desc))
	} else {
		args = append(args, internV("-I=./", batch.namespace).any(),
			internV("-I=$(S)/", batch.namespace).any(), argIB2.any(), argIS3.any())

		for _, p := range concat(e.d.protoAddInclGlobal, e.peers.ProtoInclude) {
			args = append(args, internV("-I=", p.string()).any())
		}

		args = append(args, argISContribLibsProtobufSrc.any(),
			internV("--plugin=protoc-gen-ugrpc-", batch.mode, "=", tool.string()).any(),
			internV("--ugrpc-", batch.mode, "_out=$(B)").any())

		if batch.structs {
			args = append(args, internV("--ugrpc-", batch.mode, "_opt={\"structs\": true}").any())
		}

		args = append(args, src.rel().any())
	}

	e.emitReservedNode(Node{
		Platform: e.instance.Platform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Cwd: cwd, Env: envVarsVCS}),
		Env:      envVarsVCS, Inputs: na.inputList(inputs[0], inputs[1:]...),
		Outputs: outputs, KV: kv, ForeignDepRefs: na.refList(protocRef, toolRef),
	}, ref)
}
