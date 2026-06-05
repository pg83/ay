package main

import (
	"math"
	"sort"
	"strconv"
)

// Typed replacements for the former map-valued Node fields. These drop the
// per-node map iteration, key sort and interface{} boxing the canonical-hash and
// JSON-write paths paid when KV, Requirements, TargetProperties and Env were
// string-keyed maps. The fields and their types mirror exactly what upstream
// ymake emits (see the sg* reference graphs).

// EnvVar is one environment binding. Node.Env and Cmd.Env are ordered []EnvVar
// rather than maps: nothing looks them up by key (they are only iterated — to
// serialize, to hash and to set the child process env), so a slice is cheaper to
// store (nil for the common empty case, no per-node map alloc) and to write (no
// key sort). The gate re-parses the env JSON object into a map before hashing, so
// the emission order is free.
type EnvVar struct {
	Name  string
	Value string
}

// EnvVars is the ordered binding list for Node.Env / Cmd.Env.
type EnvVars []EnvVar

func appendEnv(buf []byte, env EnvVars) []byte {
	buf = append(buf, '{')

	for i, e := range env {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendString(buf, e.Name)
		buf = append(buf, ':')
		buf = appendString(buf, e.Value)
	}

	return append(buf, '}')
}

func (c *canonBuf) writeEnv(env EnvVars) {
	c.writeUint32(uint32(len(env)))

	for _, e := range env {
		c.writeBytes(e.Name)
		c.writeBytes(e.Value)
	}
}

// Requirements is a node's scheduler resource set. The zero value (no flags set)
// is the empty set, serialized as {}.
type Requirements struct {
	CPU        float64
	RAM        float64
	Network    string
	RAMDisk    float64 // present-with-zero on test nodes, hence the explicit flag
	HasRAMDisk bool
}

func (r Requirements) isEmpty() bool {
	return r.CPU == 0 && r.RAM == 0 && r.Network == "" && !r.HasRAMDisk
}

// TargetProperties is a node's module attributes. Empty fields are omitted,
// matching the old sparse map.
type TargetProperties struct {
	ModuleDir  string
	ModuleTag  string
	ModuleLang string
	ModuleType string
}

// KV is a node's kv block. P (process kind) is on every node; PC/ShowOut/Name/
// Path/DisableCache are optional string keys; RunTestNode and the bool form of
// show_out (ShowOutBool) and the present-but-empty special_runner appear on test
// nodes; ExtOut carries the dynamic "ext_out_name_for_<file>" entries (py-proto).
type KV struct {
	P                string
	PC               string
	ShowOut          string // "yes" string form
	ShowOutBool      bool   // test nodes emit show_out as bool true (used iff ShowOut == "")
	Name             string
	Path             string
	DisableCache     string
	SpecialRunner    string
	HasSpecialRunner bool // special_runner is emitted even when empty
	RunTestNode      bool // emitted only when true
	ExtOut           []KVExt
}

// KVExt is one dynamic ext_out_name_for_<base> entry.
type KVExt struct {
	Key string
	Val string
}

func (kv KV) sortedExt() []KVExt {
	if len(kv.ExtOut) < 2 {
		return kv.ExtOut
	}

	out := append([]KVExt(nil), kv.ExtOut...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })

	return out
}

// --- JSON (graph output): keys emitted in sorted order, optional keys omitted,
// so the bytes match the former sort-then-write of the maps. ---

// jsonObj accumulates comma separation for a JSON object being appended.
type jsonObj struct {
	buf []byte
	n   int
}

func (o *jsonObj) sep() {
	if o.n > 0 {
		o.buf = append(o.buf, ',')
	}

	o.n++
}

func (o *jsonObj) str(key, val string) {
	if val == "" {
		return
	}

	o.forceStr(key, val)
}

func (o *jsonObj) forceStr(key, val string) {
	o.sep()
	o.buf = appendString(o.buf, key)
	o.buf = append(o.buf, ':')
	o.buf = appendString(o.buf, val)
}

func (o *jsonObj) boolTrue(key string, v bool) {
	if !v {
		return
	}

	o.sep()
	o.buf = appendString(o.buf, key)
	o.buf = append(o.buf, ':', 't', 'r', 'u', 'e')
}

