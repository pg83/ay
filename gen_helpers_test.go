package main

// gen_helpers_test.go — test-only shim that constructs the canonical
// (host=linux-x86_64, target=linux-aarch64) Platform pair with the
// shared testToolchainFlags and dispatches into GenWithMode. Lives
// alongside the test corpus rather than in production code: every
// production caller of GenWithMode (cmdGen / cmdMake) constructs
// platforms inline from CLI + mining, so a generic "Gen" entry that
// hardcodes defaults would just be misleading.

func testGen(sourceRoot, targetDir string) *Graph {
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"}, true)
	targetFlags := make(map[string]string, len(testToolchainFlags)+2)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["PIC"] = "no"
	targetFlags["MUSL"] = "yes"
	target := NewPlatform(OSLinux, ISAAArch64, targetFlags, nil, false)
	return GenWithMode(sourceRoot, targetDir, host, target, defaultScanCtxMode)
}
