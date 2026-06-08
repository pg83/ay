package main

import (
	"io"
	"unicode/utf8"
)

// vfsEscapedJSON caches the JSON-encoded form (`"…"`, escape-body included)
// of each interned VFS string. writeGraph emits the same path many times —
// up to ~1.3M emits in sg5 with the new CP closures — and JSON escape was
// 27% of CPU until this cache went in. The intern table is append-only and
// strID() is stable, so a slice indexed by strID is safe; we grow it lazily
// to the current intern bound on the first miss past its length.
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

// writeGraph emits the graph as compact JSON (single line, no whitespace between
// tokens). The graph is consumed by `ay dump` / `ay make`, which parse it — the
// formatting carries no meaning, so we drop it. Nodes are flushed incrementally
// to keep the in-memory buffer bounded for multi-GB graphs.
func writeGraphCompact(w io.Writer, g *Graph) {
	buf := make([]byte, 0, 1<<20)

	buf = append(buf, `{"conf":`...)
	buf = appendGraphConf(buf, g.Conf)

	buf = append(buf, `,"graph":[`...)

	for i, node := range g.Graph {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendNode(buf, node, g.uids)

		if len(buf) >= 256<<10 {
			Throw2(w.Write(buf))
			buf = buf[:0]
		}
	}

	buf = append(buf, `],"inputs":{},"result":`...)
	buf = appendUIDSlice(buf, g.Result)
	buf = append(buf, '}')

	Throw2(w.Write(buf))
}

func appendNode(buf []byte, n *Node, uids *uidVec) []byte {
	buf = append(buf, '{')

	if n.Cache != nil {
		if *n.Cache {
			buf = append(buf, `"cache":true,`...)
		} else {
			buf = append(buf, `"cache":false,`...)
		}
	}

	buf = append(buf, `"cmds":`...)
	buf = appendCmdSlice(buf, n.Cmds)

	buf = append(buf, `,"deps":`...)
	buf = appendRefUIDs(buf, n.DepRefs, uids)

	buf = append(buf, `,"env":`...)
	buf = appendEnv(buf, n.Env)

	if len(n.ForeignDepRefs) > 0 {
		buf = append(buf, `,"foreign_deps":`...)
		buf = appendToolForeignDeps(buf, n.ForeignDepRefs, uids)
	}

	if nodeHasHostTag(n.Tags) {
		buf = append(buf, `,"host_platform":true`...)
	}

	buf = append(buf, `,"inputs":`...)
	buf = appendVFSSlice(buf, n.Inputs)

	buf = append(buf, `,"kv":`...)
	buf = appendKV(buf, n.KV)

	buf = append(buf, `,"outputs":`...)
	buf = appendVFSSlice(buf, n.Outputs)

	buf = append(buf, `,"platform":`...)
	buf = appendString(buf, platformTarget(n.Platform))

	buf = append(buf, `,"requirements":`...)
	buf = appendRequirements(buf, n.Requirements)

	if n.Sandboxing {
		buf = append(buf, `,"sandboxing":true`...)
	} else {
		buf = append(buf, `,"sandboxing":false`...)
	}

	buf = append(buf, `,"self_uid":`...)
	buf = appendUID(buf, n.SelfUID)

	buf = append(buf, `,"tags":`...)
	buf = appendStringSlice(buf, n.Tags)

	buf = append(buf, `,"target_properties":`...)
	buf = appendTargetProperties(buf, n.TargetProperties)

	buf = append(buf, `,"uid":`...)
	buf = appendUID(buf, n.UID)

	return append(buf, '}')
}

