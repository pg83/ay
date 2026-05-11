package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// node_test.go — invariants on the on-disk JSON shape of Node.
//
// These tests pin down the contract that PR-04 (the JSON serializer) and
// PR-12 (the comparator) will rely on:
//   - Field order in the marshalled object matches the alphabetical key
//     order observed in /home/pg/monorepo/yatool_orig/sg.json.
//   - host_platform and foreign_deps are omitted when zero (per D5).
//   - All other fields are present even when empty (no omitempty), so
//     empty arrays/maps render as []/{}.

// expectedKeyOrder is the sequence of keys observed in the reference
// sg.json (alphabetical, including the two omitempty fields). Tests that
// supply non-zero values for the omitempty fields expect this full order.
// PR-L4-C/01: sandboxing added between requirements and self_uid.
// PR-L4-C/04: stats_uid removed (json:"-").
var expectedKeyOrder = []string{
	"cmds",
	"deps",
	"env",
	"foreign_deps",
	"host_platform",
	"inputs",
	"kv",
	"outputs",
	"platform",
	"requirements",
	"sandboxing",
	"self_uid",
	"tags",
	"target_properties",
	"uid",
}

// expectedKeyOrderMinimal is what we expect when host_platform and
// foreign_deps are zero (i.e. the typical "small" node shape).
var expectedKeyOrderMinimal = []string{
	"cmds",
	"deps",
	"env",
	"inputs",
	"kv",
	"outputs",
	"platform",
	"requirements",
	"sandboxing",
	"self_uid",
	"tags",
	"target_properties",
	"uid",
}

func extractKeyOrder(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("expected object, got %v", tok)
	}
	var keys []string
	for {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if d, ok := tok.(json.Delim); ok && d == '}' {
			break
		}
		k, ok := tok.(string)
		if !ok {
			t.Fatalf("expected key string, got %v", tok)
		}
		keys = append(keys, k)
		// Skip the value (which may itself be an object/array).
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("skip value for %s: %v", k, err)
		}
	}
	return keys
}

func TestNodeJSONKeyOrder_AllFieldsPresent(t *testing.T) {
	n := &Node{
		Cmds:             []Cmd{{CmdArgs: []string{"echo"}, Env: map[string]string{}}},
		Deps:             []string{"dep1"},
		Env:              map[string]string{"FOO": "bar"},
		ForeignDeps:      map[string][]string{"tool": {"tooluid"}},
		HostPlatform:     true,
		Inputs:           []string{"in"},
		KV:               map[string]string{"p": "LD"},
		Outputs:          []string{"out"},
		Platform:         "default-linux-aarch64",
		Requirements:     map[string]interface{}{"cpu": 1, "ram": 32},
		SelfUID:          "selfuid",
		StatsUID:         "statsuid",
		Tags:             []string{},
		TargetProperties: map[string]string{"module_lang": "cpp"},
		UID:              "uid",
	}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	keys := extractKeyOrder(t, raw)
	if len(keys) != len(expectedKeyOrder) {
		t.Fatalf("key count: got %d %v, want %d %v", len(keys), keys, len(expectedKeyOrder), expectedKeyOrder)
	}
	for i, k := range expectedKeyOrder {
		if keys[i] != k {
			t.Errorf("key[%d] = %q, want %q (full order: %v)", i, keys[i], k, keys)
		}
	}
}

func TestNodeJSONKeyOrder_OmitemptyFieldsZero(t *testing.T) {
	// HostPlatform=false (zero) and ForeignDeps=nil (zero) must drop
	// the corresponding keys. Everything else must remain present even
	// with empty values.
	n := &Node{
		Cmds:             []Cmd{},
		Deps:             []string{},
		Env:              map[string]string{},
		Inputs:           []string{},
		KV:               map[string]string{},
		Outputs:          []string{},
		Platform:         "",
		Requirements:     map[string]interface{}{},
		SelfUID:          "",
		StatsUID:         "",
		Tags:             []string{},
		TargetProperties: map[string]string{},
		UID:              "",
	}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	keys := extractKeyOrder(t, raw)
	if len(keys) != len(expectedKeyOrderMinimal) {
		t.Fatalf("key count: got %d %v, want %d %v", len(keys), keys, len(expectedKeyOrderMinimal), expectedKeyOrderMinimal)
	}
	for i, k := range expectedKeyOrderMinimal {
		if keys[i] != k {
			t.Errorf("key[%d] = %q, want %q (full order: %v)", i, keys[i], k, keys)
		}
	}
	// Spot-check that empty arrays/maps render as []/{}.
	s := string(raw)
	for _, frag := range []string{`"cmds":[]`, `"deps":[]`, `"env":{}`, `"inputs":[]`, `"kv":{}`, `"outputs":[]`, `"requirements":{}`, `"tags":[]`, `"target_properties":{}`} {
		if !strings.Contains(s, frag) {
			t.Errorf("expected output to contain %q, got: %s", frag, s)
		}
	}
}

func TestNodeJSON_DoesNotSerializeInternalRefs(t *testing.T) {
	n := &Node{
		Cmds:             []Cmd{},
		Deps:             []string{},
		Env:              map[string]string{},
		Inputs:           []string{},
		KV:               map[string]string{},
		Outputs:          []string{},
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
		DepRefs:          []NodeRef{{id: 7}},
		ForeignDepRefs:   map[string][]NodeRef{"tool": {{id: 9}}},
	}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, banned := range []string{"DepRefs", "dep_refs", "ForeignDepRefs", "foreign_dep_refs"} {
		if strings.Contains(s, banned) {
			t.Errorf("internal field %q leaked into JSON: %s", banned, s)
		}
	}
}

func TestCmdJSONKeyOrder(t *testing.T) {
	c := Cmd{CmdArgs: []string{"echo", "hi"}, Env: map[string]string{"K": "V"}}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	keys := extractKeyOrder(t, raw)
	want := []string{"cmd_args", "env"}
	if len(keys) != len(want) {
		t.Fatalf("got keys %v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Errorf("key[%d] = %q, want %q", i, keys[i], k)
		}
	}
}
