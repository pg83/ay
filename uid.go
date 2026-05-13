package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"sort"
)

// uid.go — content-derived UID hashing.
//
// UID = base64url(sha1(canonical-node-bytes))[:22]. The 22-character
// length is verified against the on-disk reference (every uid/self_uid
// in /home/pg/monorepo/yatool_orig/sg.json is exactly 22 characters
// of base64url alphabet).
//
// The "canonical node bytes" are an internal binary format — NOT JSON,
// NOT human-readable. The only contract is that the resulting sha1
// hash is stable per (semantic) node content. Production path:
// `nodeUID(n)` streams the fields directly into a `sha1.Hash` with
// zero intermediate buffers; only the 22-byte base64 string is
// allocated per node.
//
// Field encoding is purely positional: every Node field is written in
// the same fixed order (alphabetical, matching node.go's declaration)
// so no field-name tags are needed. Variable-length items (strings,
// slices, maps) are length-prefixed with a 4-byte little-endian count
// so adjacent items cannot bleed into each other regardless of
// content. UID, SelfUID, and StatsUID are excluded so the hash
// depends only on content, not identity.

const uidLength = 22

// computeUID returns the 22-character base64url SHA-1 of the input
// bytes. Generic helper retained for tests that hand in their own
// canonical bytes; the production emitter uses nodeUID directly.
func computeUID(canonicalBytes []byte) string {
	sum := sha1.Sum(canonicalBytes)

	return base64.RawURLEncoding.EncodeToString(sum[:])[:uidLength]
}

// canonicalNodeBytes produces the byte sequence we hash to derive a
// node's UID. Used by tests; production callers should use nodeUID
// which skips the buffer round-trip.
func canonicalNodeBytes(n *Node) []byte {
	var buf bytes.Buffer
	writeNode(&buf, n)

	return buf.Bytes()
}

// nodeUID derives a Node's content-UID by streaming its fields
// directly into a fresh sha1.Hash. The digest is an ~88-byte heap
// allocation — across an M3 emit run (~6000 nodes ≈ 0.5 MB total) it
// is GC-invisible and cheaper than the equivalent sync.Pool dance.
func nodeUID(n *Node) string {
	h := sha1.New()
	writeNode(h, n)

	var sumBuf [sha1.Size]byte
	sum := h.Sum(sumBuf[:0])

	return base64.RawURLEncoding.EncodeToString(sum)[:uidLength]
}

// writeNode streams the canonical form of n into w. Both sha1.Hash
// (production) and *bytes.Buffer (tests) satisfy io.Writer; w.Write
// errors are not possible for either backend.
//
// Field order matches `node.go` (alphabetical: cache, cmds, deps, env,
// foreign_deps, host_platform, inputs, kv, outputs, platform,
// requirements, sandboxing, tags, target_properties). self_uid, uid,
// and stats_uid are excluded by design.
//
// `omitempty` fields stream their "absent" form (Cache=nil → 0x00,
// HostPlatform=false → 0x00, ForeignDeps=nil → count 0). The shape is
// always present so position remains unambiguous.
func writeNode(w io.Writer, n *Node) {
	// cache: *bool. 0=nil, 1=false, 2=true.
	switch {
	case n.Cache == nil:
		writeByte(w, 0)
	case *n.Cache:
		writeByte(w, 2)
	default:
		writeByte(w, 1)
	}

	writeCmdSlice(w, n.Cmds)
	writeStringSlice(w, n.Deps)
	writeStringMap(w, n.Env)
	writeStringSliceMap(w, n.ForeignDeps)
	writeBool(w, n.HostPlatform)
	writeVFSSlice(w, n.Inputs)
	writeStringMap(w, n.KV)
	writeVFSSlice(w, n.Outputs)
	writeBytes(w, n.Platform)
	writeInterfaceMap(w, n.Requirements)
	writeBool(w, n.Sandboxing)
	writeStringSlice(w, n.Tags)
	writeStringMap(w, n.TargetProperties)
}

// writeBytes writes a 4-byte LE length prefix followed by the raw
// bytes of s. The length prefix is what makes adjacent variable-
// length items unambiguously decodable — two distinct concatenations
// of strings can only collide if their length prefixes already match.
func writeBytes(w io.Writer, s string) {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(s)))
	_, _ = w.Write(lenBuf[:])
	_, _ = w.Write([]byte(s))
}

func writeCount(w io.Writer, n int) {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(n))
	_, _ = w.Write(lenBuf[:])
}

func writeByte(w io.Writer, b byte) {
	_, _ = w.Write([]byte{b})
}

func writeBool(w io.Writer, b bool) {
	if b {
		writeByte(w, 1)
	} else {
		writeByte(w, 0)
	}
}

func writeStringSlice(w io.Writer, ss []string) {
	writeCount(w, len(ss))
	for _, s := range ss {
		writeBytes(w, s)
	}
}

func writeVFSSlice(w io.Writer, vs []VFS) {
	writeCount(w, len(vs))
	for _, v := range vs {
		writeByte(w, byte(v.Root))
		writeBytes(w, v.Rel)
	}
}

func writeStringMap(w io.Writer, m map[string]string) {
	writeCount(w, len(m))
	for _, k := range canonKeysOf(m) {
		writeBytes(w, k)
		writeBytes(w, m[k])
	}
}

func writeStringSliceMap(w io.Writer, m map[string][]string) {
	writeCount(w, len(m))
	for _, k := range canonKeysOf(m) {
		writeBytes(w, k)
		writeStringSlice(w, m[k])
	}
}

func writeInterfaceMap(w io.Writer, m map[string]interface{}) {
	writeCount(w, len(m))
	for _, k := range canonKeysOf(m) {
		writeBytes(w, k)
		switch v := m[k].(type) {
		case string:
			writeByte(w, 's')
			writeBytes(w, v)
		case float64:
			writeByte(w, 'f')
			var fBuf [8]byte
			binary.LittleEndian.PutUint64(fBuf[:], math.Float64bits(v))
			_, _ = w.Write(fBuf[:])
		case bool:
			writeByte(w, 'b')
			writeBool(w, v)
		default:
			ThrowFmt("writeInterfaceMap: unsupported value type %T for key %q", v, k)
		}
	}
}

func writeCmdSlice(w io.Writer, cmds []Cmd) {
	writeCount(w, len(cmds))
	for _, c := range cmds {
		writeStringSlice(w, c.CmdArgs)
		writeBytes(w, c.Cwd)
		writeStringMap(w, c.Env)
		writeBytes(w, c.Stdout)
	}
}

// canonKeysOf extracts and sorts a map's keys. Shared by every
// map-encoding helper.
func canonKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
