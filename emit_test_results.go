package main

import (
	"path"
	"strconv"
	"strings"
)

func testResultsDir(info TestSuiteInfo) string {
	dir := testRunDir(info)

	if info.SplitFile != "" {
		dir = path.Join(dir, strings.TrimSuffix(info.SplitFile, path.Ext(info.SplitFile)))
	}

	if info.SplitFactor > 1 {
		dir = path.Join(dir, "chunk"+strconv.Itoa(info.SplitIndex))
	}

	return dir
}

func buildTestAccumulator(na *NodeArenas, p *Platform, info TestSuiteInfo, nodes []*Node, refs []NodeRef) *Node {
	dir := testRunDir(info)
	outputs := testOutputsAt(dir)[:3]
	log := build(path.Join(dir, "results_accumulator.log"))
	args := []string{testToolHostPath, "results_accumulator", "--accumulator-path", build(dir).string(), "--source-root", testSourceRoot, "--log-path", log.string(), "--concatenate-binaries", "report.exec", "--concatenate-binaries", "operation.log"}

	if info.Options.Dist {
		args = append(args, "--truncate", "--log-level", "DEBUG")
	}

	args = append(args, "--gdb-path", testGDBPath, "--tests-limit-in-suite", "100000", "--keep-paths", "--ya-start-command-file")

	var inputs []VFS

	for _, node := range nodes {
		inputs = append(inputs, node.Outputs...)
		args = append(args, "--output", build(path.Dir(node.Outputs[0].relString())).string())
		outputs = append(outputs, node.Outputs[3])
	}

	args = append(args, "--ya-end-command-file", "--skip-file", "testing_out_stuff.tar.zstd", "--skip-file", "yt_run_test.tar")
	outputs = append(outputs, log)

	return &Node{Env: info.Options.CommonEnv, Platform: p, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(testCommandArgs(args)), Cwd: cwdVFS(testBuildRoot)}), Inputs: na.inputList(inputs), Outputs: outputs, DepRefs: refs, Requirements: &testContextRequirements, KV: &KV{P: pkTA, PC: pcLightCyan, ShowOutBool: true}}
}

func buildTestMerger(na *NodeArenas, p *Platform, info TestSuiteInfo, nodes []*Node, refs []NodeRef) *Node {
	dir := testRunDir(info)
	log := build(path.Join(dir, "results_merge.log"))
	outputs := []VFS{log}

	var inputs []VFS

	for _, node := range nodes {
		inputs = append(inputs, node.Outputs...)

		for _, out := range node.Outputs {
			base := path.Base(out.relString())

			if base == "testing_out_stuff.tar.zstd" || base == "yt_run_test.tar" {
				outputs = append(outputs, out)
			} else {
				outputs = append(outputs, build(path.Join(dir, base)))
			}
		}
	}

	args := []string{testToolHostPath, "results_merger", "--project-path", info.ProjectPath, "--suite-name", info.Name, "--source-root", testSourceRoot, "--log-path", log.string(), "--remove-tos", "--target-platform-descriptor", info.Options.descriptor(p)}

	for i := range nodes {
		run := info

		run.Retry = i + 1
		args = append(args, "--output", build(testRunDir(run)).string())
	}

	return &Node{Platform: p, Env: info.Options.CommonEnv, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(testCommandArgs(args)), Cwd: cwdVFS(testBuildRoot)}), Inputs: na.inputList(dedup(inputs)), Outputs: dedup(outputs), DepRefs: refs, KV: &KV{P: pkTR, PC: pcLightCyan, ShowOutBool: true}}
}
