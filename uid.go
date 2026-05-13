package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
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
// hash is stable per (semantic) node content.
//
// The canonical form is accumulated into a `canonBuf` (concrete type,
// not an interface). Concrete-typed `(*canonBuf).writeXxx` methods
// inline as direct `append(c.buf, ...)` calls — escape analysis keeps
// any per-call scratch arrays on the stack, and the only heap
// allocation per nodeUID call is the `canonBuf.buf` slice itself
// (grown as needed). Going through `hash.Hash` / `io.Writer` interface
// receivers would force each tiny write through dynamic dispatch and
// box the slice header onto the heap — that was the 25% alloc cost
// flagged in the post-canon-bytes-binary profile.
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
// node's UID. Used by tests; production callers should use nodeUID.
func canonicalNodeBytes(n *Node) []byte {
	var c canonBuf
	c.writeNode(n)

	return c.buf
}

// nodeUID derives a Node's content-UID by accumulating the canonical
// form into a `canonBuf` and feeding the whole buffer to sha1 in one
// shot. Concrete-typed receiver avoids the per-write interface boxing
// that `hash.Hash`-streaming carried.
func nodeUID(n *Node) string {
	var c canonBuf
	c.writeNode(n)

	sum := sha1.Sum(c.buf)

	return base64.RawURLEncoding.EncodeToString(sum[:])[:uidLength]
}

// canonBuf is a writable byte accumulator with concrete-typed
// methods. All writeXxx methods compile to direct append calls — no
// interface dispatch, no per-call scratch escaping.
type canonBuf struct {
	buf []byte
}

func (c *canonBuf) writeByte(b byte) {
	c.buf = append(c.buf, b)
}

func (c *canonBuf) writeUint32(n uint32) {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], n)
	c.buf = append(c.buf, lenBuf[:]...)
}

func (c *canonBuf) writeBool(b bool) {
	if b {
		c.buf = append(c.buf, 1)
	} else {
		c.buf = append(c.buf, 0)
	}
}

func (c *canonBuf) writeBytes(s string) {
	c.writeUint32(uint32(len(s)))
	c.buf = append(c.buf, s...)
}

func (c *canonBuf) writeStringSlice(ss []string) {
	c.writeUint32(uint32(len(ss)))
	for _, s := range ss {
		c.writeBytes(s)
	}
}

func (c *canonBuf) writeVFSSlice(vs []VFS) {
	c.writeUint32(uint32(len(vs)))
	for _, v := range vs {
		c.writeByte(byte(v.Root))
		c.writeBytes(v.Rel)
	}
}

func (c *canonBuf) writeStringMap(m map[string]string) {
	c.writeUint32(uint32(len(m)))
	for _, k := range canonKeysOf(m) {
		c.writeBytes(k)
		c.writeBytes(m[k])
	}
}

func (c *canonBuf) writeStringSliceMap(m map[string][]string) {
	c.writeUint32(uint32(len(m)))
	for _, k := range canonKeysOf(m) {
		c.writeBytes(k)
		c.writeStringSlice(m[k])
	}
}

func (c *canonBuf) writeInterfaceMap(m map[string]interface{}) {
	c.writeUint32(uint32(len(m)))
	for _, k := range canonKeysOf(m) {
		c.writeBytes(k)
		switch v := m[k].(type) {
		case string:
			c.writeByte('s')
			c.writeBytes(v)
		case float64:
			c.writeByte('f')
			var fBuf [8]byte
			binary.LittleEndian.PutUint64(fBuf[:], math.Float64bits(v))
			c.buf = append(c.buf, fBuf[:]...)
		case bool:
			c.writeByte('b')
			c.writeBool(v)
		default:
			ThrowFmt("canonBuf.writeInterfaceMap: unsupported value type %T for key %q", v, k)
		}
	}
}

func (c *canonBuf) writeCmdSlice(cmds []Cmd) {
	c.writeUint32(uint32(len(cmds)))
	for _, cm := range cmds {
		c.writeStringSlice(cm.CmdArgs)
		c.writeBytes(cm.Cwd)
		c.writeStringMap(cm.Env)
		c.writeBytes(cm.Stdout)
	}
}

// writeNode emits the canonical form of n into c.
//
// Field order matches `node.go` (alphabetical: cache, cmds, deps, env,
// foreign_deps, host_platform, inputs, kv, outputs, platform,
// requirements, sandboxing, tags, target_properties). self_uid, uid,
// and stats_uid are excluded by design.
//
// `omitempty` fields stream their "absent" form (Cache=nil → 0x00,
// HostPlatform=false → 0x00, ForeignDeps=nil → count 0). Position is
// always present-shape so encoding remains unambiguous.
func (c *canonBuf) writeNode(n *Node) {
	switch {
	case n.Cache == nil:
		c.writeByte(0)
	case *n.Cache:
		c.writeByte(2)
	default:
		c.writeByte(1)
	}

	c.writeCmdSlice(n.Cmds)
	c.writeStringSlice(n.Deps)
	c.writeStringMap(n.Env)
	c.writeStringSliceMap(n.ForeignDeps)
	c.writeBool(n.HostPlatform)
	c.writeVFSSlice(n.Inputs)
	c.writeStringMap(n.KV)
	c.writeVFSSlice(n.Outputs)
	c.writeBytes(n.Platform)
	c.writeInterfaceMap(n.Requirements)
	c.writeBool(n.Sandboxing)
	c.writeStringSlice(n.Tags)
	c.writeStringMap(n.TargetProperties)
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
