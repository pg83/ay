package main

import (
	"sort"
	"strconv"
)

var procKindStr = [...]string{
	pkNone: "", pkAS: "AS", pkAR: "AR", pkBI: "BI", pkBC: "BC", pkCC: "CC",
	pkCF: "CF", pkCH: "CH", pkCP: "CP", pkCY: "CY", pkEN: "EN", pkEV: "EV",
	pkFETCH: "FT", pkFL: "FL", pkFL64: "FL64", pkGP: "GP", pkGZ: "GZ", pkJS: "JS", pkJV: "JV", pkLD: "LD", pkLX: "LX", pkLJ: "LJ", pkOP: "OP",
	pkPB: "PB", pkPR: "PR", pkPY: "PY", pkR5: "R5", pkR6: "R6", pkRD: "RD",
	pkSB:   "SB",
	pkSTUB: "STUB", pkSW: "SW", pkTEST: "TEST", pkTEST2: "TEST2", pkTS: "TS", pkYC: "YC",
	pkld: "ld", pkDX: "DX", pkBN: "BN", pkSV: "SV", pkSC: "SC", pkPD: "PD",
}

var pColorStr = [...]string{
	pcNone: "", pcGreen: "green", pcLightBlue: "light-blue", pcLightCyan: "light-cyan",
	pcLightGreen: "light-green", pcLightRed: "light-red", pcMagenta: "magenta", pcYellow: "yellow",
}

var networkModeStr = [...]string{
	nwNone: "", nwRestricted: "restricted", nwFull: "full",
}

var moduleLangStr = [...]string{
	mlNone: "", mlCPP: "cpp", mlPy3: "py3", mlUnknown: "unknown", mlAgnostic: "agnostic",
	mlDescProto: "desc_proto", mlProtoDescriptions: "proto_descriptions",
}

var moduleTypeStr = [...]string{
	mtNone: "", mtBin: "bin", mtLib: "lib", mtSO: "so",
}

// Typed replacements for the former map-valued Node fields, dropping the map
// iteration, key sort and interface{} boxing the string-keyed maps paid.

// EnvVar is one environment binding. Node.Env and Cmd.Env are ordered []EnvVar:
// nothing looks them up by key, and the gate re-parses the env JSON into a map
// before hashing, so emission order is free.
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

		buf = appendString(buf, e.Name.string())
		buf = append(buf, ':')
		buf = appendString(buf, e.Value.string())
	}

	return append(buf, '}')
}

// NetworkMode is a node's "network" requirement. Zero value nwNone = absent.
type NetworkMode uint8

const (
	nwNone NetworkMode = iota
	nwRestricted
	nwFull
)

func (m NetworkMode) string() string {
	return networkModeStr[m]
}

// String implements fmt.Stringer.
func (m NetworkMode) String() string {
	return m.string()
}

// Requirements is a node's scheduler resource set. The zero value is the empty
// set, serialized as {}.
type Requirements struct {
	CPU        float64
	RAM        float64
	Network    NetworkMode
	RAMDisk    float64 // present-with-zero on test nodes
	HasRAMDisk bool
}

func (r Requirements) isEmpty() bool {
	return r.CPU == 0 && r.RAM == 0 && r.Network == nwNone && !r.HasRAMDisk
}

// ModuleLang is a node's "module_lang" target property. Not the ModuleInstance
// Language enum: LangPy surfaces as "py3". Zero value mlNone = absent.
type ModuleLang uint8

const (
	mlNone ModuleLang = iota
	mlCPP
	mlPy3
	mlUnknown
	mlAgnostic
	mlDescProto         // DESC_PROTO submodule merge node
	mlProtoDescriptions // PROTO_DESCRIPTIONS merge node
)

func (l ModuleLang) string() string {
	return moduleLangStr[l]
}

// String implements fmt.Stringer.
func (l ModuleLang) String() string {
	return l.string()
}

// ModuleType is a node's "module_type" target property. Zero value mtNone = absent.
type ModuleType uint8

const (
	mtNone ModuleType = iota
	mtBin
	mtLib
	mtSO
)

func (t ModuleType) string() string {
	return moduleTypeStr[t]
}

// String implements fmt.Stringer.
func (t ModuleType) String() string {
	return t.string()
}

// TargetProperties is a node's module attributes; empty fields are omitted.
type TargetProperties struct {
	ModuleDir  string
	ModuleTag  STR
	ModuleLang ModuleLang
	ModuleType ModuleType
}

// ProcKind is a node's process kind (the kv "p" value). Zero value pkNone = absent.
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
	pkFL64 // 64-bit flatbuffers compiler
	pkGP   // gperf
	pkGZ   // gazetteer converter, .gztproto → .proto
	pkJS
	pkJV
	pkLD
	pkLX // old-flex lexer producer
	pkLJ // LuaJIT objdump, .lua → .raw
	pkOP
	pkPB
	pkPR
	pkPY
	pkR5
	pkR6
	pkRD
	pkSB
	pkSTUB
	pkSW
	pkTEST
	pkTEST2
	pkTS
	pkYC
	pkld // lowercase "ld": PREBUILT_PROGRAM copy node, distinct from pkLD link
	pkDX // toolchain SBOM node
	pkBN // BUNDLE rename node
	pkSV // DECIMAL_MD5_LOWER_32_BITS hash producer
	pkSC // SPLIT_CODEGEN producer
	pkPD // proto-description producer
)

