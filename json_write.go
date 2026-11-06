package main

import (
	"encoding/binary"
	"io"
	"strconv"
	"unicode/utf8"
	"unsafe"
)

var (
	vfsEscapedJSON vfsEscapeCache
	vfsCleanJSON   BitSet
	strJSONState   TwoBitSet
)

var htmlSafeNoEscape = func() [128]bool {
	var t [128]bool

	for b := 0; b < 128; b++ {
		switch {
		case b < 0x20:
			t[b] = false
		case b == '"' || b == '\\':
			t[b] = false
		default:
			t[b] = true
		}
	}

	return t
}()

const (
	strJSONClean        = 1
	strJSONEscaped      = 2
	vfsEscapeBlockShift = 20
	hex                 = "0123456789abcdef"
	jsonCachePageBits   = 20
	jsonCachePage       = 1 << jsonCachePageBits
	jsonCacheSeenBits   = 25
)

func appendSTRComma(buf []byte, id STR) []byte {
	s := id.string()

	switch strJSONState.get(uint32(id)) {
	case strJSONClean:
		buf = append(buf, '"')
		buf = append(buf, s...)

		return append(buf, '"', ',')
	case strJSONEscaped:
		return append(appendString(buf, s), ',')
	}

	start := len(buf)

	buf = appendString(buf, s)

	if len(buf)-start == len(s)+2 {
		strJSONState.set(uint32(id), strJSONClean)
	} else {
		strJSONState.set(uint32(id), strJSONEscaped)
	}

	return append(buf, ',')
}

type vfsEscapeCache struct {
	pos    []uint32
	blocks [][]byte
}

func (c *vfsEscapeCache) get(id uint32) []byte {
	if int(id) >= len(c.pos) || c.pos[id] == 0 {
		return nil
	}

	p := c.pos[id] - 1
	b := c.blocks[p>>vfsEscapeBlockShift][p&(1<<vfsEscapeBlockShift-1):]
	n, k := binary.Uvarint(b)

	return b[k : k+int(n)]
}

func (c *vfsEscapeCache) put(id uint32, entry []byte) {
	if int(id) >= len(c.pos) {
		grown := make([]uint32, vfsBound())

		copy(grown, c.pos)
		c.pos = grown
	}

	need := binary.MaxVarintLen32 + len(entry)

	if len(c.blocks) == 0 || cap(c.blocks[len(c.blocks)-1])-len(c.blocks[len(c.blocks)-1]) < need {
		c.blocks = append(c.blocks, make([]byte, 0, max(1<<vfsEscapeBlockShift, need)))
	}

	last := len(c.blocks) - 1
	block := c.blocks[last]

	c.pos[id] = uint32(last)<<vfsEscapeBlockShift | uint32(len(block)) + 1
	block = binary.AppendUvarint(block, uint64(len(entry)))
	c.blocks[last] = append(block, entry...)
}

func appendRefNum(buf []byte, r NodeRef) []byte {
	return strconv.AppendUint(buf, uint64(r), 10)
}

func appendRefNumsSeq(buf []byte, seq func(func(NodeRef) bool)) []byte {
	buf = append(buf, '[')

	first := true

	seq(func(r NodeRef) bool {
		if !first {
			buf = append(buf, ',')
		}

		first = false
		buf = appendRefNum(buf, r)

		return true
	})

	return append(buf, ']')
}

func appendRefNumSlice(buf []byte, refs []NodeRef) []byte {
	buf = append(buf, '[')

	for i, r := range refs {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendRefNum(buf, r)
	}

	return append(buf, ']')
}

type jsonSliceKey struct {
	first unsafe.Pointer
	n     int
}

type jsonWriteCache struct {
	seen  []uint64
	spans map[jsonSliceKey]uint64
	pages [][]byte
}

func newJSONWriteCache() *jsonWriteCache {
	return &jsonWriteCache{seen: make([]uint64, 1<<jsonCacheSeenBits/64), spans: map[jsonSliceKey]uint64{}}
}

func (c *jsonWriteCache) cached(buf []byte, key jsonSliceKey) ([]byte, bool) {
	span, ok := c.spans[key]

	if !ok {
		return buf, false
	}

	page, off, n := span>>(2*jsonCachePageBits), span>>jsonCachePageBits&(jsonCachePage-1), span&(jsonCachePage-1)

	return append(buf, c.pages[page][off:off+n]...), true
}

