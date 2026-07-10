package main

import (
	"io"
	"strconv"
	"unicode/utf8"
)

var vfsEscapedJSON [][]byte

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

const hex = "0123456789abcdef"

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

func writeGraphCompact(w io.Writer, g *Graph, dropSrcInputs bool) {
	buf := make([]byte, 0, 1<<20)

	buf = append(buf, `{"graph":[`...)

	first := true

	for _, node := range g.Graph {
		if node == nil {
			continue
		}

		if !first {
			buf = append(buf, ',')
		}

		first = false
		buf = appendNode(buf, node, g.fetchRefs, dropSrcInputs)

		if len(buf) >= 256<<10 {
			throw2(w.Write(buf))
			buf = buf[:0]
		}
	}

	buf = append(buf, `],"inputs":{},"result":`...)
	buf = appendRefNumSlice(buf, g.Result)
	buf = append(buf, '}')

	throw2(w.Write(buf))
}

func appendNode(buf []byte, n *Node, fetchRefs *DenseMap[STR, NodeRef], dropSrcInputs bool) []byte {
	buf = append(buf, '{')

	if n.KV.DisableCache {
		buf = append(buf, `"cache":false,`...)
	}

	buf = append(buf, `"cmds":`...)
	buf = appendCmdSlice(buf, n.Cmds)
	buf = append(buf, `,"deps":`...)
	buf = appendRefNumsSeq(buf, n.buildDeps(fetchRefs))
	buf = append(buf, `,"env":`...)
	buf = appendEnv(buf, n.Env)

	if len(n.ForeignDepRefs) > 0 {
		buf = append(buf, `,"foreign_deps":{"tool":`...)
		buf = appendRefNumSlice(buf, n.ForeignDepRefs)
		buf = append(buf, '}')
	}

	buf = append(buf, `,"inputs":`...)

	if dropSrcInputs {
		buf = appendBuildOnlyVFSChunks(buf, n.Inputs)
	} else {
		buf = appendVFSChunks(buf, n.Inputs)
	}

	buf = append(buf, `,"kv":`...)
	buf = appendKV(buf, *n.KV, n.KVExts)
	buf = append(buf, `,"outputs":`...)
	buf = appendVFSSlice(buf, n.Outputs)
	buf = append(buf, `,"platform":`...)
	buf = appendString(buf, string(n.Platform.Target))
	buf = append(buf, `,"requirements":`...)
	buf = appendRequirements(buf, n.Requirements)
	buf = append(buf, `,"self_uid":`...)
	buf = appendRefNum(buf, n.Ref)
	buf = append(buf, `,"uid":`...)
	buf = appendRefNum(buf, n.Ref)

	return append(buf, '}')
}

func appendCmdSlice(buf []byte, cmds []Cmd) []byte {
	buf = append(buf, '[')

	for i, c := range cmds {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = append(buf, `{"cmd_args":`...)
		buf = appendStrChunks(buf, c.CmdArgs)

		if c.Cwd != 0 {
			buf = append(buf, `,"cwd":`...)
			buf = appendVFS(buf, c.Cwd)
		}

		if len(c.Env) > 0 {
			buf = append(buf, `,"env":`...)
			buf = appendEnv(buf, c.Env)
		}

		if c.Stdout != 0 {
			buf = append(buf, `,"stdout":`...)
			buf = appendVFS(buf, c.Stdout)
		}

		buf = append(buf, '}')
	}

	return append(buf, ']')
}

func appendStringSlice(buf []byte, ss []string) []byte {
	buf = append(buf, '[')

	for i, s := range ss {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendString(buf, s)
	}

	return append(buf, ']')
}

func appendStrChunks(buf []byte, chunks ArgChunks) []byte {
	buf = append(buf, '[')

	first := true

	for _, ch := range chunks {
		for _, a := range ch {
			if !first {
				buf = append(buf, ',')
			}

			first = false

			if v := a.vfs(); v != 0 {
				buf = appendVFS(buf, v)
			} else {
				buf = appendString(buf, a.str().string())
			}
		}
	}

	return append(buf, ']')
}

func appendStrSlice(buf []byte, as []STR) []byte {
	buf = append(buf, '[')

	for i, a := range as {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendString(buf, a.string())
	}

	return append(buf, ']')
}

func appendVFSChunks(buf []byte, chunks [][]VFS) []byte {
	buf = append(buf, '[')

	first := true

	for _, ch := range chunks {
		for _, v := range ch {
			if !first {
				buf = append(buf, ',')
			}

			first = false
			buf = appendVFS(buf, v)
		}
	}

	return append(buf, ']')
}

func appendBuildOnlyVFSChunks(buf []byte, chunks [][]VFS) []byte {
	buf = append(buf, '[')

	first := true

	for _, ch := range chunks {
		for _, v := range ch {
			if v.isSource() {
				continue
			}

			if !first {
				buf = append(buf, ',')
			}

			first = false
			buf = appendVFS(buf, v)
		}
	}

	return append(buf, ']')
}

func appendVFSSlice(buf []byte, vs []VFS) []byte {
	buf = append(buf, '[')

	for i, v := range vs {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendVFS(buf, v)
	}

	return append(buf, ']')
}

func appendBuildOnlyVFSSlice(buf []byte, vs []VFS) []byte {
	buf = append(buf, '[')

	first := true

	for _, v := range vs {
		if v.isSource() {
			continue
		}

		if !first {
			buf = append(buf, ',')
		}

		first = false
		buf = appendVFS(buf, v)
	}

	return append(buf, ']')
}

func appendVFS(buf []byte, v VFS) []byte {
	id := v.strID()

	if int(id) < len(vfsEscapedJSON) {
		if cached := vfsEscapedJSON[id]; cached != nil {
			return append(buf, cached...)
		}
	} else {
		nc := make([][]byte, vfsBound())

		copy(nc, vfsEscapedJSON)
		vfsEscapedJSON = nc
	}

	s := v.relString()
	out := make([]byte, 0, len(s)+7)

	out = append(out, '"')

	if s == "" {
		out = append(out, v.prefix()[:vfsPrefixLen-1]...)
	} else {
		out = append(out, v.prefix()...)
		out = appendStringEscapedBody(out, s)
	}

	out = append(out, '"')
	vfsEscapedJSON[id] = out

	return append(buf, out...)
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
