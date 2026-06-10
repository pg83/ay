package main

import (
	"sort"
	"strconv"
)

var procKindStr = [...]string{
	pkNone: "", pkAS: "AS", pkAR: "AR", pkBI: "BI", pkBC: "BC", pkCC: "CC",
	pkCF: "CF", pkCH: "CH", pkCP: "CP", pkCY: "CY", pkEN: "EN", pkEV: "EV",
	pkFETCH: "FT", pkFL: "FL", pkJS: "JS", pkJV: "JV", pkLD: "LD", pkOP: "OP",
	pkPB: "PB", pkPR: "PR", pkPY: "PY", pkR5: "R5", pkR6: "R6", pkRD: "RD",
	pkSTUB: "STUB", pkSW: "SW", pkTEST: "TEST", pkTEST2: "TEST2", pkTS: "TS", pkYC: "YC",
}

var pColorStr = [...]string{
	pcNone: "", pcGreen: "green", pcLightBlue: "light-blue", pcLightCyan: "light-cyan",
	pcLightGreen: "light-green", pcLightRed: "light-red", pcMagenta: "magenta", pcYellow: "yellow",
}

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
	Name  ENV
	Value STR
}

// EnvVars is the ordered binding list for Node.Env / Cmd.Env.
type EnvVars []EnvVar

func appendEnv(buf []byte, env EnvVars) []byte {
	buf = append(buf, '{')

	for i, e := range env {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendString(buf, e.Name.String())
		buf = append(buf, ':')
		buf = appendString(buf, e.Value.String())
	}

	return append(buf, '}')
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

// ProcKind is a node's process kind (the kv "p" value): a small fixed set of
// codes the executor and dumps dispatch on. A uint8 enum (1 byte stored per
// node, integer compares) expanded to its string only at JSON emit / hash; the
// zero value pkNone means absent (empty kv has no "p").
type ProcKind uint8

const (
	pkNone ProcKind = iota
	pkAS
	pkAR
	pkBI
	pkBC
	pkCC
	pkCF
	pkCH
	pkCP
	pkCY
	pkEN
	pkEV
	pkFETCH
	pkFL
	pkJS
	pkJV
	pkLD
	pkOP
	pkPB
	pkPR
	pkPY
	pkR5
	pkR6
	pkRD
	pkSTUB
	pkSW
	pkTEST
	pkTEST2
	pkTS
	pkYC
)

func (k ProcKind) String() string {
	return procKindStr[k]
}

// PColor is a node's display colour (the kv "pc" value); same uint8-enum scheme.
type PColor uint8

const (
	pcNone PColor = iota
	pcGreen
	pcLightBlue
	pcLightCyan
	pcLightGreen
	pcLightRed
	pcMagenta
	pcYellow
)

func (c PColor) String() string {
	return pColorStr[c]
}

// KV is a node's kv block. P (process kind) is on every node; PC/ShowOut/Name/
// Path/DisableCache are optional string keys; RunTestNode and the bool form of
// show_out (ShowOutBool) and the present-but-empty special_runner appear on test
// nodes; ExtOut carries the dynamic "ext_out_name_for_<file>" entries (py-proto).
type KV struct {
	P                ProcKind
	PC               PColor
	ShowOut          bool // emitted as the string "yes"
	ShowOutBool      bool // test nodes emit show_out as bool true (used iff !ShowOut)
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
	o.str("p", kv.P.String())
	o.str("path", kv.Path)
	o.str("pc", kv.PC.String())
	o.boolTrue("run_test_node", kv.RunTestNode)

	if kv.ShowOut {
		o.forceStr("show_out", "yes")
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
func (e EnvVars) MarshalJSON() ([]byte, error) {
	return appendEnv(nil, e), nil
}

func (kv KV) MarshalJSON() ([]byte, error) {
	return appendKV(nil, kv), nil
}

func (r Requirements) MarshalJSON() ([]byte, error) {
	return appendRequirements(nil, r), nil
}

func (t TargetProperties) MarshalJSON() ([]byte, error) {
	return appendTargetProperties(nil, t), nil
}
