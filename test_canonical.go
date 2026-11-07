package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

func (ctx *GenCtx) canonicalTestResources(s *TestSuite) []string {
	file := path.Join(s.projectPath.string(), "canondata", s.canonizeSubPath, "result.json")

	if !ctx.fs.isFilePathClean(file) {
		return nil
	}

	key := internStr(file)

	if resources, ok := ctx.canonicalResources[key]; ok {
		return resources
	}

	data := concatChunks(ctx.fs.readPath(key))

	if !json.Valid(data) {
		if parsed := ctx.parsers.parsedJSON(file); parsed.Error != nil {
			fmt.Fprintf(os.Stderr, "malformed canonical data in %s: %s (%s)\n", s.projectPath.string(), parsed.Error, file)

			return nil
		}
	}

	scan := canonicalResourceScan{data: data}

	if _, bad := scan.value(0); bad {
		throwFmt("canonical resource URI must be a string in %s", file)
	}

	resources := sortedUnique(scan.resources)

	ctx.canonicalResources[key] = resources

	return resources
}

func canonicalTestResource(uri string) string {
	for _, prefix := range []string{"https://storage.yandex-team.ru/get-devtools/", "https://{canondata_backend}/"} {
		if resource, ok := strings.CutPrefix(uri, prefix); ok {
			resource, _, _ = strings.Cut(resource, "#")

			return "mds:" + resource
		}
	}

	if match := dataValidationResourceID.FindStringSubmatch(uri); match != nil {
		return "sbr://" + match[1] + "=canondata_storage/" + match[1]
	}

	return ""
}

func (ctx *GenCtx) emitCanonicalTestData(resource string) NodeRef {
	output := build("canondata_storage/", resource)
	registry := ctx.scannerForPlatform(ctx.target).codegen

	if info := registry.use(output); info != nil {
		return info.ProducerRef
	}

	log := build(output.relString(), ".log")
	script := buildScriptsFetchFromMdsPy
	python := testYMakePython3

	if slices.Contains(ctx.tests.flags, "USE_ARCADIA_PYTHON=no") {
		python = "python3"
	}

	args := []string{python, script.string(), "--key", resource, "--entrypoint", "storage.yandex-team.ru/get-devtools", "--rename-to", output.string(), "--log-path", log.string(), "--scheme", "https"}
	requirements := *ctx.tests.contextReqs

	requirements.Network = nwFull

	ref := ctx.emit.emit(&Node{Platform: testNoPlatform, Env: ctx.tests.commonEnvVars(), Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testArgs(args)), Cwd: cwdVFS(testBuildRoot).cwd()}), Inputs: ctx.na.inputList(ctx.na.vfsList(buildScriptsFetchFromPy, script)), Outputs: ctx.na.vfsList(output, log), KV: &KV{P: pkDL, PC: pcCyan, ShowOutBool: true}, Requirements: &requirements})

	registry.register(GeneratedFileSpec{OutputPath: output, ProducerRef: ref})

	return ref
}
