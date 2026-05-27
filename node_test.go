package main

import (
	"encoding/json"
	"strings"
	"testing"
)

var expectedKeyOrder = []string{
	"cmds",
	"deps",
	"env",
	"foreign_deps",
	"inputs",
	"kv",
	"outputs",
	"platform",
	"requirements",
	"sandboxing",
	"self_uid",
	"stats_uid",
	"tags",
	"target_properties",
	"uid",
}

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
	"stats_uid",
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
		Inputs:           ToVFSSlice([]string{"in"}),
		KV:               map[string]interface{}{"p": "LD"},
		Outputs:          ToVFSSlice([]string{"out"}),
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

	n := &Node{
		Cmds:             []Cmd{},
		Deps:             []string{},
		Env:              map[string]string{},
		Inputs:           ToVFSSlice([]string{}),
		KV:               map[string]interface{}{},
		Outputs:          ToVFSSlice([]string{}),
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

	s := string(raw)
	for _, frag := range []string{`"cmds":[]`, `"deps":[]`, `"env":{}`, `"inputs":[]`, `"kv":{}`, `"outputs":[]`, `"requirements":{}`, `"stats_uid":""`, `"tags":[]`, `"target_properties":{}`} {
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
		Inputs:           ToVFSSlice([]string{}),
		KV:               map[string]interface{}{},
		Outputs:          ToVFSSlice([]string{}),
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
