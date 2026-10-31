package main

import (
	"strconv"
	"strings"
)

func (ctx *GenCtx) emitDockerImage(image string) NodeRef {
	var link, tag string

	if strings.HasPrefix(image, "docker://") {
		index := strings.LastIndexByte(image, '=')

		if index < 0 {
			throwFmt("DOCKER_IMAGE expects a link and tag separated by '='")
		}

		link, tag = image[:index], image[index+1:]
	} else {
		tag, link, _ = strings.Cut(image, "=")
	}

	address, digest, ok := strings.Cut(strings.TrimPrefix(link, "docker://"), "@sha")
	_, digest, valid := strings.Cut(digest, ":")

	if !ok || !valid || !strings.HasPrefix(link, "docker://") {
		throwFmt("DOCKER_IMAGE: invalid Docker digest URL %s", link)
	}

	host, _, _ := strings.Cut(address, "/")
	dir := "images/" + strconv.FormatUint(cityHash64(strBytes(tag)), 10)
	out := build(dir, "/image.tar")
	info := build(dir, "/image_info.json")
	registry := ctx.scannerForPlatform(ctx.target).codegen

	if previous := registry.use(out); previous != nil {
		return previous.ProducerRef
	}

	args := []string{
		testYMakePython3, "$(S)/build/scripts/fetch_from_docker_repo.py",
		"--link", link, "--tag", tag, "--output-image-path", out.string(),
		"--output-info-path", info.string(), "--preloaded-path",
		"$(RESOURCE_ROOT)/docker/" + host + "-" + digest[:min(12, len(digest))] + "/resource",
	}

	if ctx.target.Flags[envUSE_ARCADIA_PYTHON] == strNo {
		args[0] = "python3"
	}

	ref := ctx.emit.emit(&Node{Platform: testNoPlatform, Env: sortedEnvVars(ctx.tests.commonEnvMap()),
		Cmds:    ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testArgs(args)), Cwd: bldRootDirVFS}),
		Outputs: ctx.na.vfsList(out, info), Extra: &NodeExtra{ResourceURIs: []STR{internStr(link)}},
		KV: &KV{P: pkIM, PC: pcLightCyan, ShowOutBool: true}, Requirements: ctx.tests.contextReqs,
	})

	registry.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref})
	registry.register(GeneratedFileSpec{OutputPath: info, ProducerRef: ref})

	return ref
}
