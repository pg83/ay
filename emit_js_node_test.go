package main

import (
	"reflect"
	"testing"
)

func TestEmitJS_UsesRequestedPlatformTags(t *testing.T) {
	emit := NewBufferedEmitter()
	target := newTestPlatform(OSLinux, ISAX8664, "no", []string{"default-linux-x86_64", "debug", "SANDBOXING=yes"})

	ref, _ := EmitJS(hostInstance("joinmod"), "all.cpp", []string{"a.cpp"}, nil, target, testToolchain(), nil, emit)
	got := emit.nodes[ref]

	if string(got.Platform.Target) != string(target.Target) {
		t.Fatalf("JS platform = %q, want %q", string(got.Platform.Target), target.Target)
	}
	if !reflect.DeepEqual(nodeTags(got), target.Tags) {
		t.Fatalf("JS tags = %#v, want %#v", nodeTags(got), target.Tags)
	}
	if got.TargetProperties.ModuleDir != "joinmod" {
		t.Fatalf("JS module_dir = %q, want joinmod", got.TargetProperties.ModuleDir)
	}
}
