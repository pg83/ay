package main

import (
	"os"
	"path"
	"regexp"
	"strings"
)

var dataValidationResourceID = regexp.MustCompile(`^sbr:/?/?([0-9]+)=?.*`)

type ResourceValidation struct {
	Data      []string
	Resources []string
}

func resourceValidationEnabled(env Environment) bool {
	return env.string(envValidateData) != "no" && env.string(envCppAnalysisMode) != "yes"
}

func (v *ResourceValidation) addData(data []string, env Environment) {
	if !resourceValidationEnabled(env) {
		return
	}

	for _, entry := range data {
		if match := dataValidationResourceID.FindStringSubmatch(entry); match != nil {
			v.Data = append(v.Data, match[1])
		}
	}
}

func (v *ResourceValidation) addResource(id string, env Environment) {
	if resourceValidationEnabled(env) {
		v.Resources = append(v.Resources, id)
	}
}

func (ctx *GenCtx) resourceValidationSuites(project string, v ResourceValidation, env []string, globals []ResourceDecl) []*TestSuite {
	var suites []*TestSuite

	if len(v.Data) > 0 {
		suites = append(suites, ctx.resourceValidationSuite(project, "validate_data_sbr", v.Data, env, globals))
	}

	if len(v.Resources) > 0 {
		suites = append(suites, ctx.resourceValidationSuite(project, "validate_resource", v.Resources, env, globals))
	}

	return suites
}

func (ctx *GenCtx) resourceValidationSuite(project, name string, ids, env []string, globals []ResourceDecl) *TestSuite {
	ids = sortedUnique(ids)

	s := &TestSuite{p: ctx.target, projectPath: project, name: name, testType: name, ciType: "test", size: "small", timeout: 60, env: env, globals: globals, network: "full", setupPythonpathEnv: true, supportsTestParameters: true, clearEnvironment: true, noRetries: true, runInputs: []VFS{source(project)}, inputNames: ids}

	s.runCmd = func(work string, _ TestChunk) []ANY {
		checker := "check_resource"

		if ctx.tests.dist {
			checker += " --verbose"
		}

		if token := os.Getenv("YA_TOKEN"); token != "" {
			checker += " --token " + token
		}

		args := []string{testToolHostPath, "run_check", "--source-root", testSourceRoot, "--checker", checker, "--check-name", s.testType, "--trace-path", work + "/ytest.report.trace", "--out-path", work + "/testing_out_stuff", "--log-path", work + "/testing_out_stuff/run_check.log"}

		args = append(args, ids...)
		args = append(args, "--batch", "--batch-name", path.Base(project))

		return testArgs(args)
	}

	return s
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

	if ctx.tests.dist {
		args = append(args, "--for-dist-build", "--log-level", "DEBUG", "--timeout", "900")
	}

	requirements := *ctx.tests.contextReqs

	requirements.Network = nwFull

	ref := ctx.emit.emit(&Node{Platform: testNoPlatform, Env: sortedEnvVars(ctx.tests.commonEnvMap()), Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(testArgs(args)), Cwd: cwdVFS(testBuildRoot)}), Outputs: ctx.na.vfsList(build(dir+"/resource_info.json"), build(dir+"/resource"), build("res"+id+".log")), ResourceURIs: []STR{internStr("sbr:" + id)}, KV: &KV{P: pkDL, PC: pcLightCyan, ShowOutBool: true}, Requirements: &requirements})

	registry.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref})

	return ref
}
