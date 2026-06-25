package main

import "testing"

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

func TestExtMatcherBasic(t *testing.T) {
	m := NewExtMatcher([]ExtEntry[int]{
		{".c", 1},
		{".cpp", 2},
		{".proto", 3},
	})

	cases := []struct {
		path string
		want int
		ok   bool
	}{
		{"foo.c", 1, true},
		{"foo.cpp", 2, true},
		{"a/b/c.proto", 3, true},
		{"deep/dir.with.dots/file.cpp", 2, true},
		{"foo.h", 0, false},
		{"foo", 0, false},
		{"", 0, false},
		{"foo.cc", 0, false},
	}

	for _, c := range cases {
		got, ok := m.match(c.path)

		if ok != c.ok || got != c.want {
			t.Errorf("match(%q) = (%d,%v), want (%d,%v)", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestExtMatcherLongestSuffixWins(t *testing.T) {
	m := NewExtMatcher([]ExtEntry[int]{
		{".pxi", 1},
		{".pxd.pxi", 2},
		{".cpp", 3},
		{".cpp.in", 4},
	})

	cases := []struct {
		path string
		want int
	}{
		{"a.pxi", 1},
		{"a.pxd.pxi", 2},
		{"a.cpp", 3},
		{"a.cpp.in", 4},
	}

	for _, c := range cases {
		got, ok := m.match(c.path)

		if !ok || got != c.want {
			t.Errorf("match(%q) = (%d,%v), want (%d,true)", c.path, got, ok, c.want)
		}
	}
}

func TestExtMatcherDotAlignment(t *testing.T) {
	m := NewExtMatcher([]ExtEntry[int]{
		{".proto", 1},
		{".s", 2},
	})

	// ".proto" must not match files merely ending in "proto" without the dot
	if _, ok := m.match("foo.gztproto"); ok {
		t.Errorf("match(foo.gztproto) unexpectedly matched .proto")
	}

	// ".s" must not match a bare "s" without the dot
	if _, ok := m.match("foos"); ok {
		t.Errorf("match(foos) unexpectedly matched .s")
	}

	if got, ok := m.match("x.s"); !ok || got != 2 {
		t.Errorf("match(x.s) = (%d,%v), want (2,true)", got, ok)
	}
}

func TestParserExtMatcherParity(t *testing.T) {
	cases := map[string]IncludeDirectiveParser{
		"foo.cpp":      CIncludeDirectiveParser{},
		"foo.proto":    ProtoIncludeDirectiveParser{},
		"foo.gztproto": ProtoIncludeDirectiveParser{},
		"foo.cfgproto": CfgProtoIncludeDirectiveParser{},
		"foo.fbs":      FlatbuffersIncludeDirectiveParser{},
		"foo.fbs64":    FlatbuffersIncludeDirectiveParser{},
		"foo.pyx":      CythonIncludeDirectiveParser{},
		"foo.pxd.pxi":  CythonIncludeDirectiveParser{},
		"foo.rl6":      RagelIncludeDirectiveParser{},
		"foo.swg":      SwigIncludeDirectiveParser{},
		"foo.asm":      YasmIncludeDirectiveParser{},
		"foo.cpp.in":   CIncludeDirectiveParser{},
		"foo.h.in":     CIncludeDirectiveParser{},
	}

	for path, want := range cases {
		got := lookupParserForRel(path)

		if got != want {
			t.Errorf("lookupParserForRel(%q) = %T, want %T", path, got, want)
		}
	}

	if got := lookupParserForRel("foo.unknown"); got != nil {
		t.Errorf("lookupParserForRel(foo.unknown) = %T, want nil", got)
	}
}
