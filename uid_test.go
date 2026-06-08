package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// tuid derives a deterministic UID from a label for test fixtures.
func tuid(label string) UID { return computeUID([]byte(label)) }

func TestComputeUID_LengthAndAlphabet(t *testing.T) {
	got := computeUID([]byte("hello")).String()

	if len(got) != 22 {
		t.Errorf("computeUID length = %d, want 22 (got %q)", len(got), got)
	}

	const allowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	for _, c := range got {
		if !strings.ContainsRune(allowed, c) {
			t.Errorf("computeUID emitted character %q outside base64url alphabet (output %q)", c, got)
		}
	}
}

func TestComputeUID_Stable(t *testing.T) {
	a := computeUID([]byte("the quick brown fox"))
	b := computeUID([]byte("the quick brown fox"))

	if a != b {
		t.Errorf("computeUID not stable: %q vs %q", a, b)
	}
}

func TestComputeUID_DifferentInputsDifferentOutputs(t *testing.T) {
	a := computeUID([]byte("alpha"))
	b := computeUID([]byte("beta"))

	if a == b {
		t.Errorf("computeUID collision on trivial inputs: %q", a)
	}
}

func TestComputeUID_KnownVector(t *testing.T) {

	got := computeUID([]byte("")).String()
	const want = "maoG0wFHmNhgAcMkRo1Jfw"

	if got != want {
		t.Errorf("computeUID(\"\") = %q, want %q", got, want)
	}
}

func TestCanonicalNodeBytes_ZeroesIdentityFields(t *testing.T) {
	n := &Node{
		Cmds: []Cmd{},

		Env:              nil,
		Inputs:           ToVFSSlice([]string{}),
		KV:               KV{},
		Outputs:          ToVFSSlice([]string{}),
		Requirements:     Requirements{},
		Tags:             []string{},
		TargetProperties: TargetProperties{},
		UID:              tuid("AAAAA"),
		SelfUID:          tuid("BBBBB"),
	}
	canon := canonicalNodeBytes(n)
	s := string(canon)

	for _, banned := range []string{tuid("AAAAA").String(), tuid("BBBBB").String(), "should-not-appear-CCCCC"} {
		if strings.Contains(s, banned) {
			t.Errorf("canonicalNodeBytes leaked identity field value %q: %s", banned, s)
		}
	}

	if n.UID == (UID{}) || n.SelfUID == (UID{}) {
		t.Errorf("canonicalNodeBytes mutated the original node: %+v", n)
	}
}

func TestCanonicalNodeBytes_VsDefaultJSONMarshal(t *testing.T) {

	n := &Node{
		Cmds: []Cmd{{CmdArgs: appendInternStrs(nil, []string{"sh", "-c", "echo <a> & echo b"}), Env: nil}},
		Env:  nil, Inputs: ToVFSSlice([]string{}),
		KV: KV{P: pkCC, Name: "a<b>c"}, Outputs: ToVFSSlice([]string{}),
		Requirements: Requirements{}, Tags: []string{},
		TargetProperties: TargetProperties{},
	}
	canon := canonicalNodeBytes(n)

	defaultMarshalled := Throw2(json.Marshal(n))

	if bytes.Equal(canon, defaultMarshalled) {
		t.Fatalf("canonicalNodeBytes and json.Marshal produced identical bytes; "+
			"either default escaping changed or canonicalNodeBytes lost its "+
			"SetEscapeHTML(false). canon=%s default=%s", canon, defaultMarshalled)
	}

	if !bytes.Contains(canon, []byte("<b>")) || !bytes.Contains(canon, []byte("a<b>c")) {
		t.Errorf("canonicalNodeBytes should preserve literal '<' chars; got: %s", canon)
	}

	const escapedLT = "\\u003c"

	if !bytes.Contains(defaultMarshalled, []byte(escapedLT)) {
		t.Errorf("default json.Marshal should escape '<' as %s; got: %s", escapedLT, defaultMarshalled)
	}

	if bytes.Contains(canon, []byte(escapedLT)) {
		t.Errorf("canonicalNodeBytes must NOT contain %s (escaping disabled); got: %s", escapedLT, canon)
	}
}
