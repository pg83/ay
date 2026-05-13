package main

import (
	"crypto/sha1"
	"encoding/base64"
	"sort"
	"strconv"
	"sync"
)

// uid.go — content-derived UID hashing.
//
// D7: UID = base64url(sha1(canonical-node-bytes))[:22]. The 22-character
// length is verified against the on-disk reference (every uid/self_uid in
// /home/pg/monorepo/yatool_orig/sg.json is exactly 22 characters of
// base64url alphabet). stats_uid is a different beast (32-char MD5 hex)
// and is left empty by PR-02 per the plan; later PRs will fill it.
//
// Canonicalization for hashing: emit the Node fields in alphabetical
// order, compact form (no whitespace), HTML escaping disabled, with
// UID/SelfUID/StatsUID excluded so the hash is over content, not
// over identity. Maps emit keys sorted; slice ordering is the
// caller's responsibility — Finalize sorts Deps and per-key
// ForeignDeps slices before calling here.
//
// PR-M3-vfs-purge-canonjson: replaces the reflection-based
// `encoding/json` path. The pre-refactor implementation ran
// `json.NewEncoder(&buf).Encode(&clone)` per node, which dominated
// the alloc profile (~9.6M allocations across one M3 gen, ~53% of
// the run's total, ~10% of CPU on encoding/json + VFS.MarshalJSON).
// The custom writer below reuses the `appendString` /
// `appendStringEscapedBody` / `appendVFS` primitives from
// gjson_write.go and pools its buffer; VFS values are emitted via
// their inline-prefix form (no `v.String()` concat).

const uidLength = 22

// computeUID returns the 22-character base64url SHA-1 of the input bytes.
// This is the entire UID derivation: stable, content-only, no salt.
func computeUID(canonicalBytes []byte) string {
	sum := sha1.Sum(canonicalBytes)

	return base64.RawURLEncoding.EncodeToString(sum[:])[:uidLength]
}

// canonBufPool reuses buffer backing arrays across canonicalNodeBytes
// calls. One bytes-slice per goroutine is taken on entry and returned
// after the canonical form is hashed; the per-node allocation cost
// becomes amortised across the whole emitter run.
var canonBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 4096)

		return &b
	},
}

// canonicalNodeBytes produces the byte sequence we hash to derive a
// node's UID. The output excludes UID/SelfUID/StatsUID so the hash
// depends only on content; HTML escaping is disabled so '<', '>',
// '&' survive verbatim (they appear in command lines and would
// otherwise be turned into &lt; etc., causing two callers that
// produce semantically identical input to disagree on the hash if
// one of them happens to use a non-default encoder elsewhere).
//
// The caller owns the returned slice; the underlying buffer is a
// pool-borrowed scratch space that has already been copied out of.
func canonicalNodeBytes(n *Node) []byte {
	bufP := canonBufPool.Get().(*[]byte)
	scratch := (*bufP)[:0]
	scratch = appendNodeCanonical(scratch, n)
	out := append([]byte(nil), scratch...)
	*bufP = scratch[:0]
	canonBufPool.Put(bufP)

	return out
}

// appendNodeCanonical emits the compact alphabetical canonical form
// of a Node (no indentation, no whitespace between tokens) used for
// content-hashing. Field order matches gjson_write.appendNode (which
// is alphabetical, per the Node struct's declaration). UID, SelfUID,
// and StatsUID are excluded so two semantically-equal nodes hash to
// the same UID regardless of identity-field state.
func appendNodeCanonical(buf []byte, n *Node) []byte {
	buf = append(buf, '{')

	// cache: *bool, omitempty.
	if n.Cache != nil {
		buf = append(buf, `"cache":`...)
		if *n.Cache {
			buf = append(buf, 't', 'r', 'u', 'e')
		} else {
			buf = append(buf, 'f', 'a', 'l', 's', 'e')
		}
		buf = append(buf, ',')
	}

	// cmds: []Cmd
	buf = append(buf, `"cmds":`...)
	buf = appendCmdSliceCanonical(buf, n.Cmds)
	buf = append(buf, ',')

	// deps: []string
	buf = append(buf, `"deps":`...)
	buf = appendStringSliceCanonical(buf, n.Deps)
	buf = append(buf, ',')

	// env: map[string]string
	buf = append(buf, `"env":`...)
	buf = appendStringMapCanonical(buf, n.Env)
	buf = append(buf, ',')

	// foreign_deps: map[string][]string, omitempty
	if len(n.ForeignDeps) > 0 {
		buf = append(buf, `"foreign_deps":`...)
		buf = appendStringSliceMapCanonical(buf, n.ForeignDeps)
		buf = append(buf, ',')
	}

	// host_platform: bool, omitempty (only emitted when true)
	if n.HostPlatform {
		buf = append(buf, `"host_platform":true,`...)
	}

	// inputs: []VFS
	buf = append(buf, `"inputs":`...)
	buf = appendVFSSliceCanonical(buf, n.Inputs)
	buf = append(buf, ',')

	// kv: map[string]string
	buf = append(buf, `"kv":`...)
	buf = appendStringMapCanonical(buf, n.KV)
	buf = append(buf, ',')

	// outputs: []VFS
	buf = append(buf, `"outputs":`...)
	buf = appendVFSSliceCanonical(buf, n.Outputs)
	buf = append(buf, ',')

	// platform: string
	buf = append(buf, `"platform":`...)
	buf = appendString(buf, n.Platform)
	buf = append(buf, ',')

	// requirements: map[string]interface{}
	buf = append(buf, `"requirements":`...)
	buf = appendInterfaceMapCanonical(buf, n.Requirements)
	buf = append(buf, ',')

	// sandboxing: bool — always present in REF.
	if n.Sandboxing {
		buf = append(buf, `"sandboxing":true,`...)
	} else {
		buf = append(buf, `"sandboxing":false,`...)
	}

	// self_uid omitted — identity field, would make the hash depend on
	// itself transitively. uid likewise.

	// tags: []string
	buf = append(buf, `"tags":`...)
	buf = appendStringSliceCanonical(buf, n.Tags)
	buf = append(buf, ',')

	// target_properties: map[string]string
	buf = append(buf, `"target_properties":`...)
	buf = appendStringMapCanonical(buf, n.TargetProperties)

	buf = append(buf, '}')

	return buf
}

