package main

import "strings"

func (ctx *GenCtx) emitTestData(uri string) NodeRef {
	id, _, _ := strings.Cut(strings.TrimPrefix(uri, "sbr://"), "=")
	dir := "sandbox-storage/" + id
	args := []string{testToolHostPath, "download", "--storage-root", testBuildRoot, "--id", id, "--log-path", build("res" + id + ".log").string(), "--rename-to", "resource", "--resource-file", "$(RESOURCE_ROOT)/sbr/" + id + "/resource"}

	if ctx.testOptions.Dist {
		args = append(args, "--for-dist-build", "--log-level", "DEBUG", "--timeout", "900")
	}

	return ctx.emit.emit(&Node{Platform: ctx.target, Env: ctx.testOptions.CommonEnv, Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testCommandArgs(args)), Cwd: cwdVFS(testBuildRoot)}), Outputs: ctx.na.vfsList(build(dir+"/resource_info.json"), build(dir+"/resource"), build("res"+id+".log")), ResourceURIs: []STR{internStr("sbr:" + id)}, KV: &KV{P: pkDL, PC: pcLightCyan, ShowOutBool: true}, Requirements: &Requirements{Network: nwFull}})
}
