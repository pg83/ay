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

const uidLength = 22

func computeUID(canonicalBytes []byte) string {
	sum := xxh3.Hash128(canonicalBytes).Bytes()

	return base64.RawURLEncoding.EncodeToString(sum[:])[:uidLength]
}

func canonicalNodeBytes(n *Node) []byte {
	var c canonBuf
	c.writeNode(n)

	return c.buf
}

func nodeUID(n *Node) string {
	var c canonBuf

	return nodeUIDWithBuf(n, &c)
}

func nodeUIDWithBuf(n *Node, c *canonBuf) string {
	c.buf = c.buf[:0]
	c.writeNode(n)

	sum := xxh3.Hash128(c.buf).Bytes()

	return base64.RawURLEncoding.EncodeToString(sum[:])[:uidLength]
}

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

func canonKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
