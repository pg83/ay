package main

import (
	"slices"
	"strconv"
	"strings"
)

var (
	envVarsVCS       = EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}
	envVarsVCSYasm   = EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}, {Name: envYASM_TEST_SUITE, Value: strOne.any()}}
	envVarsVCSPyHash = EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}, {Name: envPYTHONHASHSEED, Value: strZero.any()}}
	envVarsVCSCuda   = EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}, {Name: cudaPathEnv, Value: cudaPathValueStr.any()}}
)

var procKindStr = [...]string{
	pkNone: "", pkAS: "AS", pkAR: "AR", pkBI: "BI", pkBC: "BC", pkCC: "CC",
	pkCF: "CF", pkCH: "CH", pkCP: "CP", pkCY: "CY", pkEN: "EN", pkEV: "EV",
	pkFETCH: "FT", pkFL: "FL", pkFL64: "FL64", pkFM: "FM", pkGP: "GP", pkGZ: "GZ", pkHT: "HT", pkJS: "JS", pkJV: "JV", pkLD: "LD", pkGO: "GO", pkGoTool: "go", pkLX: "LX", pkLJ: "LJ", pkLU: "LU", pkMN: "MN", pkOP: "OP",
	pkPB: "PB", pkPR: "PR", pkPY: "PY", pkR5: "R5", pkR6: "R6", pkRD: "RD",
	pkSB:   "SB",
	pkSF:   "SF",
	pkSTUB: "STUB", pkSW: "SW", pkTEST: "TEST", pkTEST2: "TEST2", pkTS: "TS", pkYC: "YC",
	pkld: "ld", pkDX: "DX", pkBN: "BN", pkSV: "SV", pkSC: "SC", pkPD: "PD", pkCU: "CU",
}

var pColorStr = [...]string{
	pcNone: "", pcGreen: "green", pcLightBlue: "light-blue", pcLightCyan: "light-cyan",
	pcLightGreen: "light-green", pcLightRed: "light-red", pcMagenta: "magenta", pcYellow: "yellow",
}

var networkModeStr = [...]string{
	nwNone: "", nwRestricted: "restricted", nwFull: "full",
}

const (
	nwNone NetworkMode = iota
	nwRestricted
	nwFull
)

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
	pkFL64
	pkFM
	pkGP
	pkGZ
	pkHT
	pkJS
	pkJV
	pkLD
	pkGO
	pkGoTool
	pkLX
	pkLJ
	pkLU
	pkMN
	pkOP
	pkPB
	pkPR
	pkPY
	pkR5
	pkR6
	pkRD
	pkSB
	pkSF
	pkSTUB
	pkSW
	pkTEST
	pkTEST2
	pkTS
	pkYC
	pkld
	pkDX
	pkBN
	pkSV
	pkSC
	pkPD
	pkCU
)

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

type EnvVar struct {
	Name  ENV
	Value ANY
}

type EnvVars []EnvVar

func appendEnv(buf []byte, env EnvVars) []byte {
	buf = append(buf, '{')

	for i, e := range env {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = appendString(buf, e.Name.string())
		buf = append(buf, ':')

		if v := e.Value.vfs(); v != 0 {
			buf = appendVFS(buf, v)
		} else {
			buf = appendString(buf, e.Value.string())
		}
	}

	return append(buf, '}')
}

type NetworkMode uint8

func (m NetworkMode) string() string {
	return networkModeStr[m]
}

func (m NetworkMode) String() string {
	return m.string()
}

type Requirements struct {
	CPU        float64
	RAM        float64
	Network    NetworkMode
	RAMDisk    float64
	HasRAMDisk bool
}

var emptyRequirements = Requirements{}

func (r Requirements) isEmpty() bool {
	return r.CPU == 0 && r.RAM == 0 && r.Network == nwNone && !r.HasRAMDisk
}

type ProcKind uint8

func (k ProcKind) string() string {
	return procKindStr[k]
}

func (k ProcKind) String() string {
	return k.string()
}

type PColor uint8

func (c PColor) string() string {
	return pColorStr[c]
}

func (c PColor) String() string {
	return c.string()
}

type KV struct {
	P                ProcKind
	PC               PColor
	ShowOut          bool
	ShowOutBool      bool
	Name             string
	Path             string
	DisableCache     bool
	SpecialRunner    string
	HasSpecialRunner bool
	RunTestNode      bool
}

type KVExt struct {
	Key string
	Val string
}

func sortedKVExts(exts []KVExt) []KVExt {
	if len(exts) < 2 {
		return exts
	}

	out := append([]KVExt(nil), exts...)

	slices.SortFunc(out, func(a, b KVExt) int { return strings.Compare(a.Key, b.Key) })

	return out
}

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

func appendRequirements(buf []byte, r *Requirements) []byte {
	if r == nil {
		return append(buf, `{"cpu":1,"network":"restricted","ram":32}`...)
	}

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

func appendKV(buf []byte, kv KV, exts []KVExt) []byte {
	o := JsonObj{buf: append(buf, '{')}

	if kv.DisableCache {
		o.forceStr("disable_cache", "yes")
	}

	for _, e := range sortedKVExts(exts) {
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

func (e EnvVars) marshalJSON() ([]byte, error) {
	return appendEnv(nil, e), nil
}

func (e EnvVars) MarshalJSON() ([]byte, error) {
	return e.marshalJSON()
}

func (kv KV) marshalJSON() ([]byte, error) {
	return appendKV(nil, kv, nil), nil
}

func (kv KV) MarshalJSON() ([]byte, error) {
	return kv.marshalJSON()
}

func (r Requirements) marshalJSON() ([]byte, error) {
	return appendRequirements(nil, &r), nil
}

func (r Requirements) MarshalJSON() ([]byte, error) {
	return r.marshalJSON()
}
