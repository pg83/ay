package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	encHex "encoding/hex"
	"math"
	"sort"
	"strings"

	"github.com/zeebo/xxh3"
)

// uid.go — content-derived UID hashing.
//
// UID = base64url(xxh3-128(canonical-node-bytes))[:22]. Canonical bytes are an
// internal binary format (not JSON); the only contract is hash stability per
// semantic node content — the hash is never compared to an external value (the
// L4 normalizer recomputes/strips uids), so a fast non-cryptographic 128-bit
// hash replaces SHA-1: a 16-byte digest base64url-encodes to exactly 22 chars.
//
// Field encoding is positional in alphabetical order (matching node.go);
// variable-length items are 4-byte little-endian length-prefixed. UID,
// SelfUID, StatsUID are excluded so the hash depends only on content.

const uidLength = 22

// computeUID returns the 22-character base64url xxh3-128 of the input
// bytes. Generic helper retained for tests that hand in their own
// canonical bytes; the production emitter uses nodeUID directly.
func computeUID(canonicalBytes []byte) string {
	sum := xxh3.Hash128(canonicalBytes).Bytes()

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
// form into a `canonBuf` and feeding the whole buffer to xxh3 in one
// shot. Concrete-typed receiver avoids the per-write interface boxing
// that `hash.Hash`-streaming carried.
func nodeUID(n *Node) string {
	var c canonBuf

	return nodeUIDWithBuf(n, &c)
}

// nodeUIDWithBuf is nodeUID with caller-owned scratch storage. Finalize
// hashes thousands of nodes; reusing the canonicalization buffer avoids
// repeatedly allocating large transient byte slices for wide input lists.
func nodeUIDWithBuf(n *Node, c *canonBuf) string {
	c.buf = c.buf[:0]
	c.writeNode(n)

	sum := xxh3.Hash128(c.buf).Bytes()

	return base64.RawURLEncoding.EncodeToString(sum[:])[:uidLength]
}

// nodeStatsUID mirrors upstream raw-graph stats_uid production:
// md5(str([platform, str(sorted(tags)), kv.p, str(sorted(outputs))])).
func nodeStatsUID(n *Node) string {
	sum := md5.Sum([]byte(statsUIDPreimage(n)))

	return encHex.EncodeToString(sum[:])
}

func statsUIDPreimage(n *Node) string {
	kind, _ := n.KV["p"].(string)

	return pythonStringListRepr([]string{
		n.Platform,
		pythonStringListRepr(sortedStatsTags(n)),
		kind,
		pythonStringListRepr(sortedLongOutputs(n.Outputs)),
	})
}

func sortedStatsTags(n *Node) []string {
	tags := n.Tags
	if n.StatsTags != nil {
		tags = n.StatsTags
	}

	out := append([]string(nil), tags...)
	sort.Strings(out)

	return out
}

func sortedLongOutputs(outputs []VFS) []string {
	out := make([]string, len(outputs))
	for i, v := range outputs {
		out[i] = v.LongString()
	}
	sort.Strings(out)

	return out
}

func pythonStringListRepr(items []string) string {
	if len(items) == 0 {
		return "[]"
	}

	var b strings.Builder
	b.Grow(len(items) * 8)
	b.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(pythonStringRepr(item))
	}
	b.WriteByte(']')

	return b.String()
}

func pythonStringRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}

	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if ch == quote {
				b.WriteByte('\\')
				b.WriteByte(ch)
				continue
			}
			if ch < 0x20 || ch == 0x7f {
				const hexDigits = "0123456789abcdef"
				b.WriteString(`\x`)
				b.WriteByte(hexDigits[ch>>4])
				b.WriteByte(hexDigits[ch&0x0f])
				continue
			}
			b.WriteByte(ch)
		}
	}
	b.WriteByte(quote)

	return b.String()
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
		c.writeByte(byte(v.Root()))
		c.writeBytes(v.Rel())
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

func (c *canonBuf) writeKVMap(m map[string]interface{}) {
	c.writeUint32(uint32(len(m)))
	for _, k := range canonKeysOf(m) {
		c.writeBytes(k)
		switch v := m[k].(type) {
		case string:
			c.writeBytes(v)
		case bool:
			if v {
				c.writeBytes("true")
			} else {
				c.writeBytes("false")
			}
		default:
			ThrowFmt("canonBuf.writeKVMap: unsupported value type %T for key %q", v, k)
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

// writeNode emits the canonical form of n into c. Field order matches
// node.go (alphabetical: cache, cmds, deps, env, foreign_deps, inputs,
// kv, outputs, platform, requirements, sandboxing, tags,
// target_properties); self_uid/uid/stats_uid are excluded. host-vs-target
// discrimination flows through Tags ("tool" baseline on host; empty on
// target). `omitempty` fields stream their "absent" form (Cache=nil →
// 0x00, ForeignDeps=nil → count 0); position is always present.
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
	c.writeVFSSlice(n.Inputs)
	c.writeKVMap(n.KV)
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
