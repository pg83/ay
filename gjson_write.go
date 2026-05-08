package main

import (
	"io"
	"sort"
	"strconv"
	"unicode/utf8"
)

// gjson_write.go — hand-rolled streaming JSON serializer that produces
// output byte-identical to json.Encoder with SetEscapeHTML(false) and
// SetIndent("", "  ") for the Graph shape (D16 cross-cutting constraint).
//
// Why this exists: the stock json.Encoder Encode-with-indent path runs in
// two passes — encode-compact into a temp buffer, then json.Indent into
// the destination. For the 63 MiB tools/archiver graph, that double-pass
// dominated wall-clock (~300 ms of a ~1.5 s warm run, ~20 %). The
// hand-rolled version below walks the Graph once, writing indentation
// inline as it goes, and reuses a small per-Node working buffer to avoid
// per-field allocations. It is intentionally specialised to the Graph /
// Node / Cmd shape rather than reflection-driven; that is what makes it
// fast.
//
// Byte-exactness is the ironclad invariant. Every formatting decision
// below mirrors encoding/json's behaviour for the shapes we actually
// emit:
//   - Map keys are emitted in alphabetical order (json default).
//   - omitempty fields (host_platform, foreign_deps on Node; cwd on Cmd)
//     are dropped when zero.
//   - Empty slices and maps emit as `[]` / `{}` on the same line.
//   - Strings: same escape table as encoding/json with EscapeHTML=false
//     (control chars, quote, backslash, U+2028/U+2029; '<', '>', '&'
//     pass through unchanged).
//   - Floats: strconv.AppendFloat(_, 'f', -1, 64) for the magnitudes
//     this codebase actually emits (cpu/ram in Requirements are
//     float64(1) and float64(32), which serialise as "1" and "32").
//   - Trailing newline: yes, after the closing '}' of the top-level
//     object — matching json.Encoder.Encode's behaviour.
//
// The parallel test (gjson_write_test.go) compares the output of this
// function against json.Encoder for a fixture covering every code path
// listed above; if it passes, the runtime output is byte-identical.

// writeGraphIndented streams g as indented JSON to w. Errors from w
// propagate via Throw so the cmdGen / writeGraph contract (panic up to
// main()'s Catch) is preserved.
func writeGraphIndented(w io.Writer, g *Graph) {
	// Single reusable encode buffer. 1 MiB initial capacity is roomy
	// for the largest individual node we have observed (~30 KiB) plus
	// the structural skeleton; the buffer grows by doubling if needed.
	buf := make([]byte, 0, 1<<20)

	buf = append(buf, '{', '\n')

	// "conf": {} — always empty per emitter.go's Finalize contract.
	buf = append(buf, `  "conf": {},`...)
	buf = append(buf, '\n')

	// "graph": [ ...nodes... ]
	buf = append(buf, `  "graph": `...)

	if len(g.Graph) == 0 {
		buf = append(buf, '[', ']', ',', '\n')
	} else {
		buf = append(buf, '[', '\n')

		for i, node := range g.Graph {
			buf = appendNode(buf, node, "    ")

			if i < len(g.Graph)-1 {
				buf = append(buf, ',')
			}

			buf = append(buf, '\n')

			// Flush periodically so we don't hold the whole 63 MiB output
			// in RAM. 256 KiB is well above any single node's encoded
			// size and keeps the bufio.Writer's chunks well-aligned.
			if len(buf) >= 256<<10 {
				Throw2(w.Write(buf))
				buf = buf[:0]
			}
		}

		buf = append(buf, `  ],`...)
		buf = append(buf, '\n')
	}

	// "inputs": {} — always empty per emitter.go's Finalize contract.
	buf = append(buf, `  "inputs": {},`...)
	buf = append(buf, '\n')

	// "result": [ ...uids... ]
	buf = append(buf, `  "result": `...)
	buf = appendStringSlice(buf, g.Result, "  ")
	buf = append(buf, '\n')

	buf = append(buf, '}', '\n')

	Throw2(w.Write(buf))
}

// appendNode emits a single Node as a JSON object indented under `pad`
// (the indent of the object's opening '{'). Field order is alphabetical
// per the Node struct's declaration order in node.go (cmds, deps, env,
// foreign_deps, host_platform, inputs, kv, outputs, platform,
// requirements, self_uid, stats_uid, tags, target_properties, uid).
func appendNode(buf []byte, n *Node, pad string) []byte {
	innerPad := pad + "  "
	buf = append(buf, pad...)
	buf = append(buf, '{', '\n')

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

	// inputs: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"inputs": `...)
	buf = appendStringSlice(buf, n.Inputs, innerPad)
	buf = append(buf, ',', '\n')

	// kv: map[string]string
	buf = append(buf, innerPad...)
	buf = append(buf, `"kv": `...)
	buf = appendStringMap(buf, n.KV, innerPad)
	buf = append(buf, ',', '\n')

	// outputs: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"outputs": `...)
	buf = appendStringSlice(buf, n.Outputs, innerPad)
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

	// self_uid: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"self_uid": `...)
	buf = appendString(buf, n.SelfUID)
	buf = append(buf, ',', '\n')

	// stats_uid: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"stats_uid": `...)
	buf = appendString(buf, n.StatsUID)
	buf = append(buf, ',', '\n')

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
// opening '[' (cmd_args' indent inside each Cmd is innerPad+"    ").
func appendCmdSlice(buf []byte, cmds []Cmd, pad string) []byte {
	if len(cmds) == 0 {
		return append(buf, '[', ']')
	}

	buf = append(buf, '[', '\n')
	itemPad := pad + "  "
	innerPad := itemPad + "  "

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
	itemPad := pad + "  "

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
	itemPad := pad + "  "

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
	itemPad := pad + "  "

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

// appendInterfaceMap emits map[string]interface{} for the Requirements
// field. The codebase only ever stores float64 or string values there
// (see ar.go / cc.go / ld.go / as.go / js.go). Any other type Throws so
// future contributors notice the gap rather than hitting a silent
// byte-mismatch with the reference output.
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
	itemPad := pad + "  "

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

// appendString emits s as a JSON-escaped string with EscapeHTML=false,
// matching encoding/json's behaviour byte-for-byte for inputs we
// actually encounter. The escape table below is the EscapeHTML=false
// branch of encoding/json/encode.go's encodeState.string method:
//
//   - 0x00..0x1f : \uXXXX, with shortcuts \b \t \n \f \r for 0x08..0x0d
//     (skipping 0x0b which has no shortcut).
//   - 0x22 ('"') : \"
//   - 0x5c ('\\'): \\
//   - 0x80..0xff: pass through as part of UTF-8 sequences except for
//     U+2028 (E2 80 A8) and U+2029 (E2 80 A9), which become   and
//     — JavaScript-compat carve-out preserved by encoding/json.
//   - Invalid UTF-8: replaced with �.
func appendString(buf []byte, s string) []byte {
	buf = append(buf, '"')

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

	buf = append(buf, '"')

	return buf
}

// htmlSafeNoEscape[b] reports whether ASCII byte b can be emitted
// verbatim in a JSON string when EscapeHTML is false. False means the
// byte needs escaping (control char, '"', '\\') OR the byte is part of
// a multi-byte UTF-8 sequence (>=0x80, but we never index into this
// table for those).
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