func appendCmdSlice(buf []byte, cmds []Cmd) []byte {
	buf = append(buf, '[')

	for i, c := range cmds {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = append(buf, `{"cmd_args":`...)
		buf = appendStrSlice(buf, c.CmdArgs)

		if c.Cwd != 0 {
			buf = append(buf, `,"cwd":`...)
			buf = appendString(buf, c.Cwd.String())
		}

		if len(c.Env) > 0 {
			buf = append(buf, `,"env":`...)
			buf = appendEnv(buf, c.Env)
		}

		if c.Stdout != "" {
			buf = append(buf, `,"stdout":`...)
			buf = appendString(buf, c.Stdout)
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

func appendStrSlice(buf []byte, as []STR) []byte {
	buf = append(buf, '[')

	for i, a := range as {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendString(buf, a.String())
	}

	return append(buf, ']')
}

// appendUID appends the quoted base64 uid directly into buf (the encode lands in a
// stack array inside UID.appendB64 — no per-uid string allocation).
func appendUID(buf []byte, u UID) []byte {
	buf = append(buf, '"')
	buf = u.appendB64(buf)

	return append(buf, '"')
}

func appendUIDSlice(buf []byte, us []UID) []byte {
	buf = append(buf, '[')

	for i, u := range us {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendUID(buf, u)
	}

	return append(buf, ']')
}

// appendRefUIDs writes refs as the array of their resolved dep uids — direct
// id->uid lookup, no materialized Deps slice on the node.
func appendRefUIDs(buf []byte, refs []NodeRef, uids *uidVec) []byte {
	buf = append(buf, '[')

	for i, r := range refs {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendUID(buf, uids.get(r))
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

func appendGraphConf(buf []byte, conf map[string]interface{}) []byte {
	if len(conf) == 0 {
		return append(buf, '{', '}')
	}

	resourcesAny, ok := conf["resources"]

	if !ok || len(conf) != 1 {
		ThrowFmt("writeGraph: unsupported conf shape")
	}

	resources, ok := resourcesAny.([]graphConfResource)

	if !ok {
		ThrowFmt("writeGraph: unsupported conf.resources type %T", resourcesAny)
	}

	buf = append(buf, `{"resources":`...)
	buf = appendGraphConfResources(buf, resources)

	return append(buf, '}')
}

func appendGraphConfResources(buf []byte, resources []graphConfResource) []byte {
	buf = append(buf, '[')

	for i, r := range resources {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendGraphConfResource(buf, r)
	}

	return append(buf, ']')
}

func appendGraphConfResource(buf []byte, r graphConfResource) []byte {
	buf = append(buf, '{')

	first := true
	sep := func() {
		if !first {
			buf = append(buf, ',')
		}

		first = false
	}

	if r.Name != "" {
		sep()
		buf = append(buf, `"name":`...)
		buf = appendString(buf, r.Name)
	}

	sep()
	buf = append(buf, `"pattern":`...)
	buf = appendString(buf, r.Pattern)

	if r.Resource != "" {
		sep()
		buf = append(buf, `"resource":`...)
		buf = appendString(buf, r.Resource)
	}

	if len(r.Resources) != 0 {
		sep()
		buf = append(buf, `"resources":`...)
		buf = appendGraphConfResourceURIs(buf, r.Resources)
	}

	return append(buf, '}')
}

func appendGraphConfResourceURIs(buf []byte, resources []graphConfResourceURI) []byte {
	buf = append(buf, '[')

	for i, r := range resources {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = append(buf, `{"platform":`...)
		buf = appendString(buf, r.Platform)
		buf = append(buf, `,"resource":`...)
		buf = appendString(buf, r.Resource)
		buf = append(buf, '}')
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
		nc := make([][]byte, internBound())
		copy(nc, vfsEscapedJSON)
		vfsEscapedJSON = nc
	}

	s := internTable.strs[id]
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	out = appendStringEscapedBody(out, s)
	out = append(out, '"')
	vfsEscapedJSON[id] = out
	return append(buf, out...)
}

// appendToolForeignDeps writes the foreign-dep slice as the single-key object
// {"tool":[...]} — the only key any node ever uses.
func appendToolForeignDeps(buf []byte, refs []NodeRef, uids *uidVec) []byte {
	buf = append(buf, `{"tool":`...)
	buf = appendRefUIDs(buf, refs, uids)

	return append(buf, '}')
}

const hex = "0123456789abcdef"

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
