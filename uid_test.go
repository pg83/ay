package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// uid_test.go — properties of the content-derived UID hash.

func TestComputeUID_LengthAndAlphabet(t *testing.T) {
	got := computeUID([]byte("hello"))
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
	// SHA1("") = da39a3ee5e6b4b0d3255bfef95601890afd80709
	// base64url of those 20 bytes = "2jmj7l5rSw0yVb_vlWAYkK_YBwk", first 22 = "2jmj7l5rSw0yVb_vlWAYkK".
	got := computeUID([]byte(""))
	const want = "2jmj7l5rSw0yVb_vlWAYkK"
	if got != want {
		t.Errorf("computeUID(\"\") = %q, want %q", got, want)
	}
}

func TestCanonicalNodeBytes_ZeroesIdentityFields(t *testing.T) {
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
		UID:              "should-not-appear-AAAAA",
		SelfUID:          "should-not-appear-BBBBB",
		StatsUID:         "should-not-appear-CCCCC",
	}
	canon, err := canonicalNodeBytes(n)
	if err != nil {
		t.Fatalf("canonicalNodeBytes: %v", err)
	}
	s := string(canon)
	for _, banned := range []string{"should-not-appear-AAAAA", "should-not-appear-BBBBB", "should-not-appear-CCCCC"} {
		if strings.Contains(s, banned) {
			t.Errorf("canonicalNodeBytes leaked identity field value %q: %s", banned, s)
		}
	}
	// And the original node must still have its identity values intact —
	// canonicalNodeBytes operates on a copy.
	if n.UID == "" || n.SelfUID == "" || n.StatsUID == "" {
		t.Errorf("canonicalNodeBytes mutated the original node: %+v", n)
	}
}

// TestCanonicalNodeBytes_VsDefaultJSONMarshal pins the cross-cutting
// architectural decision (tasks.md D16) at code level: any future PR that
// serializes nodes for hashing or comparison via the default
// `json.Marshal` (HTML-escape on) will produce different bytes than
// canonicalNodeBytes (HTML-escape off), and the comparator/hash will
// disagree with rule-author intent. This test exists to catch that
// regression early.
func TestCanonicalNodeBytes_VsDefaultJSONMarshal(t *testing.T) {
	// This test exists to catch any future PR that uses default
	// json.Marshal for serializing nodes — comparator depends on
	// canonicalNodeBytes' escape settings.
	n := &Node{
		Cmds: []Cmd{{CmdArgs: []string{"sh", "-c", "echo <a> & echo b"}, Env: map[string]string{}}},
		Deps: []string{}, Env: map[string]string{}, Inputs: []string{},
		KV: map[string]string{"p": "CC", "html": "a<b>c"}, Outputs: []string{},
		Requirements: map[string]interface{}{}, Tags: []string{},
		TargetProperties: map[string]string{},
	}
	canon, err := canonicalNodeBytes(n)
	if err != nil {
		t.Fatalf("canonicalNodeBytes: %v", err)
	}
	defaultMarshalled, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if bytes.Equal(canon, defaultMarshalled) {
		t.Fatalf("canonicalNodeBytes and json.Marshal produced identical bytes; "+
			"either default escaping changed or canonicalNodeBytes lost its "+
			"SetEscapeHTML(false). canon=%s default=%s", canon, defaultMarshalled)
	}
	if !bytes.Contains(canon, []byte("<b>")) || !bytes.Contains(canon, []byte("a<b>c")) {
		t.Errorf("canonicalNodeBytes should preserve literal '<' chars; got: %s", canon)
	}
	// json.Marshal escapes '<' as the six-byte sequence <. We assert
	// on those literal six bytes (how the escape appears in the encoded
	// JSON), not on the rune '<'. Use an interpreted string with a
	// double backslash so the byte slice is the six chars '\','u','0','0','3','c'.
	const escapedLT = "\\u003c"
	if !bytes.Contains(defaultMarshalled, []byte(escapedLT)) {
		t.Errorf("default json.Marshal should escape '<' as %s; got: %s", escapedLT, defaultMarshalled)
	}
	if bytes.Contains(canon, []byte(escapedLT)) {
		t.Errorf("canonicalNodeBytes must NOT contain %s (escaping disabled); got: %s", escapedLT, canon)
	}
}

func TestCanonicalNodeBytes_DoesNotEscapeHTML(t *testing.T) {
	// json.Encoder defaults to escaping <, >, & — we explicitly turned
	// that off so command strings round-trip verbatim. Pin the
	// behaviour with a node whose command contains all three.
	n := &Node{
		Cmds: []Cmd{{CmdArgs: []string{"sh", "-c", "echo <a> & echo b"}, Env: map[string]string{}}},
		Deps: []string{}, Env: map[string]string{}, Inputs: []string{},
		KV: map[string]string{}, Outputs: []string{},
		Requirements: map[string]interface{}{}, Tags: []string{},
		TargetProperties: map[string]string{},
	}
	canon, err := canonicalNodeBytes(n)
	if err != nil {
		t.Fatalf("canonicalNodeBytes: %v", err)
	}
	s := string(canon)
	if !strings.Contains(s, "<a>") {
		t.Errorf("expected literal <a> in canonical bytes, got: %s", s)
	}
	if !strings.Contains(s, " & ") {
		t.Errorf("expected literal & in canonical bytes, got: %s", s)
	}
	// The default json.Encoder would have rewritten <, >, & as the
	// six-char < / > / & escapes. We turned that off,
	// so the raw chars survive and the escape sequences must NOT
	// appear.
	for _, banned := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(s, banned) {
			t.Errorf("canonical bytes contain escaped HTML %q: %s", banned, s)
		}
	}
}
