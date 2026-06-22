package main

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"
)

func emitTestCompileGraph(t *testing.T, host, target *Platform) *Graph {
	t.Helper()

	execEmit := newBufferedEmitter()
	// CLANG is declared by build/platform/clang; its FETCH node is emitted up front
	// (as genResourcesLibrary/emitResourceFetch would) into the emitter's fetchRefs,
	// which Node.buildDeps consults on the fly for consumers' $(CLANG) deps.
	execEmit.fetchRefs.put(internStr(resourcePatternClangTool), execEmit.emit(&Node{
		Platform: host,
		Cmds:     []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"ay", "fetch", "$(B)", "$(S)", "sbr:clang", "resources/CLANG"})}}},
		KV:       KV{P: pkFETCH, PC: pcYellow, ShowOut: true},
		Outputs:  []VFS{build("resources/" + resourcePatternClangTool)},
	}))
	clangTool := prebuiltToolchainFlags()["CLANG_TOOL"]
	ref := execEmit.emit(&Node{Platform: target,
		Cmds: []Cmd{{
			CmdArgs: ArgChunks{appendInternStrs(nil, []string{clangTool, "-c", "$(S)/pkg/app/main.cpp", "-o", "$(B)/pkg/app/main.o"})},
			Env:     nil,
		}},
		Env:              nil,
		Inputs:           InputChunks{{intern("$(S)/pkg/app/main.cpp")}},
		KV:               KV{P: pkCC},
		Outputs:          []VFS{intern("$(B)/pkg/app/main.o")},
		Requirements:     Requirements{},
		TargetProperties: TargetProperties{},
		Resources:        []STR{internStr(resourcePatternClangTool)},
	})
	execEmit.result(ref)

	return finalize(execEmit)
}

func assertSingleUsedClangFetch(t *testing.T, graph *Graph) {
	t.Helper()

	fetchOutputs := map[string]bool{}
	var cc *Node
	for _, node := range graph.Graph {
		if len(node.Outputs) == 1 && node.Outputs[0].string() == "$(B)/pkg/app/main.o" {
			cc = node
			continue
		}
		if len(node.Outputs) == 1 {
			fetchOutputs[node.Outputs[0].string()] = true
		}
	}

	if cc == nil {
		t.Fatal("execution graph missing expected CC node")
	}
	if len(graphDeps(graph, cc)) != 1 {
		t.Fatalf("execution graph CC deps = %v, want single used-resource FETCH dep", graphDeps(graph, cc))
	}
	if len(fetchOutputs) != 1 {
		t.Fatalf("execution graph fetch outputs = %#v, want only the used CLANG fetch node", fetchOutputs)
	}
	if !fetchOutputs["$(B)/resources/"+resourcePatternClangTool] {
		t.Fatalf("execution graph missing used CLANG fetch node: %#v", fetchOutputs)
	}
	if fetchOutputs["$(B)/resources/"+resourcePatternClang18] {
		t.Fatalf("execution graph unexpectedly contains unused CLANG18 fetch node: %#v", fetchOutputs)
	}
	if fetchOutputs["$(B)/resources/"+resourcePatternClang20] {
		t.Fatalf("execution graph unexpectedly contains unused CLANG20 fetch node: %#v", fetchOutputs)
	}
}

// TestPlaceSandboxResource_RenameResource reproduces the sg7 build failure on
// the FROM_SANDBOX node `--copy-to-dir . --rename RESOURCE -- blacklist_default.json`:
// fetch_from.py's --rename is a single-token append (action='append'), and the
// special name RESOURCE denotes the fetched file itself, copied onto the declared
// output. The former two-token parse captured ("RESOURCE", "--") and ran
// os.Rename("RESOURCE", "--"), which failed with "no such file or directory" and
// never produced blacklist_default.json (consumed downstream by an objcopy node).
func TestPlaceSandboxResource_RenameResource(t *testing.T) {
	t.Chdir(t.TempDir())

	fetched := filepath.Join(t.TempDir(), "resource")
	if err := os.WriteFile(fetched, []byte("BLACKLIST"), 0o644); err != nil {
		t.Fatal(err)
	}

	placeSandboxResource(fetched, ".", "", []string{"RESOURCE"}, []string{"blacklist_default.json"}, false)

	got, err := os.ReadFile("blacklist_default.json")
	if err != nil {
		t.Fatalf("output not produced: %v", err)
	}

	if string(got) != "BLACKLIST" {
		t.Fatalf("output content = %q, want %q", got, "BLACKLIST")
	}
}

// TestPlaceSandboxResource_UntarTo pins the other FROM_SANDBOX pattern in the same
// build (`--untar-to . -- icookie_blacklist.json`): the archive is extracted into
// the build dir and the declared output is the extracted member.
func TestPlaceSandboxResource_UntarTo(t *testing.T) {
	t.Chdir(t.TempDir())

	fetched := filepath.Join(t.TempDir(), "resource")
	f, err := os.Create(fetched)
	if err != nil {
		t.Fatal(err)
	}

	tw := tar.NewWriter(f)
	body := []byte("{}")
	throw(tw.WriteHeader(&tar.Header{Name: "icookie_blacklist.json", Mode: 0o644, Size: int64(len(body))}))
	throw2(tw.Write(body))
	throw(tw.Close())
	throw(f.Close())

	placeSandboxResource(fetched, "", ".", nil, []string{"icookie_blacklist.json"}, false)

	if _, err := os.Stat("icookie_blacklist.json"); err != nil {
		t.Fatalf("extracted output missing: %v", err)
	}
}

func TestResourceGraphEmitter_OnlyMaterializesUsedFetchNodes(t *testing.T) {
	host := newTestPlatform(OSLinux, ISAX8664, "yes")
	target := sandboxedX8664TargetPlatform()

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
}

func TestResourceGraphEmitter_ReusedEmitterEmitsFetchPerEmitter(t *testing.T) {
	host := newTestPlatform(OSLinux, ISAX8664, "yes")
	target := sandboxedX8664TargetPlatform()

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
}

// TestSSHAgentOAuth exercises the live SSH-agent → OAuth → Sandbox path end to end.
// Guarded behind AY_TEST_SSH_OAUTH because it talks to the SSH agent (may prompt a
// Secure-Enclave touch) and the network. Run: AY_TEST_SSH_OAUTH=1 go test -run SSHAgentOAuth -v
func TestSSHAgentOAuth(t *testing.T) {
	if os.Getenv("AY_TEST_SSH_OAUTH") == "" {
		t.Skip("set AY_TEST_SSH_OAUTH=1 to exercise the live SSH-agent OAuth exchange")
	}

	tok := tokenFromSSHAgent(oauthLogin())
	if tok == "" {
		t.Fatal("tokenFromSSHAgent returned empty (no agent key accepted)")
	}

	t.Logf("got OAuth token via SSH agent (login=%s, len=%d)", oauthLogin(), len(tok))

	// The token must authenticate the Sandbox API.
	info := querySandboxResource("8563229520", tok)
	if info.State != "READY" {
		t.Fatalf("sandbox resource state = %q, want READY", info.State)
	}
}
