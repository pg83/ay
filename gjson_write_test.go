package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func encodeWithStdlib(g *Graph) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	Throw(enc.Encode(g))

	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}

	return b
}

func encodeWithHandRolled(g *Graph) []byte {
	var buf bytes.Buffer
	writeGraphIndented(&buf, g)

	return buf.Bytes()
}

func TestWriteGraphIndented_ByteExact(t *testing.T) {
	g := &Graph{
		Conf: map[string]interface{}{
			"resources": []graphConfResource{
				{
					Pattern: "CLANG",
					Resources: []graphConfResourceURI{
						{Platform: "linux", Resource: "sbr:1"},
						{Platform: "darwin", Resource: "sbr:2"},
					},
				},
				{
					Name:     "vcs",
					Pattern:  "VCS",
					Resource: "base64:vcs.json:e30=",
				},
			},
		},
		Graph: []*Node{
			{
				Cmds: []Cmd{
					{
						CmdArgs: []string{"a", "b<c>&d", "tab\there", "quote\"x", "back\\slash", "newline\nhere", "u2028   done"},
						Cwd:     "$(B)",
						Env:     map[string]string{"FOO": "bar", "BAZ": "qux"},
					},
					{
						CmdArgs: []string{},
						Env:     map[string]string{},
					},
				},
				Deps:        []string{"depUid1", "depUid2"},
				Env:         map[string]string{"PATH": "/usr/bin"},
				ForeignDeps: map[string][]string{"clone": {"u1", "u2"}, "tool": {"u3"}},
				Inputs:      ToVFSSlice([]string{"in1", "in2"}),
				KV:          map[string]interface{}{"key1": "val1", "key2": "val2"},
				Outputs:     ToVFSSlice([]string{"out1"}),
				Platform:    "default-linux-x86_64",
				Requirements: map[string]interface{}{
					"cpu":     float64(1),
					"network": "restricted",
					"ram":     float64(32),
					"frac":    float64(1.5),
					"flag":    true,
				},
				Sandboxing:       true,
				SelfUID:          "selfUidXXXXXXXXXXXXXX1",
				StatsUID:         "statsUidXXXXXXXXXXXXXXXXXX1",
				Tags:             []string{"tag1", "tag2"},
				TargetProperties: map[string]string{"module_dir": "x/y", "module_lang": "cpp"},
				UID:              "uidXXXXXXXXXXXXXXXXXX1",
			},
			{
				Cmds:             []Cmd{},
				Deps:             []string{},
				Env:              map[string]string{},
				Inputs:           ToVFSSlice([]string{}),
				KV:               map[string]interface{}{},
				Outputs:          ToVFSSlice([]string{}),
				Platform:         "platform-with-control-and--and-\b-and-\f",
				Requirements:     map[string]interface{}{},
				Sandboxing:       true,
				SelfUID:          "selfUidXXXXXXXXXXXXXX2",
				StatsUID:         "statsUidXXXXXXXXXXXXXXXXXX2",
				Tags:             []string{},
				TargetProperties: map[string]string{},
				UID:              "uidXXXXXXXXXXXXXXXXXX2",
			},
		},
		Inputs: map[string]interface{}{},
		Result: []string{"uidXXXXXXXXXXXXXXXXXX1", "uidXXXXXXXXXXXXXXXXXX2"},
	}

	want := encodeWithStdlib(g)
	got := encodeWithHandRolled(g)

	if !bytes.Equal(want, got) {

		minLen := len(want)
		if len(got) < minLen {
			minLen = len(got)
		}

		div := minLen
		for i := 0; i < minLen; i++ {
			if want[i] != got[i] {
				div = i

				break
			}
		}

		ctx := 80
		from := div - ctx
		if from < 0 {
			from = 0
		}

		toW := div + ctx
		if toW > len(want) {
			toW = len(want)
		}

		toG := div + ctx
		if toG > len(got) {
			toG = len(got)
		}

		t.Fatalf("byte mismatch at offset %d (want len=%d, got len=%d)\nwant[%d:%d]: %q\ngot [%d:%d]: %q",
			div, len(want), len(got), from, toW, want[from:toW], from, toG, got[from:toG])
	}
}

func TestWriteGraphIndented_EmptyGraph(t *testing.T) {
	g := &Graph{
		Conf:   map[string]interface{}{},
		Graph:  []*Node{},
		Inputs: map[string]interface{}{},
		Result: []string{},
	}

	want := encodeWithStdlib(g)
	got := encodeWithHandRolled(g)

	if !bytes.Equal(want, got) {
		t.Fatalf("empty graph mismatch:\nwant=%q\ngot =%q", want, got)
	}
}
