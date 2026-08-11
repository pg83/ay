package main

import "testing"

func TestEmitJS_UsesRequestedPlatformTags(t *testing.T) {
	emit := newStreamingEmitter(nil)
	target := newTestPlatform(OSLinux, ISAX8664, "no")

	ref := emit.reserve()

	nodeTestEmitContext(emit, hostInstance("joinmod")).emitJSReserved("all.cpp", []string{"a.cpp"}, nil, target, testToolchain(), nil, ref)

	got := emit.nodes.s[ref]

	if string(got.Platform.Target) != string(target.Target) {
		t.Fatalf("JS platform = %q, want %q", string(got.Platform.Target), target.Target)
	}
}
