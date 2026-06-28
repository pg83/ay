package main

import (
	"os"
	"testing"
)

func TestSSHAgentOAuth(t *testing.T) {
	if os.Getenv("AY_TEST_SSH_OAUTH") == "" {
		t.Skip("set AY_TEST_SSH_OAUTH=1 to exercise the live SSH-agent OAuth exchange")
	}

	tok := tokenFromSSHAgent(oauthLogin())

	if tok == "" {
		t.Fatal("tokenFromSSHAgent returned empty (no agent key accepted)")
	}

	t.Logf("got OAuth token via SSH agent (login=%s, len=%d)", oauthLogin(), len(tok))

	info := querySandboxResource("8563229520", tok)

	if info.State != "READY" {
		t.Fatalf("sandbox resource state = %q, want READY", info.State)
	}
}
