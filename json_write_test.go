package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
)

func encodeWithStdlib(g *Graph) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	throw(enc.Encode(g))

	b := buf.Bytes()

	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}

	return b
}

func encodeWithHandRolled(g *Graph) []byte {
	var buf bytes.Buffer
	writeGraphCompact(&buf, g, false)

	return buf.Bytes()
}

func TestWriteGraphCompact_RoundTrip(t *testing.T) {
	trickyArgs := []string{"a", "b<c>&d", "tab\there", "quote\"x", "back\\slash", "newline\nhere"}

	e := newStreamingEmitter(nil)
	leaf := e.emitNode(Node{Platform: &Platform{},
		Cmds:         []Cmd{},
		Env:          nil,
		Inputs:       InputChunks{ToVFSSlice([]string{})},
		KV:           &KV{Name: "leaf"},
		Outputs:      ToVFSSlice([]string{"leaf.o"}),
		Requirements: Requirements{},
	})
	main := e.emitNode(Node{Platform: &Platform{},
		Cmds:           []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, trickyArgs))}, Cwd: bldRootDirVFS, Env: EnvVars{{Name: internEnv("FOO"), Value: internStr("bar")}}}},
		ForeignDepRefs: []NodeRef{leaf},
		Env:            EnvVars{{Name: internEnv("PATH"), Value: internStr("/usr/bin")}},
		Inputs:         InputChunks{ToVFSSlice([]string{"in1"})},
		KV:             &KV{Name: "main", P: pkCC},
		Outputs:        ToVFSSlice([]string{"main.o"}),
		Requirements:   Requirements{CPU: 1, RAM: 32, Network: nwRestricted},
	})
	e.result(main)
	g := finalize(e)

	out := encodeWithHandRolled(g)

	for _, b := range out {
		if b == '\n' || b == '\t' {
			t.Fatalf("output contains literal %q — not compact: %s", b, out)
		}
	}

	var parsed struct {
		Graph []struct {
			Cmds []struct {
				CmdArgs []string `json:"cmd_args"`
			} `json:"cmds"`
			Deps        []nodeID            `json:"deps"`
			ForeignDeps map[string][]nodeID `json:"foreign_deps"`
			KV          map[string]any      `json:"kv"`
		} `json:"graph"`
		Result []nodeID `json:"result"`
	}

	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("compact output does not parse: %v\n%s", err, out)
	}

	leafUID := nodeID(strconv.FormatUint(uint64(leaf), 10))
	mainUID := nodeID(strconv.FormatUint(uint64(main), 10))

	var mainNode *struct {
		Cmds []struct {
			CmdArgs []string `json:"cmd_args"`
		} `json:"cmds"`
		Deps        []nodeID            `json:"deps"`
		ForeignDeps map[string][]nodeID `json:"foreign_deps"`
		KV          map[string]any      `json:"kv"`
	}

	for i := range parsed.Graph {
		if parsed.Graph[i].KV["name"] == "main" {
			mainNode = &parsed.Graph[i]
		}
	}

	if mainNode == nil {
		t.Fatal("main node missing from written graph")
	}

	if len(mainNode.Deps) != 1 || mainNode.Deps[0] != leafUID {
		t.Errorf("deps = %v, want [%s]", mainNode.Deps, leafUID)
	}

	if got := mainNode.ForeignDeps["tool"]; len(got) != 1 || got[0] != leafUID {
		t.Errorf("foreign_deps[tool] = %v, want [%s]", got, leafUID)
	}

	if len(mainNode.Cmds) != 1 || !equalStrings(mainNode.Cmds[0].CmdArgs, trickyArgs) {
		t.Errorf("cmd_args = %v, want %v", mainNode.Cmds[0].CmdArgs, trickyArgs)
	}

	if len(parsed.Result) != 1 || parsed.Result[0] != mainUID {
		t.Errorf("result = %v, want [%s]", parsed.Result, mainUID)
	}
}

func TestWriteGraphCompact_StringEscaping(t *testing.T) {
	cases := []string{
		"plain",
		"a<b>&c",
		"quote\"x",
		"back\\slash",
		"tab\tnl\ncr\r",
		"\x00\x01\x1f",
		"  ",
		"unicode: ☃ é",
	}

	for _, s := range cases {
		got := string(appendString(nil, s))

		want, err := json.Marshal(s)

		if err != nil {
			t.Fatalf("stdlib marshal %q: %v", s, err)
		}

		var nb bytes.Buffer
		enc := json.NewEncoder(&nb)
		enc.SetEscapeHTML(false)
		throw(enc.Encode(s))
		wantNoHTML := bytes.TrimRight(nb.Bytes(), "\n")

		if got != string(wantNoHTML) {
			t.Errorf("appendString(%q) = %s, want %s (stdlib-with-html=%s)", s, got, wantNoHTML, want)
		}
	}
}

func TestWriteGraphCompact_EmptyGraph(t *testing.T) {
	g := &Graph{
		Graph:  []*Node{},
		Inputs: map[string]interface{}{},
		Result: []NodeRef{},
	}

	want := encodeWithStdlib(g)
	got := encodeWithHandRolled(g)

	if !bytes.Equal(want, got) {
		t.Fatalf("empty graph mismatch:\nwant=%q\ngot =%q", want, got)
	}
}
