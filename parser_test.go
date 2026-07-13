package main

import "testing"

func TestChunkLinesAcrossBoundaries(t *testing.T) {
	var got []string

	eachLine([][]byte{[]byte("first\r"), []byte("\nsecond\nthi"), []byte("rd\nlast")}, func(line []byte) {
		got = append(got, string(line))
	})

	want := []string{"first", "second", "third", "last"}

	if len(got) != len(want) {
		t.Fatalf("got lines %q, want %q", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCParserAcrossChunkBoundaries(t *testing.T) {
	set := CIncludeDirectiveParser{}.parse("source.cpp", [][]byte{
		[]byte("/* leading\n#include <hidden.h>\n"),
		[]byte("*/\n#include <visi"),
		[]byte("ble.h>\n"),
	}, newBumpAllocator[IncludeDirective]())
	directives := set.bucket(parsedIncludesLocal)

	if len(directives) != 1 || directives[0].target.string() != "visible.h" {
		t.Fatalf("chunked directives = %+v", directives)
	}
}

func TestCParserMatchesSingleChunkAtEverySplit(t *testing.T) {
	data := []byte("/* leading\n#include <hidden.h>\n*/\n# include <visible.h>\n\t#include \"quoted.h\"\n")
	want := parseCTestTargets(t, [][]byte{data})

	for split := 0; split <= len(data); split++ {
		got := parseCTestTargets(t, [][]byte{data[:split], data[split:]})

		if len(got) != len(want) {
			t.Fatalf("split %d: got %v, want %v", split, got, want)
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("split %d: got %v, want %v", split, got, want)
			}
		}
	}
}

func parseCTestTargets(t *testing.T, chunks [][]byte) []string {
	t.Helper()

	set := CIncludeDirectiveParser{}.parse("source.cpp", chunks, newBumpAllocator[IncludeDirective]())
	directives := set.bucket(parsedIncludesLocal)
	out := make([]string, len(directives))

	for i, directive := range directives {
		out[i] = directive.target.string()
	}

	return out
}

func TestParseDelimitedIncludeTarget_QuotedAngleSystem(t *testing.T) {
	target, kind, ok := parseDelimitedIncludeTarget([]byte("\"<util/system/error.h>\""))

	if !ok {
		t.Fatal("parseDelimitedIncludeTarget returned ok=false")
	}

	if bytesString(target) != "util/system/error.h" {
		t.Fatalf("target = %q, want %q", target, "util/system/error.h")
	}

	if kind != includeSystem {
		t.Fatalf("kind = %v, want includeSystem", kind)
	}
}