func (k ProcKind) string() string {
	return procKindStr[k]
}

// String implements fmt.Stringer.
func (k ProcKind) String() string {
	return k.string()
}

// PColor is a node's display colour (the kv "pc" value).
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

func (c PColor) string() string {
	return pColorStr[c]
}

// String implements fmt.Stringer.
func (c PColor) String() string {
	return c.string()
}

// KV is a node's kv block. P (process kind) is on every node; the rest are
// optional or test-node-specific. ExtOut carries the dynamic
// "ext_out_name_for_<file>" entries.
type KV struct {
	P                ProcKind
	PC               PColor
	ShowOut          bool // emitted as the string "yes"
	ShowOutBool      bool // test nodes emit show_out as bool true (iff !ShowOut)
	Name             string
	Path             string
	DisableCache     string
	SpecialRunner    string
	HasSpecialRunner bool // special_runner is emitted even when empty
	RunTestNode      bool
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

// --- JSON (graph output): keys emitted in sorted order, optional keys omitted. ---

// jsonObj accumulates comma separation for a JSON object being appended.
type JsonObj struct {
	buf []byte
	n   int
}

func (o *JsonObj) sep() {
	if o.n > 0 {
		o.buf = append(o.buf, ',')
	}

	o.n++
}

func (o *JsonObj) str(key, val string) {
	if val == "" {
		return
	}

	o.forceStr(key, val)
}

func (o *JsonObj) forceStr(key, val string) {
	o.sep()
	o.buf = appendString(o.buf, key)
	o.buf = append(o.buf, ':')
	o.buf = appendString(o.buf, val)
}

func (o *JsonObj) boolTrue(key string, v bool) {
	if !v {
		return
	}

	o.sep()
	o.buf = appendString(o.buf, key)
	o.buf = append(o.buf, ':', 't', 'r', 'u', 'e')
}

func (o *JsonObj) num(key string, v float64) {
	o.sep()
	o.buf = appendString(o.buf, key)
	o.buf = append(o.buf, ':')
	o.buf = strconv.AppendFloat(o.buf, v, 'f', -1, 64)
}

func appendRequirements(buf []byte, r Requirements) []byte {
	if r.isEmpty() {
		return append(buf, '{', '}')
	}

	o := JsonObj{buf: append(buf, '{')}

	if r.CPU != 0 {
		o.num("cpu", r.CPU)
	}

	o.str("network", r.Network.string())

	if r.RAM != 0 {
		o.num("ram", r.RAM)
	}

	if r.HasRAMDisk {
		o.num("ram_disk", r.RAMDisk)
	}

	return append(o.buf, '}')
}

func appendTargetProperties(buf []byte, t TargetProperties) []byte {
	o := JsonObj{buf: append(buf, '{')}

	o.str("module_dir", t.ModuleDir)
	o.str("module_lang", t.ModuleLang.string())
	o.str("module_tag", t.ModuleTag.string())
	o.str("module_type", t.ModuleType.string())

	return append(o.buf, '}')
}

func appendKV(buf []byte, kv KV) []byte {
	o := JsonObj{buf: append(buf, '{')}

	o.str("disable_cache", kv.DisableCache)

	// "ext_out_name_for_*" sorts after disable_cache and before "name".
	for _, e := range kv.sortedExt() {
		o.forceStr(e.Key, e.Val)
	}

	o.str("name", kv.Name)
	o.str("p", kv.P.string())
	o.str("path", kv.Path)
	o.str("pc", kv.PC.string())
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

// marshalJSON makes the standard encoder agree with the hand-rolled graph
// writer (sorted keys / {} for empty), not the struct's Go field names.
func (e EnvVars) marshalJSON() ([]byte, error) {
	return appendEnv(nil, e), nil
}

// MarshalJSON implements json.Marshaler.
func (e EnvVars) MarshalJSON() ([]byte, error) {
	return e.marshalJSON()
}

func (kv KV) marshalJSON() ([]byte, error) {
	return appendKV(nil, kv), nil
}

// MarshalJSON implements json.Marshaler.
func (kv KV) MarshalJSON() ([]byte, error) {
	return kv.marshalJSON()
}

func (r Requirements) marshalJSON() ([]byte, error) {
	return appendRequirements(nil, r), nil
}

// MarshalJSON implements json.Marshaler.
func (r Requirements) MarshalJSON() ([]byte, error) {
	return r.marshalJSON()
}

func (t TargetProperties) marshalJSON() ([]byte, error) {
	return appendTargetProperties(nil, t), nil
}

// MarshalJSON implements json.Marshaler.
func (t TargetProperties) MarshalJSON() ([]byte, error) {
	return t.marshalJSON()
}
