package main

import (
	"strings"
	"testing"
)

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

func writeSbomFixture(files map[string]string) {
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
	files["contrib/python/protobuf/ya.make"] = "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n"
	files["contrib/libs/python/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n"

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

func TestSbom_CppProtoLibraryComponentTagged(t *testing.T) {
	const protoDir = "contrib/libs/cppproto"

	files := map[string]string{}
	writeSbomFixture(files)

	files[protoDir+"/ya.make"] = "PROTO_LIBRARY()\nLICENSE(BSD-3-Clause)\nVERSION(1.0)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nEXCLUDE_TAGS(GO_PROTO)\nSRCS(foo.proto)\nEND()\n"
	files[protoDir+"/foo.proto"] = "syntax = \"proto3\";\npackage foo;\nmessage Foo { int32 x = 1; }\n"

	files["app/ya.make"] = "PROGRAM(app)\nNO_LIBC()\nNO_RUNTIME()\nNO_PLATFORM()\nNO_UTIL()\nPEERDIR(" + protoDir + ")\nSRCS(m.cpp)\nEND()\n"
	files["app/m.cpp"] = "int main(){return 0;}\n"

	g := testGenX86(newMemFS(files), "app")

	want := "$(B)/" + protoDir + "/" + realPrjName(protoDir) + ".CPP.component.sbom"
	n := mustNodeByAnyOutput(t, g, want)

	if got := n.TargetProperties.ModuleTag.string(); got != "cpp_proto" {
		t.Errorf("CPP_PROTO library SBOM component module_tag = %q, want cpp_proto", got)
	}
}

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
