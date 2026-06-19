package main

import (
	"strings"
	"testing"
)

// sbomComponentOutputs returns every `.component.sbom` output path emitted in
// the graph (the per-module _GEN_SBOM_COMPONENT DX nodes).
func sbomComponentOutputs(g *Graph) []string {
	var out []string
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), ".component.sbom") {
				out = append(out, o.string())
			}
		}
	}
	return out
}

func hasSbomOutputUnderDir(g *Graph, moddir string) (bool, string) {
	prefix := "$(B)/" + moddir + "/"
	for _, o := range sbomComponentOutputs(g) {
		if strings.HasPrefix(o, prefix) {
			return true, o
		}
	}
	return false, ""
}

// TestSbom_PY23NativeLibraryIsCPPLangNotPY3 pins the pycxx divergence: the PY3
// submodule of a PY23_NATIVE_LIBRARY (build/conf/python.conf:1245) does
// SET(MODULE_LANG CPP), so its SBOM component is <name>.CPP.component.sbom — not
// .PY3. The MODULE_LANG drives both the output suffix and the --lang arg
// (build/internal/conf/sbom.conf:43).
//
// TestSbom_PyOnlyProtoLibraryEmitsNoComponent pins the builtin_proto
// divergence: a PROTO_LIBRARY with EXCLUDE_TAGS(CPP_PROTO) builds no CPP_PROTO
// submodule (the only proto submodule that carries _NEED_SBOM_INFO=yes); the
// remaining py-proto submodule does DISABLE(_NEED_SBOM_INFO) (proto.conf:806).
// So such a module emits no .component.sbom at all.
func writeSbomFixture(files map[string]string) {
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
	files["contrib/python/protobuf/ya.make"] = "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n"
	files["contrib/libs/python/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n"

	// internal SBOM contour + toolchain peers (mirrors emit_proto_desc_test).
	files["build/internal/conf/sbom.conf"] = "SBOM_GENERATION_ALLOWED=yes\n"
	files["build/platform/python/ymake_python3/ya.make"] = "RESOURCES_LIBRARY()\nTOOLCHAIN(python3)\nVERSION(3.12.6)\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(YMAKE_PYTHON3 python.json)\nEND()\n"
	files["build/platform/python/ymake_python3/python.json"] = `{"by_platform":{"linux-x86_64":{"uri":"sbr:test"}}}`
	files["build/internal/platform/clang_toolchain_info/ya.make"] = "RESOURCES_LIBRARY()\nTOOLCHAIN(clang)\nVERSION(16)\nEND()\n"
}

func TestSbom_PY23NativeLibraryIsCPPLangNotPY3(t *testing.T) {
	const natDir = "contrib/libs/nat"

	files := map[string]string{}
	writeSbomFixture(files)

	files[natDir+"/ya.make"] = "PY23_NATIVE_LIBRARY()\nLICENSE(BSD-3-Clause)\nVERSION(1.0)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nSRCS(a.cpp)\nEND()\n"
	files[natDir+"/a.cpp"] = "int a(){return 0;}\n"

	files["app/ya.make"] = "PROGRAM(app)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nPEERDIR(" + natDir + ")\nSRCS(m.cpp)\nEND()\n"
	files["app/m.cpp"] = "int main(){return 0;}\n"

	g := testGenX86(newMemFS(files), "app")

	prj := realPrjName(natDir)
	wantCPP := "$(B)/" + natDir + "/" + prj + ".CPP.component.sbom"
	badPY3 := "$(B)/" + natDir + "/" + prj + ".PY3.component.sbom"

	if n := nodeByOutput(g, badPY3); n != nil {
		t.Errorf("PY23_NATIVE_LIBRARY emitted PY3 component %q; MODULE_LANG of its PY3 submodule is CPP", badPY3)
	}
	cpp := mustNodeByAnyOutput(t, g, wantCPP)

	// The --lang arg must match the suffix (both come from MODULE_LANG).
	var sawLang bool
	for _, c := range cpp.Cmds {
		args := c.CmdArgs.flat()
		for i, a := range args {
			if a.string() == "--lang" && i+1 < len(args) {
				sawLang = true
				if got := args[i+1].string(); got != "CPP" {
					t.Errorf("--lang = %q, want CPP", got)
				}
			}
		}
	}
	if !sawLang {
		t.Fatalf("SBOM component node has no --lang arg; cmds=%v", cpp.Cmds)
	}
}

func TestSbom_PyOnlyProtoLibraryEmitsNoComponent(t *testing.T) {
	const protoDir = "contrib/libs/pyonlyproto"

	files := map[string]string{}
	writeSbomFixture(files)

	files[protoDir+"/ya.make"] = "PROTO_LIBRARY()\nLICENSE(BSD-3-Clause)\nVERSION(1.0)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nEXCLUDE_TAGS(CPP_PROTO GO_PROTO)\nSRCS(foo.proto)\nEND()\n"
	files[protoDir+"/foo.proto"] = "syntax = \"proto3\";\npackage foo;\nmessage Foo { int32 x = 1; }\n"

	files["app/ya.make"] = "PROGRAM(app)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nPEERDIR(" + protoDir + ")\nSRCS(m.cpp)\nEND()\n"
	files["app/m.cpp"] = "int main(){return 0;}\n"

	g := testGenX86(newMemFS(files), "app")

	if has, o := hasSbomOutputUnderDir(g, protoDir); has {
		t.Errorf("py-only PROTO_LIBRARY (EXCLUDE_TAGS CPP_PROTO) emitted SBOM component %q; the py-proto submodule does DISABLE(_NEED_SBOM_INFO) and the CPP_PROTO submodule is excluded", o)
	}
}

// TestSbom_OrdinaryLibrariesUnchanged guards that the two fixes do not disturb
// ordinary C++ / Python library SBOM behavior: a licensed C++ LIBRARY keeps its
// .CPP component and a licensed PY3_LIBRARY keeps its .PY3 component.
func TestSbom_OrdinaryLibrariesUnchanged(t *testing.T) {
	const cppDir = "contrib/libs/plaincpp"
	const pyDir = "contrib/libs/plainpy"

	files := map[string]string{}
	writeSbomFixture(files)

	files[cppDir+"/ya.make"] = "LIBRARY()\nLICENSE(BSD-3-Clause)\nVERSION(1.0)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nSRCS(a.cpp)\nEND()\n"
	files[cppDir+"/a.cpp"] = "int a(){return 0;}\n"
	files[pyDir+"/ya.make"] = "PY3_LIBRARY()\nLICENSE(BSD-3-Clause)\nVERSION(1.0)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nEND()\n"

	files["app/ya.make"] = "PROGRAM(app)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nPEERDIR(" + cppDir + "\n" + pyDir + ")\nSRCS(m.cpp)\nEND()\n"
	files["app/m.cpp"] = "int main(){return 0;}\n"

	g := testGenX86(newMemFS(files), "app")

	wantCPP := "$(B)/" + cppDir + "/" + realPrjName(cppDir) + ".CPP.component.sbom"
	wantPY3 := "$(B)/" + pyDir + "/" + realPrjName(pyDir) + ".PY3.component.sbom"
	mustNodeByAnyOutput(t, g, wantCPP)
	mustNodeByAnyOutput(t, g, wantPY3)
}
