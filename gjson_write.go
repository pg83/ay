package main

import (
	"io"
	"sort"
	"strconv"
	"unicode/utf8"
)

var (
	// vfsEscapedJSON caches the JSON-encoded form (`"…"`, escape-body included)
	// of each interned VFS string. writeGraph emits the same path many times —
	// up to ~1.3M emits in sg5 with the new CP closures — and JSON escape was
	// 27% of CPU until this cache went in. The intern table is append-only and
	// strID() is stable, so a slice indexed by strID is safe; we grow it lazily
	// to the current intern bound on the first miss past its length.
	vfsEscapedJSON   [][]byte
	htmlSafeNoEscape = func() [128]bool {
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
)

func writeGraphIndented(w io.Writer, g *Graph) {

	buf := make([]byte, 0, 1<<20)

	buf = append(buf, '{', '\n')

	buf = append(buf, `    "conf": `...)
	buf = appendGraphConf(buf, g.Conf, "    ")
	buf = append(buf, ',')
	buf = append(buf, '\n')

	buf = append(buf, `    "graph": `...)

	if len(g.Graph) == 0 {
		buf = append(buf, '[', ']', ',', '\n')
	} else {
		buf = append(buf, '[', '\n')

		for i, node := range g.Graph {
			buf = appendNode(buf, node, "        ")

			if i < len(g.Graph)-1 {
				buf = append(buf, ',')
			}

			buf = append(buf, '\n')

			if len(buf) >= 256<<10 {
				Throw2(w.Write(buf))
				buf = buf[:0]
			}
		}

		buf = append(buf, `    ],`...)
		buf = append(buf, '\n')
	}

	buf = append(buf, `    "inputs": {},`...)
	buf = append(buf, '\n')

	buf = append(buf, `    "result": `...)
	buf = appendStringSlice(buf, g.Result, "    ")
	buf = append(buf, '\n')

	buf = append(buf, '}')

	Throw2(w.Write(buf))
}

func appendNode(buf []byte, n *Node, pad string) []byte {
	innerPad := pad + "    "
	buf = append(buf, pad...)
	buf = append(buf, '{', '\n')

	if n.Cache != nil {
		buf = append(buf, innerPad...)
		if *n.Cache {
			buf = append(buf, `"cache": true,`...)
		} else {
			buf = append(buf, `"cache": false,`...)
		}
		buf = append(buf, '\n')
	}

	buf = append(buf, innerPad...)
	buf = append(buf, `"cmds": `...)
	buf = appendCmdSlice(buf, n.Cmds, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"deps": `...)
	buf = appendStringSlice(buf, n.Deps, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"env": `...)
	buf = appendStringMap(buf, n.Env, innerPad)
	buf = append(buf, ',', '\n')

	if len(n.ForeignDeps) > 0 {
		buf = append(buf, innerPad...)
		buf = append(buf, `"foreign_deps": `...)
		buf = appendStringSliceMap(buf, n.ForeignDeps, innerPad)
		buf = append(buf, ',', '\n')
	}

	if nodeHasHostTag(n.Tags) {
		buf = append(buf, innerPad...)
		buf = append(buf, `"host_platform": true,`...)
		buf = append(buf, '\n')
	}

	buf = append(buf, innerPad...)
	buf = append(buf, `"inputs": `...)
	buf = appendVFSSlice(buf, n.Inputs, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"kv": `...)
	buf = appendInterfaceMap(buf, n.KV, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"outputs": `...)
	buf = appendVFSSlice(buf, n.Outputs, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"platform": `...)
	buf = appendString(buf, n.Platform)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"requirements": `...)
	buf = appendInterfaceMap(buf, n.Requirements, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	if n.Sandboxing {
		buf = append(buf, `"sandboxing": true,`...)
	} else {
		buf = append(buf, `"sandboxing": false,`...)
	}
	buf = append(buf, '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"self_uid": `...)
	buf = appendString(buf, n.SelfUID)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"stats_uid": `...)
	buf = appendString(buf, n.StatsUID)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"tags": `...)
	buf = appendStringSlice(buf, n.Tags, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"target_properties": `...)
	buf = appendStringMap(buf, n.TargetProperties, innerPad)
	buf = append(buf, ',', '\n')

	buf = append(buf, innerPad...)
	buf = append(buf, `"uid": `...)
	buf = appendString(buf, n.UID)
	buf = append(buf, '\n')

	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
}

func appendCmdSlice(buf []byte, cmds []Cmd, pad string) []byte {
	if len(cmds) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[', '\n')
	itemPad := pad + "    "
	innerPad := itemPad + "    "

	for i, c := range cmds {
		buf = append(buf, itemPad...)
		buf = append(buf, '{', '\n')

		buf = append(buf, innerPad...)
		buf = append(buf, `"cmd_args": `...)
		buf = appendStringSlice(buf, c.CmdArgs, innerPad)

		if c.Cwd != "" {
			buf = append(buf, ',', '\n')
			buf = append(buf, innerPad...)
			buf = append(buf, `"cwd": `...)
			buf = appendString(buf, c.Cwd)
		}

		if len(c.Env) > 0 {
			buf = append(buf, ',', '\n')
			buf = append(buf, innerPad...)
			buf = append(buf, `"env": `...)
			buf = appendStringMap(buf, c.Env, innerPad)
		}

		if c.Stdout != "" {
			buf = append(buf, ',', '\n')
			buf = append(buf, innerPad...)
			buf = append(buf, `"stdout": `...)
			buf = appendString(buf, c.Stdout)
		}

		buf = append(buf, '\n')

		buf = append(buf, itemPad...)
		buf = append(buf, '}')

		if i < len(cmds)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, ']')

	return buf
}

func appendStringSlice(buf []byte, ss []string, pad string) []byte {
	if len(ss) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[', '\n')
	itemPad := pad + "    "

	for i, s := range ss {
		buf = append(buf, itemPad...)
		buf = appendString(buf, s)

		if i < len(ss)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, ']')

	return buf
}

func appendVFSSlice(buf []byte, vs []VFS, pad string) []byte {
	if len(vs) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[', '\n')
	itemPad := pad + "    "

	for i, v := range vs {
		buf = append(buf, itemPad...)
		buf = appendVFS(buf, v)

		if i < len(vs)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, ']')

	return buf
}

func appendGraphConf(buf []byte, conf map[string]interface{}, pad string) []byte {
	if len(conf) == 0 {
		return append(buf, '{', '}')
	}

	resourcesAny, ok := conf["resources"]
	if !ok || len(conf) != 1 {
		ThrowFmt("writeGraphIndented: unsupported conf shape")
	}

	resources, ok := resourcesAny.([]graphConfResource)
	if !ok {
		ThrowFmt("writeGraphIndented: unsupported conf.resources type %T", resourcesAny)
	}

	buf = append(buf, '{', '\n')
	itemPad := pad + "    "

	buf = append(buf, itemPad...)
	buf = append(buf, `"resources": `...)
	buf = appendGraphConfResources(buf, resources, itemPad)
	buf = append(buf, '\n')

	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
}

func appendGraphConfResources(buf []byte, resources []graphConfResource, pad string) []byte {
	if len(resources) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[', '\n')
	itemPad := pad + "    "

	for i, r := range resources {
		buf = appendGraphConfResource(buf, r, itemPad)

		if i < len(resources)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, ']')

	return buf
}

func appendGraphConfResource(buf []byte, r graphConfResource, pad string) []byte {
	buf = append(buf, pad...)
	buf = append(buf, '{', '\n')
	itemPad := pad + "    "

	first := true
	addComma := func() {
		if !first {
			buf = append(buf, ',')
			buf = append(buf, '\n')
		}

		first = false
	}

	if r.Name != "" {
		addComma()
		buf = append(buf, itemPad...)
		buf = append(buf, `"name": `...)
		buf = appendString(buf, r.Name)
	}

	addComma()
	buf = append(buf, itemPad...)
	buf = append(buf, `"pattern": `...)
	buf = appendString(buf, r.Pattern)

	if r.Resource != "" {
		addComma()
		buf = append(buf, itemPad...)
		buf = append(buf, `"resource": `...)
		buf = appendString(buf, r.Resource)
	}

	if len(r.Resources) != 0 {
		addComma()
		buf = append(buf, itemPad...)
		buf = append(buf, `"resources": `...)
		buf = appendGraphConfResourceURIs(buf, r.Resources, itemPad)
	}

	buf = append(buf, '\n')
	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
}

func appendGraphConfResourceURIs(buf []byte, resources []graphConfResourceURI, pad string) []byte {
	if len(resources) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[', '\n')
	itemPad := pad + "    "

	for i, r := range resources {
		buf = append(buf, itemPad...)
		buf = append(buf, '{', '\n')
		fieldPad := itemPad + "    "

		buf = append(buf, fieldPad...)
		buf = append(buf, `"platform": `...)
		buf = appendString(buf, r.Platform)
		buf = append(buf, ',', '\n')

		buf = append(buf, fieldPad...)
		buf = append(buf, `"resource": `...)
		buf = appendString(buf, r.Resource)
		buf = append(buf, '\n')

		buf = append(buf, itemPad...)
		buf = append(buf, '}')

		if i < len(resources)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, ']')

	return buf
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

func appendStringMap(buf []byte, m map[string]string, pad string) []byte {
	if len(m) == 0 {
		return append(buf, '{', '}')
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf = append(buf, '{', '\n')
	itemPad := pad + "    "

	for i, k := range keys {
		buf = append(buf, itemPad...)
		buf = appendString(buf, k)
		buf = append(buf, ':', ' ')
		buf = appendString(buf, m[k])

		if i < len(keys)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
}

func appendStringSliceMap(buf []byte, m map[string][]string, pad string) []byte {
	if len(m) == 0 {
		return append(buf, '{', '}')
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf = append(buf, '{', '\n')
	itemPad := pad + "    "

	for i, k := range keys {
		buf = append(buf, itemPad...)
		buf = appendString(buf, k)
		buf = append(buf, ':', ' ')
		buf = appendStringSlice(buf, m[k], itemPad)

		if i < len(keys)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
}

func appendInterfaceMap(buf []byte, m map[string]interface{}, pad string) []byte {
	if len(m) == 0 {
		return append(buf, '{', '}')
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf = append(buf, '{', '\n')
	itemPad := pad + "    "

	for i, k := range keys {
		buf = append(buf, itemPad...)
		buf = appendString(buf, k)
		buf = append(buf, ':', ' ')

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
			ThrowFmt("writeGraphIndented: unsupported requirement value type %T for key %q", v, k)
		}

		if i < len(keys)-1 {
			buf = append(buf, ',')
		}

		buf = append(buf, '\n')
	}

	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
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
