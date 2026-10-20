package main

import (
	"path"
	"strconv"
	"strings"
)

func testResultsDir(info TestSuiteInfo) string {
	dir := path.Join(info.ProjectPath, "test-results", info.Name)

	if info.SplitFile != "" {
		dir = path.Join(dir, strings.TrimSuffix(info.SplitFile, path.Ext(info.SplitFile)))
	}

	if info.SplitFactor > 1 {
		dir = path.Join(dir, "chunk"+strconv.Itoa(info.SplitIndex))
	}

	return dir
}

func buildTestAccumulator(na *NodeArenas, p *Platform, info TestSuiteInfo, nodes []*Node, refs []NodeRef) *Node {
	dir := path.Join(info.ProjectPath, "test-results", info.Name)
	outputs := testOutputsAt(dir)[:3]
	log := build(path.Join(dir, "results_accumulator.log"))
	args := []string{testToolHostPath, "results_accumulator", "--accumulator-path", build(dir).string(), "--source-root", testSourceRoot, "--log-path", log.string(), "--concatenate-binaries", "report.exec", "--concatenate-binaries", "operation.log", "--gdb-path", testGDBPath, "--tests-limit-in-suite", "100000", "--keep-paths", "--ya-start-command-file"}

	var inputs []VFS

	for _, node := range nodes {
		inputs = append(inputs, node.Outputs...)
		args = append(args, "--output", build(path.Dir(node.Outputs[0].relString())).string())
		outputs = append(outputs, node.Outputs[3])
	}

	args = append(args, "--ya-end-command-file", "--skip-file", "testing_out_stuff.tar.zstd", "--skip-file", "yt_run_test.tar")
	outputs = append(outputs, log)

	return &Node{Platform: p, Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(testCommandArgs(args)), Cwd: cwdVFS(testBuildRoot)}), Inputs: na.inputList(inputs), Outputs: outputs, DepRefs: refs, Requirements: &testContextRequirements, KV: &KV{P: pkTA, PC: pcLightCyan, ShowOutBool: true}}
}
