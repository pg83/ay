package main

import (
	"github.com/zeebo/xxh3"
	"strings"
	"testing"
)

func tuid(label string) UID {
	return computeUID([]byte(label))
}

func TestComputeUID_LengthAndAlphabet(t *testing.T) {
	got := computeUID([]byte("hello")).string()

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
	got := computeUID([]byte("")).string()
	const want = "maoG0wFHmNhgAcMkRo1Jfw"

	if got != want {
		t.Errorf("computeUID(\"\") = %q, want %q", got, want)
	}
}

func TestCanonicalNodeBytes_ZeroesIdentityFields(t *testing.T) {
	n := Node{Platform: &Platform{},
		Cmds: []Cmd{},

		Env:          nil,
		Inputs:       InputChunks{ToVFSSlice([]string{})},
		KV:           &KV{},
		Outputs:      ToVFSSlice([]string{}),
		Requirements: Requirements{},
		UID:          tuid("AAAAA"),
		SelfUID:      tuid("BBBBB"),
	}
	canon := canonicalNodeBytes(&n)
	s := string(canon)

	for _, banned := range []string{tuid("AAAAA").string(), tuid("BBBBB").string(), "should-not-appear-CCCCC"} {
		if strings.Contains(s, banned) {
			t.Errorf("canonicalNodeBytes leaked identity field value %q: %s", banned, s)
		}
	}

	if n.UID == (UID{}) || n.SelfUID == (UID{}) {
		t.Errorf("canonicalNodeBytes mutated the original node: %+v", n)
	}
}

func computeUID(canonicalBytes []byte) UID {
	sum := xxh3.Hash128(canonicalBytes)

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

func canonicalNodeBytes(n *Node) []byte {
	var c CanonBuf
	c.writeNode(n)

	return c.buf
}
