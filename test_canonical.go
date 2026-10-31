package main

import (
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

func (ctx *GenCtx) canonicalTestResources(s *TestSuite) []string {
	file := path.Join(s.projectPath, "canondata", s.canonizeSubPath, "result.json")

	if !ctx.fs.isFilePathClean(file) {
		return nil
	}

	parsed := ctx.parsers.parsedJSON(file)

	if parsed.Error != nil {
		fmt.Fprintf(os.Stderr, "malformed canonical data in %s: %s (%s)\n", s.projectPath, parsed.Error, file)

		return nil
	}

	var resources []string

	var visit func(any)

	visit = func(data any) {
		switch value := data.(type) {
		case map[string]any:
			uri, uriPresent := value["uri"]
			_, checksumPresent := value["checksum"]

			if uriPresent && checksumPresent {
				uri, ok := uri.(string)

				if !ok {
					throwFmt("canonical resource URI must be a string in %s", file)
				}

				if resource := canonicalTestResource(uri); resource != "" {
					resources = append(resources, resource)
				}

				return
			}

			for _, child := range value {
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}

	visit(parsed.Value)

	return sortedUnique(resources)
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

	ref := ctx.emit.emit(&Node{Platform: testNoPlatform, Env: sortedEnvVars(ctx.tests.commonEnvMap()), Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testArgs(args)), Cwd: cwdVFS(testBuildRoot).cwd()}), Inputs: ctx.na.inputList(ctx.na.vfsList(buildScriptsFetchFromPy, script)), Outputs: ctx.na.vfsList(output, log), KV: &KV{P: pkDL, PC: pcCyan, ShowOutBool: true}, Requirements: &requirements})

	registry.register(GeneratedFileSpec{OutputPath: output, ProducerRef: ref})

	return ref
}