// appendCmdSliceCanonical emits []Cmd in compact alphabetical form.
func appendCmdSliceCanonical(buf []byte, cmds []Cmd) []byte {
	if len(cmds) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[')

	for i, c := range cmds {
		buf = append(buf, '{')

		// cmd_args
		buf = append(buf, `"cmd_args":`...)
		buf = appendStringSliceCanonical(buf, c.CmdArgs)
		buf = append(buf, ',')

		// cwd (omitempty)
		if c.Cwd != "" {
			buf = append(buf, `"cwd":`...)
			buf = appendString(buf, c.Cwd)
			buf = append(buf, ',')
		}

		// env
		buf = append(buf, `"env":`...)
		buf = appendStringMapCanonical(buf, c.Env)

		// stdout (omitempty)
		if c.Stdout != "" {
			buf = append(buf, ',', '"', 's', 't', 'd', 'o', 'u', 't', '"', ':')
			buf = appendString(buf, c.Stdout)
		}

		buf = append(buf, '}')

		if i < len(cmds)-1 {
			buf = append(buf, ',')
		}
	}

	buf = append(buf, ']')

	return buf
}

// appendStringSliceCanonical emits []string in compact form.
func appendStringSliceCanonical(buf []byte, ss []string) []byte {
	if len(ss) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[')

	for i, s := range ss {
		buf = appendString(buf, s)

		if i < len(ss)-1 {
			buf = append(buf, ',')
		}
	}

	buf = append(buf, ']')

	return buf
}

// appendVFSSliceCanonical emits []VFS in compact form, using the
// inline-prefix form (no v.String() concat) per appendVFS.
func appendVFSSliceCanonical(buf []byte, vs []VFS) []byte {
	if len(vs) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[')

	for i, v := range vs {
		buf = appendVFS(buf, v)

		if i < len(vs)-1 {
			buf = append(buf, ',')
		}
	}

	buf = append(buf, ']')

	return buf
}

// appendStringMapCanonical emits map[string]string with keys sorted,
// compact form.
func appendStringMapCanonical(buf []byte, m map[string]string) []byte {
	if len(m) == 0 {
		return append(buf, '{', '}')
	}

	keys := canonKeysOf(m)

	buf = append(buf, '{')

	for i, k := range keys {
		buf = appendString(buf, k)
		buf = append(buf, ':')
		buf = appendString(buf, m[k])

		if i < len(keys)-1 {
			buf = append(buf, ',')
		}
	}

	buf = append(buf, '}')

	return buf
}

// appendStringSliceMapCanonical emits map[string][]string with keys
// sorted, compact form.
func appendStringSliceMapCanonical(buf []byte, m map[string][]string) []byte {
	if len(m) == 0 {
		return append(buf, '{', '}')
	}

	keys := canonKeysOf(m)

	buf = append(buf, '{')

	for i, k := range keys {
		buf = appendString(buf, k)
		buf = append(buf, ':')
		buf = appendStringSliceCanonical(buf, m[k])

		if i < len(keys)-1 {
			buf = append(buf, ',')
		}
	}

	buf = append(buf, '}')

	return buf
}

// appendInterfaceMapCanonical emits map[string]interface{} for the
// Requirements field. Same value-type contract as
// gjson_write.appendInterfaceMap.
func appendInterfaceMapCanonical(buf []byte, m map[string]interface{}) []byte {
	if len(m) == 0 {
		return append(buf, '{', '}')
	}

	keys := canonKeysOf(m)

	buf = append(buf, '{')

	for i, k := range keys {
		buf = appendString(buf, k)
		buf = append(buf, ':')

		switch v := m[k].(type) {
		case string:
			buf = appendString(buf, v)
		case float64:
			buf = strconv.AppendFloat(buf, v, 'f', -1, 64)
		case bool:
			if v {
				buf = append(buf, 't', 'r', 'u', 'e')
			} else {
				buf = append(buf, 'f', 'a', 'l', 's', 'e')
			}
		default:
			ThrowFmt("appendInterfaceMapCanonical: unsupported value type %T for key %q", v, k)
		}

		if i < len(keys)-1 {
			buf = append(buf, ',')
		}
	}

	buf = append(buf, '}')

	return buf
}

// canonKeysOf extracts and sorts a map's keys. Generic-typed because
// the three map shapes we emit (map[string]string, map[string][]string,
// map[string]interface{}) share the keys-only contract.
func canonKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
