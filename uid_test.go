package main

import (
	"bytes"
	"crypto/md5"
	encHex "encoding/hex"
	"encoding/json"
	"reflect"
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

func TestNodeStatsUID_KnownVector(t *testing.T) {
	n := &Node{
		KV:       map[string]interface{}{"p": "LD"},
		Outputs:  []VFS{Build("tools/archiver/archiver")},
		Platform: "default-linux-aarch64",
		StatsTags: []string{
			"FAKEID=sandboxing",
			"SANDBOXING=yes",
			"debug",
			"default-linux-aarch64",
			"musl",
		},
	}

	const want = "c76f8ebdc20cd1d452491e62afe5aa78"
	if got := nodeStatsUID(n); got != want {
		t.Fatalf("nodeStatsUID mismatch:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(n))
	}
}

func TestNodeStatsUID_IgnoresUnrelatedTargetCLIFlags(t *testing.T) {
	newNode := func(cliFlags map[string]string) *Node {
		flags := map[string]string{
			"GG_BUILD_TYPE": "debug",
			"PIC":           "no",
			"SANDBOXING":    "yes",
		}
		for k, v := range cliFlags {
			flags[k] = v
		}
		p := NewPlatform(OSLinux, ISAAArch64, flags, nil, "", "")
		p.StatsFlags = buildTargetStatsFlags(flags, cliFlags)

		return &Node{
			KV:        map[string]interface{}{"p": "LD"},
			Outputs:   []VFS{Build("tools/archiver/archiver")},
			Platform:  string(p.Target),
			StatsTags: statsTagsForPlatform(p),
		}
	}

	base := newNode(map[string]string{"MUSL": "yes"})
	withUnrelated := newNode(map[string]string{
		"MUSL":      "yes",
		"UNRELATED": "yes",
	})

	const want = "c76f8ebdc20cd1d452491e62afe5aa78"
	if got := nodeStatsUID(base); got != want {
		t.Fatalf("base nodeStatsUID mismatch:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(base))
	}

	if got := nodeStatsUID(withUnrelated); got != want {
		t.Fatalf("unrelated target flag perturbed stats_uid:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(withUnrelated))
	}

	for _, tag := range withUnrelated.StatsTags {
		if tag == "UNRELATED=yes" {
			t.Fatalf("unrelated target flag leaked into stats tags: %#v", withUnrelated.StatsTags)
		}
	}
}

func TestNodeStatsUID_UsesBaseTargetFlags(t *testing.T) {
	flags := map[string]string{
		"GG_BUILD_TYPE": "debug",
		"PIC":           "no",
		"USE_LTO":       "yes",
	}
	p := NewPlatform(OSLinux, ISAAArch64, flags, nil, "", "")
	p.StatsFlags = buildTargetStatsFlags(flags, map[string]string{"UNRELATED": "yes"})

	n := &Node{
		KV:        map[string]interface{}{"p": "LD"},
		Outputs:   []VFS{Build("tools/archiver/archiver")},
		Platform:  string(p.Target),
		StatsTags: statsTagsForPlatform(p),
	}

	wantTags := []string{
		"default-linux-aarch64",
		"debug",
		"lto",
	}
	if got := n.StatsTags; !reflect.DeepEqual(got, wantTags) {
		t.Fatalf("stats tags mismatch:\n got: %#v\nwant: %#v", got, wantTags)
	}

	const want = "bdbe1d8ab9fcd589050e754ac396dd57"
	if got := nodeStatsUID(n); got != want {
		t.Fatalf("base target flag stats_uid mismatch:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(n))
	}
}

func TestNodeStatsUID_UsesLongRootOutputs(t *testing.T) {
	n := &Node{
		KV:       map[string]interface{}{"p": "LD"},
		Outputs:  []VFS{Build("tools/archiver/archiver")},
		Platform: "default-linux-aarch64",
		StatsTags: []string{
			"FAKEID=sandboxing",
			"SANDBOXING=yes",
			"debug",
			"default-linux-aarch64",
			"musl",
		},
	}

	got := nodeStatsUID(n)

	shortRootPreimage := pythonStringListRepr([]string{
		n.Platform,
		pythonStringListRepr(sortedStatsTags(n)),
		"LD",
		pythonStringListRepr([]string{Build("tools/archiver/archiver").String()}),
	})
	shortRootHash := md5.Sum([]byte(shortRootPreimage))
	shortRootUID := encHex.EncodeToString(shortRootHash[:])

	if got == shortRootUID {
		t.Fatalf("nodeStatsUID used short-root outputs: %s", shortRootPreimage)
	}
}

func TestCanonicalNodeBytes_ZeroesIdentityFields(t *testing.T) {
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
		UID:              "should-not-appear-AAAAA",
		SelfUID:          "should-not-appear-BBBBB",
		StatsUID:         "should-not-appear-CCCCC",
	}
	canon := canonicalNodeBytes(n)
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
		Deps: []string{}, Env: map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: map[string]interface{}{"p": "CC", "html": "a<b>c"}, Outputs: ToVFSSlice([]string{}),
		Requirements: map[string]interface{}{}, Tags: []string{},
		TargetProperties: map[string]string{},
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
		Deps: []string{}, Env: map[string]string{}, Inputs: ToVFSSlice([]string{}),
		KV: map[string]interface{}{}, Outputs: ToVFSSlice([]string{}),
		Requirements: map[string]interface{}{}, Tags: []string{},
		TargetProperties: map[string]string{},
	}
	canon := canonicalNodeBytes(n)
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
