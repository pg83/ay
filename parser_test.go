package main

import "testing"

func TestParseDelimitedIncludeTarget_QuotedAngleSystem(t *testing.T) {
	target, kind, ok := parseDelimitedIncludeTarget("\"<util/system/error.h>\"")

	if !ok {
		t.Fatal("parseDelimitedIncludeTarget returned ok=false")
	}

	if target != "util/system/error.h" {
		t.Fatalf("target = %q, want %q", target, "util/system/error.h")
	}

	if kind != includeSystem {
		t.Fatalf("kind = %v, want includeSystem", kind)
	}
}
