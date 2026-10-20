package main

import "strings"

func (ctx *GenCtx) emitTestData(uri string) NodeRef {
	id, _, _ := strings.Cut(strings.TrimPrefix(uri, "sbr://"), "=")
	dir := "sandbox-storage/" + id
	out := build(dir + "/resource")
	registry := ctx.scannerForPlatform(ctx.target).codegen

	if info := registry.use(out); info != nil {
		return info.ProducerRef
	}

	args := []string{testToolHostPath, "download", "--storage-root", testBuildRoot, "--id", id, "--log-path", build("res" + id + ".log").string(), "--rename-to", "resource", "--resource-file", "$(RESOURCE_ROOT)/sbr/" + id + "/resource"}

	if ctx.tests.dist {
		args = append(args, "--for-dist-build", "--log-level", "DEBUG", "--timeout", "900")
	}

	requirements := *ctx.tests.contextReqs

	requirements.Network = nwFull

	ref := ctx.emit.emit(&Node{Platform: testNoPlatform, Env: sortedEnvVars(ctx.tests.commonEnvMap()), Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testArgs(args)), Cwd: cwdVFS(testBuildRoot)}), Outputs: ctx.na.vfsList(build(dir+"/resource_info.json"), build(dir+"/resource"), build("res"+id+".log")), ResourceURIs: []STR{internStr("sbr:" + id)}, KV: &KV{P: pkDL, PC: pcLightCyan, ShowOutBool: true}, Requirements: &requirements})

	registry.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})

	return ref
}
