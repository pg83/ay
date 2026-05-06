package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
)

// uid.go — content-derived UID hashing.
//
// D7: UID = base64url(sha1(canonical-node-bytes))[:22]. The 22-character
// length is verified against the on-disk reference (every uid/self_uid in
// /home/pg/monorepo/yatool_orig/g.json is exactly 22 characters of
// base64url alphabet). stats_uid is a different beast (32-char MD5 hex)
// and is left empty by PR-02 per the plan; later PRs will fill it.
//
// Canonicalization for hashing: marshal the Node with its UID/SelfUID/
// StatsUID fields zeroed (so the hash is over content, not over identity),
// using a json.Encoder with HTML escaping disabled (D14). encoding/json
// already sorts map keys alphabetically when marshalling, which gives us
// deterministic output for env/kv/requirements/target_properties/
// foreign_deps. Slice ordering is the caller's responsibility — Finalize
// sorts Deps and the per-key ForeignDeps slices before the canonicalization
// step.

const uidLength = 22

// computeUID returns the 22-character base64url SHA-1 of the input bytes.
// This is the entire UID derivation: stable, content-only, no salt.
func computeUID(canonicalBytes []byte) string {
	sum := sha1.Sum(canonicalBytes)
	return base64.RawURLEncoding.EncodeToString(sum[:])[:uidLength]
}

// canonicalNodeBytes produces the byte sequence we hash to derive a node's
// UID. It marshals a copy of n with the three identity fields cleared so
// the hash depends only on content; HTML escaping is disabled so '<', '>',
// '&' survive verbatim (they appear in command lines and would otherwise
// be turned into < etc., causing two callers that produce semantically
// identical input to disagree on the hash if one of them happens to use a
// non-default encoder elsewhere).
//
// The trailing newline that json.Encoder.Encode appends is stripped before
// hashing so the canonical form is exactly the marshalled object.
func canonicalNodeBytes(n *Node) ([]byte, error) {
	clone := *n
	clone.UID = ""
	clone.SelfUID = ""
	clone.StatsUID = ""

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&clone); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// Encode appends '\n'; trim it so the canonical bytes are exactly the
	// marshalled object.
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}
