package main

import (
	"crypto/md5"
	"encoding/binary"
	encHex "encoding/hex"
	"math"
	"sort"

	"github.com/zeebo/xxh3"
)

func computeUID(canonicalBytes []byte) UID {
	sum := xxh3.Hash128(canonicalBytes)

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

func canonicalNodeBytes(n *Node) []byte {
	var c canonBuf
	c.writeNode(n)

	return c.buf
}

func nodeUID(n *Node) UID {
	var c canonBuf

	return nodeUIDWithBuf(n, &c)
}

func nodeUIDWithBuf(n *Node, c *canonBuf) UID {
	c.buf = c.buf[:0]
	c.writeNode(n)

	sum := xxh3.Hash128(c.buf)

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

func nodeStatsUID(n *Node, c *canonBuf) string {
	c.strBuf = appendStatsPreimage(c.strBuf[:0], c, n)
	sum := md5.Sum(c.strBuf)

	return encHex.EncodeToString(sum[:])
}

// statsUIDPreimage returns the preimage as a string for test/diagnostic callers;
// the hot path (nodeStatsUID) md5s the bytes directly.
func statsUIDPreimage(n *Node, c *canonBuf) string {
	return string(appendStatsPreimage(c.strBuf[:0], c, n))
}

// appendStatsPreimage builds the 4-element stats preimage into dst, with the two
// nested list reprs built in c.strBuf2 and quoted into dst as bytes — equivalent
// to the old nested pythonStringListRepr but with no intermediate strings.
func appendStatsPreimage(dst []byte, c *canonBuf, n *Node) []byte {
	kind, _ := n.KV["p"].(string)

	dst = append(dst, '[')
	dst = appendPyRepr(dst, n.Platform)
	dst = append(dst, ',', ' ')
	c.strBuf2 = appendPythonListRepr(c.strBuf2[:0], sortedStatsTags(n))
	dst = appendPyRepr(dst, c.strBuf2)
	dst = append(dst, ',', ' ')
	dst = appendPyRepr(dst, kind)
	dst = append(dst, ',', ' ')
	c.strBuf2 = appendPythonListRepr(c.strBuf2[:0], sortedLongOutputs(n.Outputs))
	dst = appendPyRepr(dst, c.strBuf2)
	dst = append(dst, ']')

	return dst
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

// pythonStringListRepr returns the python-list repr as a string (test callers).
func pythonStringListRepr(c *canonBuf, items []string) string {
	return string(appendPythonListRepr(c.strBuf[:0], items))
}

func appendPythonListRepr(dst []byte, items []string) []byte {
	dst = append(dst, '[')

	for i, item := range items {
		if i > 0 {
			dst = append(dst, ',', ' ')
		}

		dst = appendPyRepr(dst, item)
	}

	return append(dst, ']')
}

// appendPyRepr appends the python repr of s (a string or a []byte) to dst.
func appendPyRepr[T ~string | ~[]byte](dst []byte, s T) []byte {
	hasSingle, hasDouble := false, false

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			hasSingle = true
		case '"':
			hasDouble = true
		}
	}

	quote := byte('\'')

	if hasSingle && !hasDouble {
		quote = '"'
	}

	dst = append(dst, quote)

	for i := 0; i < len(s); i++ {
		ch := s[i]

		switch ch {
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			if ch == quote {
				dst = append(dst, '\\', ch)
				continue
			}

			if ch < 0x20 || ch == 0x7f {
				const hexDigits = "0123456789abcdef"
				dst = append(dst, '\\', 'x', hexDigits[ch>>4], hexDigits[ch&0x0f])
				continue
			}

			dst = append(dst, ch)
		}
	}

	return append(dst, quote)
}

type canonBuf struct {
	buf []byte
	// fs, when set, makes writeVFSSlice mix each $(S) input's file-content hash
	// (xxh3, recorded by the FS on read) into the node hash, so a source edit
	// changes the node uid. Left nil where only the structural hash is wanted
	// (e.g. the dump/-G path, which is re-uid'd from canonical content anyway).
	fs FS

	// strBuf/strBuf2 are reused scratch for the stats-uid preimage: strBuf holds
	// the outer list (md5'd directly), strBuf2 the two nested list reprs quoted
	// into it — no intermediate strings.
	strBuf  []byte
	strBuf2 []byte
}

func (c *canonBuf) writeByte(b byte) {
	c.buf = append(c.buf, b)
}

func (c *canonBuf) writeUint32(n uint32) {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], n)
	c.buf = append(c.buf, lenBuf[:]...)
}

func (c *canonBuf) writeUint64(n uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], n)
	c.buf = append(c.buf, b[:]...)
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

func (c *canonBuf) writeUIDSlice(us []UID) {
	c.writeUint32(uint32(len(us)))

	for _, u := range us {
		c.writeUint64(u.Hi)
		c.writeUint64(u.Lo)
	}
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
		c.writeUint64(internTable.hashes[v.strID()])

		// Mix the content hash of source inputs so editing a $(S) file changes the
		// node uid. $(B) inputs are produced — their content is captured via the
		// producing node's uid in deps, not here. ContentHash faults if the file
		// was never read by the FS (the hash must have been recorded during gen).
		if c.fs != nil && v.IsSource() {
			c.writeUint64(c.fs.ContentHash(v))
		}
	}
}

func (c *canonBuf) writeStringMap(m map[string]string) {
	c.writeUint32(uint32(len(m)))

	for _, k := range canonKeysOf(m) {
		c.writeBytes(k)
		c.writeBytes(m[k])
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
	c.writeUIDSlice(n.Deps)
	c.writeStringMap(n.Env)
	c.writeUIDSlice(n.ForeignDeps)
	c.writeVFSSlice(n.Inputs)
	c.writeKVMap(n.KV)
	c.writeVFSSlice(n.Outputs)
	c.writeBytes(n.Platform)
	c.writeInterfaceMap(n.Requirements)
	c.writeBool(n.Sandboxing)
	c.writeStringSlice(n.Tags)
	c.writeStringMap(n.TargetProperties)
}

func canonKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
