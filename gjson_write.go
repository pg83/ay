package main

import (
	"io"
	"sort"
	"strconv"
	"unicode/utf8"
)

// gjson_write.go — hand-rolled streaming JSON serializer, byte-identical
// to json.Encoder with SetEscapeHTML(false) + SetIndent("", "    ") for
// the Graph shape. Replaces json.Encoder's two-pass indent path; walks
// Graph once with inline indentation and a reused per-Node buffer.
//
// Byte-exactness invariants:
//   - Map keys alphabetical (json default).
//   - omitempty fields (host_platform, foreign_deps, cache on Node;
//     cwd, stdout on Cmd) dropped when zero.
//   - Empty slices/maps render as `[]`/`{}` inline.
//   - Strings: encoding/json escape table with EscapeHTML=false
//     (U+2028/U+2029 still escaped; '<>&' pass through).
//   - Floats: strconv.AppendFloat(_, 'f', -1, 64).
//   - No trailing newline — final byte is '}'.

// writeGraphIndented streams g as indented JSON to w. Errors from w
// propagate via Throw (the make -G / writeGraph contract panics up to
// main's Catch).
func writeGraphIndented(w io.Writer, g *Graph) {
	// Single reusable encode buffer. 1 MiB initial caps the largest
	// observed node (~30 KiB) plus skeleton; grows by doubling.
	buf := make([]byte, 0, 1<<20)

	buf = append(buf, '{', '\n')

	buf = append(buf, `    "conf": `...)
	buf = appendGraphConf(buf, g.Conf, "    ")
	buf = append(buf, ',')
	buf = append(buf, '\n')

	// "graph": [ ...nodes... ]
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

			// Flush periodically so we don't hold the whole 63 MiB output
			// in RAM. 256 KiB stays above any single node's encoded size
			// and aligns well with the bufio.Writer's chunks.
			if len(buf) >= 256<<10 {
				Throw2(w.Write(buf))
				buf = buf[:0]
			}
		}

		buf = append(buf, `    ],`...)
		buf = append(buf, '\n')
	}

	// "inputs": {} — always empty per emitter.go's Finalize contract.
	buf = append(buf, `    "inputs": {},`...)
	buf = append(buf, '\n')

	// "result": [ ...uids... ]
	buf = append(buf, `    "result": `...)
	buf = appendStringSlice(buf, g.Result, "    ")
	buf = append(buf, '\n')

	// No trailing newline — REF's last byte is '}'.
	buf = append(buf, '}')

	Throw2(w.Write(buf))
}

// appendNode emits a single Node as a JSON object indented under `pad`.
// Field order is alphabetical (declaration order in node.go); stats_uid
// is omitted (json:"-"). cache is emitted only when n.Cache != nil
// (omitempty equivalent).
func appendNode(buf []byte, n *Node, pad string) []byte {
	innerPad := pad + "    "
	buf = append(buf, pad...)
	buf = append(buf, '{', '\n')

	// cache: *bool, omitempty — emit only when explicitly set.
	if n.Cache != nil {
		buf = append(buf, innerPad...)
		if *n.Cache {
			buf = append(buf, `"cache": true,`...)
		} else {
			buf = append(buf, `"cache": false,`...)
		}
		buf = append(buf, '\n')
	}

	// cmds: []Cmd
	buf = append(buf, innerPad...)
	buf = append(buf, `"cmds": `...)
	buf = appendCmdSlice(buf, n.Cmds, innerPad)
	buf = append(buf, ',', '\n')

	// deps: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"deps": `...)
	buf = appendStringSlice(buf, n.Deps, innerPad)
	buf = append(buf, ',', '\n')

	// env: map[string]string
	buf = append(buf, innerPad...)
	buf = append(buf, `"env": `...)
	buf = appendStringMap(buf, n.Env, innerPad)
	buf = append(buf, ',', '\n')

	// foreign_deps: map[string][]string, omitempty
	if len(n.ForeignDeps) > 0 {
		buf = append(buf, innerPad...)
		buf = append(buf, `"foreign_deps": `...)
		buf = appendStringSliceMap(buf, n.ForeignDeps, innerPad)
		buf = append(buf, ',', '\n')
	}

	// host_platform: bool, omitempty (only emitted when true)
	if n.HostPlatform {
		buf = append(buf, innerPad...)
		buf = append(buf, `"host_platform": true,`...)
		buf = append(buf, '\n')
	}

	// inputs: []VFS
	buf = append(buf, innerPad...)
	buf = append(buf, `"inputs": `...)
	buf = appendVFSSlice(buf, n.Inputs, innerPad)
	buf = append(buf, ',', '\n')

	// kv: map[string]string
	buf = append(buf, innerPad...)
	buf = append(buf, `"kv": `...)
	buf = appendStringMap(buf, n.KV, innerPad)
	buf = append(buf, ',', '\n')

	// outputs: []VFS
	buf = append(buf, innerPad...)
	buf = append(buf, `"outputs": `...)
	buf = appendVFSSlice(buf, n.Outputs, innerPad)
	buf = append(buf, ',', '\n')

	// platform: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"platform": `...)
	buf = appendString(buf, n.Platform)
	buf = append(buf, ',', '\n')

	// requirements: map[string]interface{}
	buf = append(buf, innerPad...)
	buf = append(buf, `"requirements": `...)
	buf = appendInterfaceMap(buf, n.Requirements, innerPad)
	buf = append(buf, ',', '\n')

	// sandboxing: bool — emitted unconditionally to match REF.
	buf = append(buf, innerPad...)
	if n.Sandboxing {
		buf = append(buf, `"sandboxing": true,`...)
	} else {
		buf = append(buf, `"sandboxing": false,`...)
	}
	buf = append(buf, '\n')

	// self_uid: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"self_uid": `...)
	buf = appendString(buf, n.SelfUID)
	buf = append(buf, ',', '\n')

	// stats_uid is tagged json:"-" and omitted; see node.go.

	// tags: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"tags": `...)
	buf = appendStringSlice(buf, n.Tags, innerPad)
	buf = append(buf, ',', '\n')

	// target_properties: map[string]string
	buf = append(buf, innerPad...)
	buf = append(buf, `"target_properties": `...)
	buf = appendStringMap(buf, n.TargetProperties, innerPad)
	buf = append(buf, ',', '\n')

	// uid: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"uid": `...)
	buf = appendString(buf, n.UID)
	buf = append(buf, '\n')

	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
}

