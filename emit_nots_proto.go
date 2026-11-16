package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"path"
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

	protocRef, protocPath := e.tool(argContribToolsProtoc)

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

	if d.grpc {
		if msg := e.notsGrpcError(dir, opts); msg != "" {
			ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dir), Error: msg})
		} else if !slices.ContainsFunc(opts, notsOutputServices) {
			opts = append(slices.Clone(opts), "outputServices=grpc-js")
		}
	}

	for _, opt := range opts {
		args = append(args, "--ts-proto-opt", opt)
	}

	args = append(args, "--protoc-bin", protocPath.string(), "--proto-srcs")

	for _, src := range d.srcs {
		file := e.protoSourceRel(src.Source).source()

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

func notsOutputServices(opt string) bool {
	return strings.HasPrefix(opt, "outputServices")
}

func (e *EmitContext) notsGrpcError(dir string, opts []string) string {
	if !e.d.nots.AutoProto {
		if slices.ContainsFunc(opts, notsOutputServices) {
			return ""
		}

		for _, deps := range notsDependencies(e.ctx.parsers.notsJSON(dir + "/package.json")) {
			if _, ok := deps["@grpc/grpc-js"]; ok {
				return ""
			}
		}

		return "ya.make includes macro " + tsCyan + "GRPC()" + tsReset + " but " + tsRed + "package.json is missing @grpc/grpc-js" + tsReset + " dependency.\n" +
			"Call " + tsGreen + "ya tool nots add @grpc/grpc-js" + tsReset + " to fix the issue.\n" +
			"Docs: https://docs.yandex-team.ru/frontend-in-arcadia/references/TS_PROTO#grpc_custom_package"
	}

	supported := []string{"grpc-js", "generic-definitions", "default", "false", "none"}

	var wrong []string

	for _, opt := range opts {
		if !notsOutputServices(opt) {
			continue
		}

		_, value, _ := strings.Cut(opt, "=")

		if !slices.Contains(supported, value) && !slices.Contains(wrong, value) {
			wrong = append(wrong, value)
		}
	}

	if len(wrong) == 0 {
		return ""
	}

	return "ya.make includes macro " + tsCyan + "GRPC()" + tsReset + " but " + tsCyan + "TS_PROTO_OPT()" + tsReset + " has outputServices option with unsupported value(s): " + tsRed + strings.Join(wrong, ", ") + tsReset + ".\n" +
		"Remove outputServices from " + tsCyan + "TS_PROTO_OPT()" + tsReset + " to use default " + tsGreen + "grpc-js" + tsReset + ".\n" +
		"Or set one of supported values: " + tsGreen + strings.Join(supported, ", ") + tsReset + ".\n" +
		"Docs: https://docs.yandex-team.ru/frontend-in-arcadia/references/TS_PROTO#grpc"
}

func (e *EmitContext) notsPrebuilderError(dir string, pj map[string]any) string {
	pm := e.ctx.parsers
	lock := pm.notsLockfile(dir + "/pnpm-lock.yaml")
	head := "Project is configured to use @yatool/prebuilder. \n"

	if strings.HasPrefix(pm.notsVersion("@yatool/prebuilder", ""), "0.") {
		if len(lock.NoHasAddons) == 0 {
			return ""
		}

		return head + "Some packages in the pnpm-lock.yaml are misconfigured.\n" +
			"Run " + tsGreen + "`ya tool nots update-lockfile`" + tsReset + " to fix lockfile.\n" +
			"All packages with `requiresBuild:true` have to be marked with `hasAddons:true/false`.\n" +
			"Misconfigured keys: \n  - " + strings.Join(lock.NoHasAddons, "\n  - ")
	}

	overrides := notsObject(notsObject(pj["pnpm"])["overrides"])

	if config := notsString(notsObject(pj["nots"])["commonConfigPath"]); config != "" {
		overrides = maps.Clone(overrides)

		for key := range notsObject(pm.notsYAML(path.Join(dir, config))["overrides"]) {
			overrides[key] = true
		}
	}

	flags := notsObject(notsObject(pj["@yatool/prebuilder"])["usePrebuild"])

	var messages []string

	for _, key := range lock.RequiresBuild {
		if _, ok := flags[key]; !ok && !strings.HasPrefix(key, "@yandex-prebuild/") {
			if len(messages) == 0 {
				messages = append(messages, "These packages possibly have addons but are not checked yet:")
			}

			messages = append(messages, "  - "+key)
		}
	}

	var missing []string

	for _, key := range notsJSONKeys(concatChunks(pm.fs.readPath(internStr(dir+"/package.json"))), "@yatool/prebuilder", "usePrebuild") {
		if _, ok := overrides[key]; !ok && flags[key] == true {
			missing = append(missing, "  - "+key)
		}
	}

	if len(missing) != 0 {
		messages = append(append(messages, "These packages have addons but overrides are not set:"), missing...)
	}

	if len(messages) == 0 {
		return ""
	}

	return head + "Some packages are misconfigured.\n" +
		"Run " + tsGreen + "`ya tool nots update-lockfile`" + tsReset + " to fix pnpm-lock.yaml and package.json.\n" +
		"Validation details: \n" + strings.Join(messages, "\n")
}

func notsJSONKeys(data []byte, keys ...string) []string {
	dec := json.NewDecoder(bytes.NewReader(data))

	var walk func(depth int) []string

	walk = func(depth int) []string {
		if t, err := dec.Token(); err != nil || t != json.Delim('{') {
			return nil
		}

		var out []string

		for dec.More() {
			t, err := dec.Token()

			if err != nil {
				return nil
			}

			key, _ := t.(string)

			switch {
			case depth == len(keys):
				out = append(out, key)

				var skip json.RawMessage

				if dec.Decode(&skip) != nil {
					return nil
				}
			case key == keys[depth]:
				return walk(depth + 1)
			default:
				var skip json.RawMessage

				if dec.Decode(&skip) != nil {
					return nil
				}
			}
		}

		return out
	}

	return walk(0)
}
