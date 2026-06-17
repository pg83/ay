package main

import (
	"os"
	"testing"
)

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