func (c *jsonWriteCache) remember(key jsonSliceKey, serialized []byte) {
	h := uint64(uintptr(key.first))*0x9e3779b97f4a7c15 ^ uint64(key.n)*0xc2b2ae3d27d4eb4f
	fresh := false

	for i := range 3 {
		bit := h >> (64 - jsonCacheSeenBits)
		word, mask := bit/64, uint64(1)<<(bit%64)

		if c.seen[word]&mask == 0 {
			c.seen[word] |= mask
			fresh = true
		}

		h = h*0xff51afd7ed558ccd + uint64(i) + 1
	}

	if fresh || len(serialized) >= jsonCachePage {
		return
	}

	last := len(c.pages) - 1

	if last < 0 || len(c.pages[last])+len(serialized) > jsonCachePage {
		c.pages = append(c.pages, make([]byte, 0, jsonCachePage))
		last++
	}

	off := len(c.pages[last])

	c.pages[last] = append(c.pages[last], serialized...)
	c.spans[key] = uint64(last)<<(2*jsonCachePageBits) | uint64(off)<<jsonCachePageBits | uint64(len(serialized))
}

func writeGraphCompact(w io.Writer, g *Graph, dropSrcInputs bool) {
	buf := make([]byte, 0, 1<<20)
	cache := newJSONWriteCache()

	buf = append(buf, `{"graph":[`...)

	first := true

	for _, node := range g.Graph {
		if !first {
			buf = append(buf, ',')
		}

		first = false
		buf = appendNode(buf, node, node.Inputs, g.fetchRefs, dropSrcInputs, cache)

		if len(buf) >= 1<<20 {
			throw2(w.Write(buf))
			buf = buf[:0]
		}
	}

	buf = append(buf, `],"inputs":{},"result":`...)
	buf = appendRefNumSlice(buf, g.Result)
	buf = append(buf, '}')

	throw2(w.Write(buf))
}

func appendNode(buf []byte, n *Node, inputs InputChunks, fetchRefs *DenseMap[STR, NodeRef], dropSrcInputs bool, cache *jsonWriteCache) []byte {
	buf = append(buf, '{')

	if n.disableCache() || n.KV.DisableCache {
		buf = append(buf, `"cache":false,`...)
	}

	buf = append(buf, `"cmds":`...)
	buf = appendCmdSlice(buf, n.Cmds, cache)
	buf = append(buf, `,"deps":`...)
	buf = appendRefNumsSeq(buf, n.buildDeps(fetchRefs))
	buf = append(buf, `,"env":`...)
	buf = cache.appendEnv(buf, n.Env)

	if len(n.ForeignDepRefs) > 0 {
		buf = append(buf, `,"foreign_deps":{"tool":`...)
		buf = appendRefNumSlice(buf, n.ForeignDepRefs)
		buf = append(buf, '}')
	}

	buf = append(buf, `,"inputs":`...)

	if dropSrcInputs {
		buf = appendBuildOnlyVFSChunks(buf, inputs)
	} else {
		buf = appendVFSChunks(buf, inputs)
	}

	if len(n.inputNames()) > 0 {
		buf = buf[:len(buf)-1]

		for _, name := range n.inputNames() {
			if buf[len(buf)-1] != '[' {
				buf = append(buf, ',')
			}

			buf = appendString(buf, name.string())
		}

		buf = append(buf, ']')
	}

	buf = append(buf, `,"kv":`...)
	buf = appendKV(buf, *n.KV, n.Extra)
	buf = append(buf, `,"outputs":`...)
	buf = appendVFSSlice(buf, n.Outputs)

	if n.Platform.Target != "" {
		buf = append(buf, `,"platform":`...)
		buf = appendString(buf, string(n.Platform.Target))
	}

	buf = append(buf, `,"requirements":`...)
	buf = appendRequirements(buf, n.Requirements)

	if len(n.resourceURIs()) > 0 {
		buf = append(buf, `,"resources":[`...)

		for i, uri := range n.resourceURIs() {
			if i > 0 {
				buf = append(buf, ',')
			}

			buf = append(buf, `{"uri":`...)
			buf = appendString(buf, uri.string())
			buf = append(buf, '}')
		}

		buf = append(buf, ']')
	}

	buf = append(buf, `,"self_uid":`...)
	buf = appendRefNum(buf, n.Ref)
	buf = append(buf, `,"uid":`...)
	buf = appendRefNum(buf, n.Ref)

	return append(buf, '}')
}

func appendCmdSlice(buf []byte, cmds []Cmd, cache *jsonWriteCache) []byte {
	buf = append(buf, '[')

	for i, c := range cmds {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = append(buf, `{"cmd_args":`...)
		buf = cache.appendStrChunks(buf, c.CmdArgs)

		if c.Cwd != 0 {
			buf = append(buf, `,"cwd":`...)

			if v := c.Cwd.vfs(); v != 0 {
				buf = appendVFS(buf, v)
			} else {
				buf = appendString(buf, c.Cwd.str().string())
			}
		}

		if len(c.Env) > 0 {
			buf = append(buf, `,"env":`...)
			buf = cache.appendEnv(buf, c.Env)
		}

		if c.Stdout != 0 {
			buf = append(buf, `,"stdout":`...)
			buf = appendVFS(buf, c.Stdout)
		}

		buf = append(buf, '}')
	}

	return append(buf, ']')
}

