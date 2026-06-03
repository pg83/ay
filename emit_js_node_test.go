package main

import (
	"reflect"
	"testing"
)

func TestEmitJS_UsesRequestedPlatformTags(t *testing.T) {
	emit := NewBufferedEmitter()
	target := newTestPlatform(OSLinux, ISAX8664, "no", []string{"default-linux-x86_64", "debug", "SANDBOXING=yes"})

	ref, _ := EmitJS(hostInstance("joinmod"), "all.cpp", []string{"a.cpp"}, nil, target, nil, emit)
	got := emit.nodes[ref]

	if got.Platform != string(target.Target) {
		t.Fatalf("JS platform = %q, want %q", got.Platform, target.Target)
	}
	if !reflect.DeepEqual(got.Tags, target.Tags) {
		t.Fatalf("JS tags = %#v, want %#v", got.Tags, target.Tags)
	}
	if got.TargetProperties["module_dir"] != "joinmod" {
		t.Fatalf("JS module_dir = %q, want joinmod", got.TargetProperties["module_dir"])
	}
}