// appendCmdSlice emits []Cmd. Inner pad is the indent of the array's
// opening '[' (cmd_args' indent inside each Cmd is innerPad+"        ").
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

		// cmd_args: []string
		buf = append(buf, innerPad...)
		buf = append(buf, `"cmd_args": `...)
		buf = appendStringSlice(buf, c.CmdArgs, innerPad)
		buf = append(buf, ',', '\n')

		// cwd: string, omitempty
		if c.Cwd != "" {
			buf = append(buf, innerPad...)
			buf = append(buf, `"cwd": `...)
			buf = appendString(buf, c.Cwd)
			buf = append(buf, ',', '\n')
		}

		// env: map[string]string
		buf = append(buf, innerPad...)
		buf = append(buf, `"env": `...)
		buf = appendStringMap(buf, c.Env, innerPad)

		// stdout: omitempty; emitted after env for alphabetical key order
		// (cmd_args, cwd, env, stdout).
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

// appendStringSlice emits []string. Empty slice is `[]` on the same line.
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

// appendVFSSlice emits []VFS in the same shape as appendStringSlice —
// each element renders as its canonical "$(S)/<rel>" or "$(B)/<rel>".
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

// appendVFS emits a single VFS in JSON-string form. Materialises the
// canonical VFS string through VFS.String().
func appendVFS(buf []byte, v VFS) []byte {
	buf = append(buf, '"')
	buf = appendStringEscapedBody(buf, v.String())
	buf = append(buf, '"')

	return buf
}

// appendStringMap emits map[string]string with keys sorted (json default).
// Empty map is `{}` on the same line.
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

// appendStringSliceMap emits map[string][]string with keys sorted.
// Empty map is `{}` on the same line.
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

// appendInterfaceMap emits map[string]interface{} for Requirements. The
// codebase stores only float64/string/bool here; any other dynamic type
// throws so a future gap surfaces immediately rather than as a silent
// byte-mismatch.
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

// hex is the lookup table for \u00xx escapes in appendString.
const hex = "0123456789abcdef"

// appendString emits s as a JSON-escaped string matching encoding/json
// with EscapeHTML=false: 0x00..0x1f as \uXXXX (with \b\t\n\f\r shortcuts),
// '"' and '\\' escaped, U+2028/U+2029 escaped (JS-compat carve-out),
// invalid UTF-8 replaced with U+FFFD; '<>&' pass through.
func appendString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	buf = appendStringEscapedBody(buf, s)
	buf = append(buf, '"')
	return buf
}

// appendStringEscapedBody emits the JSON-escaped BODY of a string,
// without surrounding quotes. Extracted so concatenation callers (e.g.
// appendVFS materialising "$(S)/" + rel) avoid heap concat: append
// prefix body + rel body inside one shared pair of quotes.
func appendStringEscapedBody(buf []byte, s string) []byte {
	start := 0
	for i := 0; i < len(s); {
		// Fast ASCII path (no escape needed).
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
				// 0x00..0x1f minus the shortcuts above.
				buf = append(buf, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xf])
			}

			i++
			start = i

			continue
		}

		// Multi-byte UTF-8.
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

		// JSONP/JS line separator carve-out.
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

// htmlSafeNoEscape[b] reports whether ASCII byte b can pass through
// verbatim with EscapeHTML=false. False means escaping needed (control
// char, '"', '\\'). We never index this table for b >= 0x80.
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