func (c *jsonWriteCache) appendStrChunks(buf []byte, chunks ArgChunks) []byte {
	buf = append(buf, '[')

	for _, ch := range chunks {
		if len(ch) == 0 {
			continue
		}

		key := jsonSliceKey{unsafe.Pointer(&ch[0]), len(ch)}

		if next, ok := c.cached(buf, key); ok {
			buf = next

			continue
		}

		start := len(buf)

		for _, a := range ch {
			if v := a.vfs(); v != 0 {
				buf = appendVFSComma(buf, v)
			} else {
				buf = appendSTRComma(buf, a.str())
			}
		}

		c.remember(key, buf[start:])
	}

	return appendArrayEnd(buf)
}

func appendVFSChunks(buf []byte, chunks [][]VFS) []byte {
	buf = append(buf, '[')

	for _, ch := range chunks {
		for _, v := range ch {
			buf = appendVFSComma(buf, v)
		}
	}

	return appendArrayEnd(buf)
}

func appendBuildOnlyVFSChunks(buf []byte, chunks [][]VFS) []byte {
	buf = append(buf, '[')

	for _, ch := range chunks {
		for _, v := range ch {
			if v.isSource() {
				continue
			}

			buf = appendVFSComma(buf, v)
		}
	}

	return appendArrayEnd(buf)
}

func appendVFSSlice(buf []byte, vs []VFS) []byte {
	buf = append(buf, '[')

	for _, v := range vs {
		buf = appendVFSComma(buf, v)
	}

	return appendArrayEnd(buf)
}

func appendArrayEnd(buf []byte) []byte {
	if buf[len(buf)-1] == ',' {
		buf[len(buf)-1] = ']'

		return buf
	}

	return append(buf, ']')
}

func appendVFS(buf []byte, v VFS) []byte {
	buf = appendVFSComma(buf, v)

	return buf[:len(buf)-1]
}

func appendVFSComma(buf []byte, v VFS) []byte {
	id := v.strID()

	if vfsCleanJSON.has(id) {
		buf = append(buf, '"')
		buf = append(buf, v.prefix()...)
		buf = append(buf, v.relString()...)

		return append(buf, '"', ',')
	}

	if cached := vfsEscapedJSON.get(id); cached != nil {
		return append(buf, cached...)
	}

	return appendVFSUncached(buf, v, id)
}

func appendVFSUncached(buf []byte, v VFS, id uint32) []byte {
	s := v.relString()
	start := len(buf)

	buf = append(buf, '"')

	if s == "" {
		buf = append(buf, v.prefix()[:vfsPrefixLen-1]...)
	} else {
		buf = append(buf, v.prefix()...)
		buf = appendStringEscapedBody(buf, s)
	}

	buf = append(buf, '"', ',')

	if s != "" && len(buf)-start == len(s)+vfsPrefixLen+3 {
		vfsCleanJSON.add(id)
	} else {
		vfsEscapedJSON.put(id, buf[start:])
	}

	return buf
}

func appendString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	buf = appendStringEscapedBody(buf, s)
	buf = append(buf, '"')

	return buf
}

func appendStringEscapedBody(buf []byte, s string) []byte {
	start := 0

	for i := 0; i < len(s); {
		for len(s)-i >= 8 {
			word := binary.LittleEndian.Uint64(strBytes(s[i : i+8]))
			quote, slash := word^0x2222222222222222, word^0x5c5c5c5c5c5c5c5c
			special := (word - 0x2020202020202020) | word

			special |= (quote - 0x0101010101010101) &^ quote
			special |= (slash - 0x0101010101010101) &^ slash

			if special&0x8080808080808080 != 0 {
				break
			}

			i += 8
		}

		if i == len(s) {
			break
		}

		if b := s[i]; b < utf8.RuneSelf {
			if htmlSafeNoEscape[b] {
				i++

				continue
			}

			if start < i {
				buf = append(buf, s[start:i]...)
			}

			switch b {
			case '\\', '"':
				buf = append(buf, '\\', b)
			case '\b':
				buf = append(buf, '\\', 'b')
			case '\f':
				buf = append(buf, '\\', 'f')
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:

				buf = append(buf, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xf])
			}

			i++
			start = i

			continue
		}

		c, size := utf8.DecodeRuneInString(s[i:])

		if c == utf8.RuneError && size == 1 {
			if start < i {
				buf = append(buf, s[start:i]...)
			}

			buf = append(buf, '\\', 'u', 'f', 'f', 'f', 'd')
			i += size
			start = i

			continue
		}

		if c == ' ' || c == ' ' {
			if start < i {
				buf = append(buf, s[start:i]...)
			}

			buf = append(buf, '\\', 'u', '2', '0', '2', hex[c&0xf])
			i += size
			start = i

			continue
		}

		i += size
	}

	if start < len(s) {
		buf = append(buf, s[start:]...)
	}

	return buf
}
