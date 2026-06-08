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

func TestNodeStatsUID_KnownVector(t *testing.T) {
	n := &Node{
		KV:       KV{P: pkLD},
		Outputs:  []VFS{Intern("$(B)/tools/archiver/archiver")},
		Platform: &Platform{Target: "default-linux-aarch64"},
		StatsTags: []string{
			"FAKEID=sandboxing",
			"SANDBOXING=yes",
			"debug",
			"default-linux-aarch64",
			"race",
		},
	}

	const want = "b3107447d6cc0947f72c25f4ce0c059f"
	if got := nodeStatsUID(n, &canonBuf{}); got != want {
		t.Fatalf("nodeStatsUID mismatch:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(n, &canonBuf{}))
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
		p := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, nil, "", "", nil)
		p.StatsFlags = buildTargetStatsFlags(flags, cliFlags)

		return &Node{
			KV:        KV{P: pkLD},
			Outputs:   []VFS{Intern("$(B)/tools/archiver/archiver")},
			Platform:  &Platform{Target: p.Target},
			StatsTags: statsTagsForPlatform(p),
		}
	}

	base := newNode(map[string]string{"RACE": "yes"})
	withUnrelated := newNode(map[string]string{
		"RACE":      "yes",
		"UNRELATED": "yes",
	})

	const want = "b3107447d6cc0947f72c25f4ce0c059f"
	if got := nodeStatsUID(base, &canonBuf{}); got != want {
		t.Fatalf("base nodeStatsUID mismatch:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(base, &canonBuf{}))
	}

	if got := nodeStatsUID(withUnrelated, &canonBuf{}); got != want {
		t.Fatalf("unrelated target flag perturbed stats_uid:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(withUnrelated, &canonBuf{}))
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
	p := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, nil, "", "", nil)
	p.StatsFlags = buildTargetStatsFlags(flags, map[string]string{"UNRELATED": "yes"})

	n := &Node{
		KV:        KV{P: pkLD},
		Outputs:   []VFS{Intern("$(B)/tools/archiver/archiver")},
		Platform:  &Platform{Target: p.Target},
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
	if got := nodeStatsUID(n, &canonBuf{}); got != want {
		t.Fatalf("base target flag stats_uid mismatch:\n got: %s\nwant: %s\npreimage: %s", got, want, statsUIDPreimage(n, &canonBuf{}))
	}
}

func TestNodeStatsUID_UsesLongRootOutputs(t *testing.T) {
	n := &Node{
		KV:       KV{P: pkLD},
		Outputs:  []VFS{Intern("$(B)/tools/archiver/archiver")},
		Platform: &Platform{Target: "default-linux-aarch64"},
		StatsTags: []string{
			"FAKEID=sandboxing",
			"SANDBOXING=yes",
			"debug",
			"default-linux-aarch64",
			"race",
		},
	}

	got := nodeStatsUID(n, &canonBuf{})

	pc := &canonBuf{}
	shortRootPreimage := pythonStringListRepr(pc, []string{
		platformTarget(n.Platform),
		pythonStringListRepr(pc, sortedStatsTags(n)),
		"LD",
		pythonStringListRepr(pc, []string{Intern("$(B)/tools/archiver/archiver").String()}),
	})
	shortRootHash := md5.Sum([]byte(shortRootPreimage))
	shortRootUID := encHex.EncodeToString(shortRootHash[:])

	if got == shortRootUID {
		t.Fatalf("nodeStatsUID used short-root outputs: %s", shortRootPreimage)
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
		StatsUID:         "should-not-appear-CCCCC",
	}
	canon := canonicalNodeBytes(n)
	s := string(canon)

	for _, banned := range []string{tuid("AAAAA").String(), tuid("BBBBB").String(), "should-not-appear-CCCCC"} {
		if strings.Contains(s, banned) {
			t.Errorf("canonicalNodeBytes leaked identity field value %q: %s", banned, s)
		}
	}

	if n.UID == (UID{}) || n.SelfUID == (UID{}) || n.StatsUID == "" {
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

func TestCanonicalNodeBytes_DoesNotEscapeHTML(t *testing.T) {

	n := &Node{
		Cmds: []Cmd{{CmdArgs: appendInternStrs(nil, []string{"sh", "-c", "echo <a> & echo b"}), Env: nil}},
		Env:  nil, Inputs: ToVFSSlice([]string{}),
		KV: KV{}, Outputs: ToVFSSlice([]string{}),
		Requirements: Requirements{}, Tags: []string{},
		TargetProperties: TargetProperties{},
	}
	canon := canonicalNodeBytes(n)
	s := string(canon)

	if !strings.Contains(s, "<a>") {
		t.Errorf("expected literal <a> in canonical bytes, got: %s", s)
	}

	if !strings.Contains(s, " & ") {
		t.Errorf("expected literal & in canonical bytes, got: %s", s)
	}

	for _, banned := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(s, banned) {
			t.Errorf("canonical bytes contain escaped HTML %q: %s", banned, s)
		}
	}
}