func (o *jsonObj) num(key string, v float64) {
	o.sep()
	o.buf = appendString(o.buf, key)
	o.buf = append(o.buf, ':')
	o.buf = strconv.AppendFloat(o.buf, v, 'f', -1, 64)
}

func appendRequirements(buf []byte, r Requirements) []byte {
	if r.isEmpty() {
		return append(buf, '{', '}')
	}

	o := jsonObj{buf: append(buf, '{')}

	if r.CPU != 0 {
		o.num("cpu", r.CPU)
	}

	o.str("network", r.Network)

	if r.RAM != 0 {
		o.num("ram", r.RAM)
	}

	if r.HasRAMDisk {
		o.num("ram_disk", r.RAMDisk)
	}

	return append(o.buf, '}')
}

func appendTargetProperties(buf []byte, t TargetProperties) []byte {
	o := jsonObj{buf: append(buf, '{')}

	o.str("module_dir", t.ModuleDir)
	o.str("module_lang", t.ModuleLang)
	o.str("module_tag", t.ModuleTag)
	o.str("module_type", t.ModuleType)

	return append(o.buf, '}')
}

func appendKV(buf []byte, kv KV) []byte {
	o := jsonObj{buf: append(buf, '{')}

	o.str("disable_cache", kv.DisableCache)

	// "ext_out_name_for_*" sorts after disable_cache and before "name".
	for _, e := range kv.sortedExt() {
		o.forceStr(e.Key, e.Val)
	}

	o.str("name", kv.Name)
	o.str("p", kv.P)
	o.str("path", kv.Path)
	o.str("pc", kv.PC)
	o.boolTrue("run_test_node", kv.RunTestNode)

	if kv.ShowOut != "" {
		o.forceStr("show_out", kv.ShowOut)
	} else {
		o.boolTrue("show_out", kv.ShowOutBool)
	}

	if kv.HasSpecialRunner {
		o.forceStr("special_runner", kv.SpecialRunner)
	}

	return append(o.buf, '}')
}

// MarshalJSON makes the standard encoder agree with the hand-rolled graph
// writer (appendKV/appendRequirements/appendTargetProperties) — so json.Marshal
// of a Node emits {} for the empty case and the sorted keys otherwise, instead
// of the struct's Go field names.
func (e EnvVars) MarshalJSON() ([]byte, error)          { return appendEnv(nil, e), nil }
func (kv KV) MarshalJSON() ([]byte, error)              { return appendKV(nil, kv), nil }
func (r Requirements) MarshalJSON() ([]byte, error)     { return appendRequirements(nil, r), nil }
func (t TargetProperties) MarshalJSON() ([]byte, error) { return appendTargetProperties(nil, t), nil }

// --- canonical hash (gen-time self_uid): a fixed-field deterministic encoding.
// The gate recomputes content hashes from the JSON, so only determinism matters
// here, not parity with the old map encoding. ---

func (c *canonBuf) writeRequirements(r Requirements) {
	c.writeUint64(math.Float64bits(r.CPU))
	c.writeUint64(math.Float64bits(r.RAM))
	c.writeBytes(r.Network)
	c.writeUint64(math.Float64bits(r.RAMDisk))
	c.writeBool(r.HasRAMDisk)
}

func (c *canonBuf) writeTargetProperties(t TargetProperties) {
	c.writeBytes(t.ModuleDir)
	c.writeBytes(t.ModuleTag)
	c.writeBytes(t.ModuleLang)
	c.writeBytes(t.ModuleType)
}

func (c *canonBuf) writeKV(kv KV) {
	c.writeBytes(kv.P)
	c.writeBytes(kv.PC)
	c.writeBytes(kv.ShowOut)
	c.writeBool(kv.ShowOutBool)
	c.writeBytes(kv.Name)
	c.writeBytes(kv.Path)
	c.writeBytes(kv.DisableCache)
	c.writeBytes(kv.SpecialRunner)
	c.writeBool(kv.HasSpecialRunner)
	c.writeBool(kv.RunTestNode)
	c.writeUint32(uint32(len(kv.ExtOut)))

	for _, e := range kv.sortedExt() {
		c.writeBytes(e.Key)
		c.writeBytes(e.Val)
	}
}
