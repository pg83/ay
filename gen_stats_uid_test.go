package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type statsUIDRefNode struct {
	HostPlatform bool `json:"host_platform,omitempty"`
	KV           struct {
		P string `json:"p"`
	} `json:"kv"`
	Platform string   `json:"platform"`
	Outputs  []string `json:"outputs"`
	StatsUID string   `json:"stats_uid"`
}

type statsUIDNodeKey struct {
	Outputs      string
	Kind         string
	HostPlatform bool
	Platform     string
}

type indexedStatsUIDNode struct {
	StatsUID string
}

func TestGen_ToolsArchiver_TargetStatsUIDsMatchReference(t *testing.T) {
	const targetDir = "tools/archiver"

	if _, err := os.Stat(filepath.Join(sourceRoot, "sg.json")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s/sg.json", sourceRoot)
		}

		t.Fatalf("stat sg.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := genStatsUIDReferenceSample(sourceRoot, targetDir)
	ref := loadStatsUIDRefNodes(t, filepath.Join(sourceRoot, "sg.json"))

	assertTargetStatsUIDsMatchReference(t, our.Graph, ref, 1, "sg.json")
}

func TestGen_YaBinTargetStatsUIDsMatchReference(t *testing.T) {
	const targetDir = "devtools/ya/bin"

	if _, err := os.Stat(filepath.Join(sourceRoot, "sg3.json")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s/sg3.json", sourceRoot)
		}

		t.Fatalf("stat sg3.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := genStatsUIDReferenceSample(sourceRoot, targetDir)
	ref := loadStatsUIDRefNodes(t, filepath.Join(sourceRoot, "sg3.json"))

	assertTargetStatsUIDsMatchReference(t, our.Graph, ref, 5000, "sg3.json")
}

func genStatsUIDReferenceSample(sourceRoot, targetDir string) *Graph {
	hostFlags := make(map[string]string, len(testToolchainFlags)+4)
	for k, v := range testToolchainFlags {
		hostFlags[k] = v
	}
	hostFlags["GG_BUILD_TYPE"] = "release"
	hostFlags["MUSL"] = "yes"
	hostFlags["PIC"] = "yes"
	hostFlags["SANDBOXING"] = "yes"
	host := NewPlatform(OSLinux, ISAX8664, hostFlags, []string{"tool"}, "", "")
	host.StatsFlags = buildHostStatsFlags(map[string]string{"MUSL": "yes"}, true)

	targetFlags := make(map[string]string, len(testToolchainFlags)+4)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["GG_BUILD_TYPE"] = "debug"
	targetFlags["MUSL"] = "yes"
	targetFlags["PIC"] = "no"
	targetFlags["SANDBOXING"] = "yes"
	target := NewPlatform(OSLinux, ISAAArch64, targetFlags, nil, "", "")
	target.Tags = sandboxingNodeTags(target)
	target.StatsFlags = buildTargetStatsFlags(targetFlags, map[string]string{"MUSL": "yes"})

	return GenWithMode(sourceRoot, targetDir, host, target, defaultScanCtxMode, func(Warn) {})
}

func loadStatsUIDRefNodes(t *testing.T, path string) []statsUIDRefNode {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var graph struct {
		Graph []statsUIDRefNode `json:"graph"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	return graph.Graph
}

func assertTargetStatsUIDsMatchReference(t *testing.T, our []*Node, ref []statsUIDRefNode, minCommon int, refName string) {
	t.Helper()

	ourByKey := indexTargetStatsUIDNodes(t, our)
	refByKey := indexTargetStatsUIDRefNodes(t, ref)

	commonKeys, onlyOur, onlyRef := diffStatsUIDNodeKeys(ourByKey, refByKey)
	if len(onlyOur) > 0 || len(onlyRef) > 0 {
		var problems []string
		if len(onlyOur) > 0 {
			problems = append(problems,
				"extra generated non-host key "+statsUIDDescribeKey(onlyOur[0])+
					" ("+strconv.Itoa(len(onlyOur))+" total)")
		}
		if len(onlyRef) > 0 {
			problems = append(problems,
				"missing generated non-host key "+statsUIDDescribeKey(onlyRef[0])+
					" ("+strconv.Itoa(len(onlyRef))+" total)")
		}
		t.Fatalf("non-host node key drift vs %s: %s", refName, strings.Join(problems, "; "))
	}

	for _, key := range commonKeys {
		ourNode := ourByKey[key]
		refNode := refByKey[key]
		if ourNode.StatsUID != refNode.StatsUID {
			t.Fatalf("stats_uid mismatch for %s:\n got: %s\nwant: %s",
				statsUIDDescribeKey(key), ourNode.StatsUID, refNode.StatsUID)
		}
	}

	if len(commonKeys) < minCommon {
		t.Fatalf("expected at least %d common non-host nodes vs %s, found %d", minCommon, refName, len(commonKeys))
	}
}

func indexTargetStatsUIDNodes(t *testing.T, nodes []*Node) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if nodeHasHostTag(node.Tags) {
			continue
		}

		key := statsUIDNodeKeyFromNode(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate generated non-host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func indexTargetStatsUIDRefNodes(t *testing.T, nodes []statsUIDRefNode) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if node.HostPlatform {
			continue
		}

		key := statsUIDNodeKeyFromRef(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate reference non-host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func diffStatsUIDNodeKeys(our, ref map[statsUIDNodeKey]indexedStatsUIDNode) ([]statsUIDNodeKey, []statsUIDNodeKey, []statsUIDNodeKey) {
	commonKeys := make([]statsUIDNodeKey, 0, len(our))
	onlyOur := make([]statsUIDNodeKey, 0)
	onlyRef := make([]statsUIDNodeKey, 0)

	for key := range our {
		if _, ok := ref[key]; ok {
			commonKeys = append(commonKeys, key)
			continue
		}
		onlyOur = append(onlyOur, key)
	}
	for key := range ref {
		if _, ok := our[key]; ok {
			continue
		}
		onlyRef = append(onlyRef, key)
	}

	sortStatsUIDNodeKeys(commonKeys)
	sortStatsUIDNodeKeys(onlyOur)
	sortStatsUIDNodeKeys(onlyRef)

	return commonKeys, onlyOur, onlyRef
}

func sortStatsUIDNodeKeys(keys []statsUIDNodeKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Outputs != keys[j].Outputs {
			return keys[i].Outputs < keys[j].Outputs
		}
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		if keys[i].HostPlatform != keys[j].HostPlatform {
			return !keys[i].HostPlatform && keys[j].HostPlatform
		}
		return keys[i].Platform < keys[j].Platform
	})
}

func statsUIDDescribeKey(key statsUIDNodeKey) string {
	return "outputs=" + strings.Join(statsUIDOutputsFromKey(key), ",") +
		" kind=" + key.Kind +
		" host_platform=" + boolString(key.HostPlatform) +
		" platform=" + key.Platform
}

func statsUIDOutputsFromKey(key statsUIDNodeKey) []string {
	if key.Outputs == "" {
		return nil
	}
	return strings.Split(key.Outputs, "\x00")
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func statsUIDOutputKey(outputs []string) string {
	normalized := append([]string(nil), outputs...)
	for i, out := range normalized {
		normalized[i] = normalizeStatsUIDOutput(out)
	}
	sort.Strings(normalized)

	return strings.Join(normalized, "\x00")
}

func statsUIDNodeKeyFromNode(node *Node) statsUIDNodeKey {
	kind, _ := node.KV["p"].(string)

	return statsUIDNodeKey{
		Outputs:      statsUIDOutputKey(vfsStrings(node.Outputs)),
		Kind:         kind,
		HostPlatform: nodeHasHostTag(node.Tags),
		Platform:     node.Platform,
	}
}

func statsUIDNodeKeyFromRef(node statsUIDRefNode) statsUIDNodeKey {
	return statsUIDNodeKey{
		Outputs:      statsUIDOutputKey(node.Outputs),
		Kind:         node.KV.P,
		HostPlatform: node.HostPlatform,
		Platform:     node.Platform,
	}
}

func normalizeStatsUIDOutput(out string) string {
	out = strings.ReplaceAll(out, "$(BUILD_ROOT)", "$(B)")
	out = strings.ReplaceAll(out, "$(SOURCE_ROOT)", "$(S)")

	return out
}
