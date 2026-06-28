package main

import (
	"reflect"
	"testing"
)

func TestReverseStr(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"a":        "a",
		".proto":   "otorp.",
		".pxd.pxi": "ixp.dxp.",
	}

	for in, want := range cases {
		if got := reverseStr(in); got != want {
			t.Errorf("reverseStr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitShellWords(t *testing.T) {
	got := splitShellWords(`-O2 -DNAME="hello world" '-DOTHER=two words' -DQUOTE=\"x\" trailing\ slash`)
	want := []string{
		"-O2",
		"-DNAME=hello world",
		"-DOTHER=two words",
		`-DQUOTE="x"`,
		"trailing slash",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitShellWords mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
