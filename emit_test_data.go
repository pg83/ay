package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var dataValidationResourceID = regexp.MustCompile(`^sbr:/?/?([0-9]+)=?.*`)

const maxCorpusResourcesAllowed = 5

func resourceValidationEnabled(env Environment) bool {
	return env.string(envValidateData) != "no" && env.string(envCppAnalysisMode) != "yes"
}

func (ctx *GenCtx) dataValidationSuite(project string, data, env []string, globals []ResourceDecl, uidExt string) *TestSuite {
	var ids []string

	for _, entry := range data {
		if match := dataValidationResourceID.FindStringSubmatch(entry); match != nil {
			ids = append(ids, match[1])
		}
	}

	return ctx.resourceValidationSuite(project, "validate_data_sbr", ids, env, globals, uidExt)
}

func (ctx *GenCtx) sandboxValidationSuite(project string, fromSandboxes []*FromSandboxStmt, env []string, globals []ResourceDecl, uidExt string) *TestSuite {
	ids := make([]string, len(fromSandboxes))

	for i, stmt := range fromSandboxes {
		ids[i] = stmt.ResourceId.string()
	}

	return ctx.resourceValidationSuite(project, "validate_resource", ids, env, globals, uidExt)
}

func (ctx *GenCtx) resourceValidationSuite(project, name string, ids, env []string, globals []ResourceDecl, uidExt string) *TestSuite {
	if len(ids) == 0 {
		return nil
	}

	ids = sortedUnique(ids)

	s := &TestSuite{p: ctx.target, projectPath: project, name: name, testType: name, ciType: "test", size: "small", timeout: 60, env: env, globals: globals, network: "full", setupPythonpathEnv: true, supportsTestParameters: true, clearEnvironment: true, noRetries: true, runInputs: []VFS{source(project)}, inputNames: ids}

	if uidExt != "" {
		s.uidSalt = "sbr_uid_ext=" + uidExt
	}

	s.runCmd = func(work string, _ TestChunk) []ANY {
		checker := "check_resource"

		if ctx.tests.dist {
			checker += " --verbose"
		}

		if token := os.Getenv("YA_TOKEN"); token != "" {
			checker += " --token " + token
		}

		args := []string{testToolHostPath, "run_check", "--source-root", testSourceRoot, "--checker", checker, "--check-name", s.testType, "--trace-path", work + "/ytest.report.trace", "--out-path", work + "/testing_out_stuff", "--log-path", work + "/testing_out_stuff/run_check.log"}

		args = append(args, s.inputNames...)
		args = append(args, "--batch", "--batch-name", path.Base(project))

		return testArgs(args)
	}

	return s
}

func (ctx *GenCtx) fuzzCorpusResources(corpus string) ([]string, int) {
	var data []byte

	if path.IsAbs(corpus) {
		var err error

		if data, err = os.ReadFile(corpus); err != nil {
			return nil, 0
		}
	} else if ctx.fs.isFile(srcRootRel, corpus) {
		data = concatChunks(ctx.fs.read(corpus))
	} else {
		return nil, 0
	}

	if !json.Valid(data) {
		return nil, 0
	}

	dec := json.NewDecoder(bytes.NewReader(data))

	if throw2(dec.Token()) != json.Delim('{') {
		throwFmt("%s: corpus description is not a JSON object", corpus)
	}

	var resources []string

	exceeded := 0

	for dec.More() {
		field := throw2(dec.Token()).(string)

		var ids []json.Number

		throw(dec.Decode(&ids))

		if field == "corpus_parts" && len(ids) > 2*maxCorpusResourcesAllowed {
			exceeded = len(ids)
		}

		for n, id := range ids {
			resources = append(resources, strconv.Itoa(throw2(strconv.Atoi(id.String())))+"="+field+"/"+strconv.Itoa(n))
		}
	}

	return resources, exceeded
}

func (ctx *GenCtx) emitTestData(uri string) NodeRef {
	id, _, _ := strings.Cut(strings.TrimPrefix(uri, "sbr://"), "=")
	dir := "sandbox-storage/" + id
	out := build(dir + "/resource")
	registry := ctx.scannerForPlatform(ctx.target).codegen

	if info := registry.use(out); info != nil {
		return info.ProducerRef
	}

	args := []string{testToolHostPath, "download", "--storage-root", testBuildRoot, "--id", id, "--log-path", build("res" + id + ".log").string(), "--rename-to", "resource", "--resource-file", "$(RESOURCE_ROOT)/sbr/" + id + "/resource"}

	var outputs []VFS

	if ctx.tests.dirOutputsInNodes {
		tar := build(dir + "/dir_res.tar")

		outputs = append(outputs, tar)
		args = append(args, "--dir-output-tared-path", tar.string())
	}

	outputs = append(outputs, build(dir+"/resource_info.json"), out, build("res"+id+".log"))

	if ctx.tests.dist {
		args = append(args, "--for-dist-build", "--log-level", "DEBUG", "--timeout", "900")
	}

	if token := os.Getenv("YA_TOKEN"); token != "" {
		args = append(args, "--token", token)
	}

	requirements := *ctx.tests.contextReqs

	requirements.Network = nwFull

	ref := ctx.emit.emit(&Node{Platform: testNoPlatform, Env: sortedEnvVars(ctx.tests.commonEnvMap()), Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testArgs(args)), Cwd: cwdVFS(testBuildRoot)}), Outputs: ctx.na.vfsList(outputs...), ResourceURIs: []STR{internStr("sbr:" + id)}, KV: &KV{P: pkDL, PC: pcLightCyan, ShowOutBool: true}, Requirements: &requirements})

	registry.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref})

	return ref
}

func (ctx *GenCtx) emitExternalLocalTestData(rel string) NodeRef {
	out := build("external_local/" + rel)
	registry := ctx.scannerForPlatform(ctx.target).codegen

	if info := registry.use(out); info != nil {
		return info.ProducerRef
	}

	src := source(rel)
	args := []string{testYMakePython3, "$(S)/build/scripts/fs_tools.py", "copy", src.string(), out.string()}
	ref := ctx.emit.emit(&Node{DisableCache: true, Platform: testNoPlatform, Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testArgs(args))}), Inputs: NodeInputs{files: ctx.na.inputList(ctx.na.vfsList(src))}, Outputs: ctx.na.vfsList(out), KV: &testNodesKV})

	registry.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref})

	return ref
}
