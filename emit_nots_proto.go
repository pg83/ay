package main

import (
	"slices"
	"strings"
)

func (e *EmitContext) notsProtoInputs() (args []string, inputs []VFS, refs []NodeRef, leaves []VFS, toolRef NodeRef, globals []ResourceDecl) {
	ctx, d, c := e.ctx, e.d, e.d.nots
	dir := e.instance.Path.relString()
	span := descPeerClosure(e, d.peerdirs, false, false)

	globals = span.globals

	for _, peer := range span.peers {
		refs = append(refs, peer.MergeRef)
		leaves = append(leaves, peer.SelfProtodesc)
	}

	protoInclude := dedup(d.protoAddInclGlobal, span.includes)

	d.scanCtx = newModuleScanContext(ctx, e.instance, d, nil, nil, nil, protoInclude)
	d.srcDirs = append([]VFS{e.instance.Path}, d.srcDirs...)

	protocRef, protocPath := ctx.tool(argContribToolsProtoc)

	inputs = append(inputs, protocPath)
	toolRef = protocRef

	args = append(args, "--tsconfigs")
	args = append(args, c.Configs...)

	if c.AutoProto {
		args = append(args, "--auto-package-name", c.ProtoPackage, "--auto-deps-path", c.ProtoDeps)

		if c.Hermetic {
			inputs = append(inputs, build(dir, "/node_modules.layer"))
		}
	}

	opts := c.ProtoOptions

	if d.grpc && !slices.ContainsFunc(opts, func(s string) bool { return strings.HasPrefix(s, "outputServices") }) {
		opts = append(slices.Clone(opts), "outputServices=grpc-js")
	}

	for _, opt := range opts {
		args = append(args, "--ts-proto-opt", opt)
	}

	args = append(args, "--protoc-bin", protocPath.string(), "--proto-srcs")

	for _, src := range d.srcs {
		file := e.protoSourceRel(src.Source.string()).source()

		args = append(args, file.string())
		inputs = append(inputs, file)

		closure := e.scanner.walkClosure(file, d.scanCtx, scanDomainProto)

		for _, bucket := range closure.bucketList() {
			inputs = append(inputs, bucket...)
		}
	}

	if c.ProtoNonRecursive {
		args = append(args, "--proto-peers")
		args = append(args, anyStrs(c.ProtoPeers)...)
	}

	namespace := protoCPPOutRoot(d)

	args = append(args, "--proto-paths", "./"+namespace, "$(S)/"+namespace, "$(B)", "$(S)")

	for _, inc := range protoInclude {
		args = append(args, inc.string())
	}

	if d.useCommonGoogleAPIs {
		args = append(args, "$(S)/contrib/libs/googleapis-common-protos")
	}

	args = append(args, "$(B)", pbRuntimeBaseVFS.string())

	return
}
