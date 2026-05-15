package main

import (
	"reflect"
	"testing"
)

func TestParseCompilerFlags(t *testing.T) {
	got := parseCompilerFlags(`-O2 -DNAME="hello world" '-DOTHER=two words' -DQUOTE=\"x\" trailing\ slash`)
	want := []string{
		"-O2",
		"-DNAME=hello world",
		"-DOTHER=two words",
		`-DQUOTE="x"`,
		"trailing slash",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCompilerFlags mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNewPlatform_ParsesCompilerFlags(t *testing.T) {
	flags := map[string]string{
		"PIC": "no",
	}

	p := NewPlatform(OSLinux, ISAAArch64, flags, nil, false, `-O2 -DNAME="hello world"`, `-stdlib=libc++ -DCPP=1`)

	if !reflect.DeepEqual(p.CFlags, []string{"-O2", "-DNAME=hello world"}) {
		t.Fatalf("CFlags = %#v", p.CFlags)
	}

	if !reflect.DeepEqual(p.CXXFlags, []string{"-stdlib=libc++", "-DCPP=1"}) {
		t.Fatalf("CXXFlags = %#v", p.CXXFlags)
	}
}
