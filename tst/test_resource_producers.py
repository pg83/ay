import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
LIBRARY_STUB = f"LIBRARY()\n{BARE}END()\n"
PROTOBUF_HEADERS = (
    "arena.h", "arenastring.h", "descriptor.proto", "extension_set.h",
    "generated_message_bases.h", "generated_message_reflection.h",
    "generated_message_util.h", "io/coded_stream.h", "io/printer.h",
    "io/zero_copy_sink.h", "map_entry.h", "map_entry_lite.h", "map_field.h",
    "map_field_inl.h", "map_field_lite.h", "message.h", "metadata_lite.h",
    "port_def.inc", "port_undef.inc", "reflection_ops.h", "repeated_field.h",
    "stubs/hash.h", "stubs/stringpiece.h", "stubs/strutil.h",
    "unknown_field_set.h", "wire_format.h",
)

CYTHON_UTILITY = (
    "arrayarray.h", "AsyncGen.c", "Buffer.c", "Builtins.c", "CConvert.pyx",
    "CMath.c", "CommonStructures.c", "CommonTypes.c", "Complex.c",
    "Coroutine.c", "CpdefEnums.pyx", "CppConvert.pyx", "CppSupport.cpp",
    "CythonFunction.c", "Dataclasses.c", "Embed.c", "Exceptions.c",
    "ExtensionTypes.c", "FunctionArguments.c", "ImportExport.c",
    "MemoryView.pyx", "MemoryView_C.c", "ModuleSetupCode.c",
    "NumpyImportArray.c", "ObjectHandling.c", "Optimize.c", "Overflow.c",
    "Printing.c", "Profile.c", "StringTools.c", "TestCyUtilityLoader.pyx",
    "TestCythonScope.pyx", "TestUtilityLoader.c", "UFuncs_C.c",
)
CYTHON_HEADERS = (
    "contrib/tools/cython/cython.py",
    "contrib/tools/cython/generated_c_headers.h",
    "contrib/tools/cython/generated_cpp_headers.h",
    "contrib/libs/python/Include/compile.h",
    "contrib/libs/python/Include/frameobject.h",
    "contrib/libs/python/Include/longintrepr.h",
    "contrib/libs/python/Include/pyconfig.h",
    "contrib/libs/python/Include/Python.h",
    "contrib/libs/python/Include/pythread.h",
    "contrib/libs/python/Include/structmember.h",
    "contrib/libs/python/Include/traceback.h",
    "contrib/libs/cxxsupp/openmp/omp.h",
)
SWIG_LIBRARY = ("swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg")


def with_tools(files, *paths):
    for path in paths:
        lib.tool_program(files, path, path.split("/")[-1])
    return files


def proto_files(body, sources):
    files = {
        "lib/ya.make": f"LIBRARY()\n{BARE}{body}END()\n",
        "build/scripts/cpp_proto_wrapper.py": "",
        "contrib/libs/protobuf/ya.make": LIBRARY_STUB,
        "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h": "",
        "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h": "",
    }
    for header in PROTOBUF_HEADERS:
        files[f"contrib/libs/protobuf/src/google/protobuf/{header}"] = ""
    files.update(sources)
    return with_tools(
        files,
        "contrib/tools/protoc",
        "contrib/tools/protoc/plugins/cpp_styleguide",
        "tools/gen",
    )


def make_failure(files, target):
    with tempfile.TemporaryDirectory(prefix="ay-producers-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        result = subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=10,
            check=False,
        )
    if result.returncode == 0:
        raise AssertionError("ay make unexpectedly succeeded")
    return result.stderr


class ProducerOrderTest(unittest.TestCase):
    def test_consumers_declared_first_still_follow_their_producers(self):
        files = with_tools({
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "CREATE_BUILDINFO_FOR(buildinfo_data.h)\n"
                "DECIMAL_MD5_LOWER_32_BITS(md5.cpp FUNCNAME GetMd5"
                " ${ARCADIA_BUILD_ROOT}/lib/opt.txt)\n"
                "BUILD_MN(model.info mymn)\n"
                "ARCHIVE_ASM(NAME asm_data a.txt)\n"
                "ARCHIVE(NAME arch.inc b.txt)\n"
                "CHECK_CONFIG_H(conf.h)\n"
                "GENERATE_ENUM_SERIALIZATION(e.h)\n"
                "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(e2.h)\n"
                "LJ_21_ARCHIVE(NAME scripts s.lua)\n"
                "JOIN_SRCS(all.cpp a.cpp)\n"
                "CONFIGURE_FILE(${ARCADIA_BUILD_ROOT}/lib/tpl.in cfg.h)\n"
                "RUN_PROGRAM(tools/gen OUT_NOAUTO opt.txt tpl.in)\n"
                "SRCS(a.cpp user.cpp)\n"
                "END()\n"
            ),
            "lib/a.cpp": "",
            "lib/user.cpp": '#include "cfg.h"\n#include "buildinfo_data.h"\n',
            "lib/model.info": "",
            "lib/a.txt": "",
            "lib/b.txt": "",
            "lib/conf.h": "",
            "lib/e.h": "enum E { A };\n",
            "lib/e2.h": "enum F { B };\n",
            "lib/s.lua": "",
            "kernel/matrixnet/mn_sse.h": "",
            "util/generic/serialized_enum.h": "",
            "build/scripts/build_info_gen.py": "",
            "build/scripts/configure_file.py": "",
            "build/scripts/xargs.py": "",
            "build/scripts/yield_line.py": "",
        }, "tools/gen", "tools/archiver", "contrib/tools/yasm",
            "contrib/libs/luajit_21/compiler", "tools/enum_parser/enum_parser",
            "tools/enum_parser/enum_serialization_runtime")
        graph = lib.make(
            files, "lib", "--target-platform", "default-linux-x86_64",
        )
        producer = lib.node_by_output(graph, "$(B)/lib/opt.txt")
        configure = lib.node_by_output(graph, "$(B)/lib/cfg.h")
        self.assertEqual(configure["kv"]["p"], "CF")
        self.assertEqual(configure["inputs"], [
            "$(S)/build/scripts/configure_file.py",
            "$(B)/lib/tpl.in",
            "$(B)/lib/opt.txt",
        ])
        self.assertEqual(configure["deps"], [producer["uid"]])
        md5 = lib.node_by_output(graph, "$(B)/lib/md5.cpp")
        self.assertEqual(md5["inputs"], [
            "$(B)/lib/opt.txt",
            "$(S)/build/scripts/decimal_md5.py",
        ])
        self.assertEqual(md5["deps"], [producer["uid"]])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/lib/buildinfo_data.h")["kv"]["p"],
            "BI",
        )
        for output in (
            "$(B)/lib/mn.mymn.cpp",
            "$(B)/lib/asm_data.rodata",
            "$(B)/lib/arch.inc",
            "$(B)/lib/conf.config.cpp",
            "$(B)/lib/e.h_serialized.cpp",
            "$(B)/lib/e2.h_serialized.h",
            "$(B)/lib/s.raw",
            "$(B)/lib/LuaScripts.inc",
            "$(B)/lib/all.cpp",
        ):
            lib.node_by_output(graph, output)
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        for member in (
            "$(B)/lib/mn.mymn.cpp.o",
            "$(B)/lib/md5.cpp.o",
            "$(B)/lib/e.h_serialized.cpp.o",
            "$(B)/lib/conf.config.cpp.o",
            "$(B)/lib/all.cpp.o",
            "$(B)/lib/user.cpp.o",
        ):
            self.assertIn(member, archive["inputs"])

    def test_generating_macros_forming_a_cycle_are_rejected(self):
        stderr = make_failure({
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "COPY_FILE(${ARCADIA_BUILD_ROOT}/lib/b.txt a.txt)\n"
                "COPY_FILE(${ARCADIA_BUILD_ROOT}/lib/a.txt b.txt)\n"
                "END()\n"
            ),
        }, "lib")
        self.assertIn("gen: lib declares a dependency cycle among generating macros", stderr)

    def test_module_build_input_without_producer_is_reported(self):
        stderr = make_failure(with_tools({
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "RUN_PROGRAM(tools/gen IN ${ARCADIA_BUILD_ROOT}/other/x.txt"
                " ${ARCADIA_BUILD_ROOT}/lib/nothing.txt STDOUT out.cpp)\n"
                "END()\n"
            ),
        }, "tools/gen"), "lib")
        self.assertIn(
            'missing-producer: lib: IN "$(B)/lib/nothing.txt" resolves to build file'
            " $(B)/lib/nothing.txt that no declared macro produces",
            stderr,
        )
        self.assertNotIn("other/x.txt", stderr)


class ProtoProducerTest(unittest.TestCase):
    def test_proto_waits_for_generated_import(self):
        files = proto_files(
            "GRPC()\n"
            "SET(PROTOC_TRANSITIVE_HEADERS no)\n"
            "ALICE_CAPABILITY()\n"
            "SRCS(a.proto ${BINDIR}/gen.proto)\n"
            "RUN_PROGRAM(tools/gen OUT_NOAUTO gen.proto)\n",
            {
                "lib/a.proto": 'syntax = "proto3";\nimport "lib/gen.proto";\n',
                "yandex_io/libs/protobuf_utils/ya.make": LIBRARY_STUB,
                "contrib/libs/grpc/ya.make": LIBRARY_STUB,
            },
        )
        with_tools(
            files,
            "yandex_io/tools/capability_gen",
            "contrib/tools/protoc/plugins/grpc_cpp",
        )
        graph = lib.make(files, "lib")
        producer = lib.node_by_output(graph, "$(B)/lib/gen.proto")
        suffixes = (".pb.h", ".pb.cc", ".deps.pb.h", ".grpc.pb.cc", ".grpc.pb.h", ".cap.h")
        for stem, source in (("a", "$(S)/lib/a.proto"), ("gen", "$(B)/lib/gen.proto")):
            with self.subTest(stem=stem):
                node = lib.node_by_output(graph, f"$(B)/lib/{stem}.pb.h")
                self.assertEqual(node["kv"]["p"], "PB")
                self.assertEqual(
                    node["outputs"],
                    [f"$(B)/lib/{stem}{suffix}" for suffix in suffixes],
                )
                self.assertIn(source, node["inputs"])
                self.assertIn(producer["uid"], node["deps"])
        a_node = lib.node_by_output(graph, "$(B)/lib/a.pb.h")
        self.assertIn("$(B)/lib/gen.proto", a_node["inputs"])

    def test_proto_namespace_output_roots(self):
        for namespace, include in (("ns", "-I=$(S)/ns"), ("..", "-I=$(S)/..")):
            with self.subTest(namespace=namespace):
                graph = lib.make(proto_files(
                    f"PROTO_NAMESPACE({namespace})\n"
                    "SRCS(a.proto b.proto c.proto d.proto)\n",
                    {
                        "lib/a.proto": (
                            'syntax = "proto3";\n'
                            'import "lib/b.proto";\n'
                            'import "lib/./c.proto";\n'
                            'import "$(S)/lib/d.proto";\n'
                        ),
                        "lib/b.proto": 'syntax = "proto3";\n',
                        "lib/c.proto": 'syntax = "proto3";\n',
                        "lib/d.proto": 'syntax = "proto3";\n',
                    },
                ), "lib")
                node = lib.node_by_output(graph, "$(B)/lib/a.pb.h")
                args = node["cmds"][0]["cmd_args"]
                self.assertIn(include, args)
                self.assertEqual(args[-1], "lib/a.proto")
                self.assertEqual(sorted(p for p in node["inputs"] if p.endswith(".proto")), [
                    "$(S)/lib/a.proto",
                    "$(S)/lib/b.proto",
                    "$(S)/lib/c.proto",
                    "$(S)/lib/d.proto",
                ])

    def test_python_proto_variant_and_gazetteer_sources(self):
        files = proto_files("", {
            "proto/ya.make": "PROTO_LIBRARY()\nGRPC()\nSRCS(a.proto x.gztproto)\nEND()\n",
            "proto/a.proto": 'syntax = "proto3";\n',
            "proto/x.gztproto": 'syntax = "proto3";\n',
            "py/ya.make": "PY3_PROGRAM()\nPEERDIR(proto)\nPY_SRCS(MAIN main.py)\nEND()\n",
            "py/main.py": "",
            "build/scripts/gen_py_protos.py": "",
        })
        for path in (
            "contrib/libs/python",
            "contrib/python/protobuf",
            "contrib/python/grpcio",
            "contrib/libs/grpc",
            "contrib/tools/python3",
            "contrib/tools/python3/Modules/_sqlite",
            "kernel/gazetteer/proto",
            "library/cpp/malloc/jemalloc",
            "library/cpp/resource",
            "library/python/import_tracing/constructor",
            "library/python/resource",
            "library/python/runtime_py3",
            "library/python/runtime_py3/main",
            "library/python/testing/import_test",
        ):
            files[f"{path}/ya.make"] = LIBRARY_STUB
        with_tools(
            files,
            "contrib/tools/protoc/plugins/grpc_cpp",
            "contrib/tools/protoc/plugins/grpc_python",
            "contrib/python/mypy-protobuf/bin/protoc-gen-mypy",
            "dict/gazetteer/converter",
            "tools/archiver",
            "tools/py3cc",
            "tools/py3cc/slow",
            "tools/rescompiler",
            "tools/rescompressor",
        )
        graph = lib.make(files, "py")
        python_proto = lib.node_by_output(graph, "$(B)/proto/a__intpy3___pb2.py")
        self.assertEqual(python_proto["outputs"], [
            "$(B)/proto/a__intpy3___pb2.py",
            "$(B)/proto/a__intpy3___pb2_grpc.py",
            "$(B)/proto/a__intpy3___pb2.pyi",
        ])
        converter = lib.node_by_output(graph, "$(B)/proto/x.proto")
        self.assertEqual(converter["kv"]["p"], "GZ")
        self.assertEqual(converter["inputs"], [
            "$(B)/dict/gazetteer/converter/converter",
            "$(S)/proto/x.gztproto",
        ])
        cpp_proto = lib.node_by_output(graph, "$(B)/proto/x.pb.h")
        self.assertEqual(cpp_proto["outputs"], [
            "$(B)/proto/x.pb.h",
            "$(B)/proto/x.pb.cc",
            "$(B)/proto/x.grpc.pb.cc",
            "$(B)/proto/x.grpc.pb.h",
        ])
        self.assertIn("$(B)/proto/x.proto", cpp_proto["inputs"])
        self.assertIn(converter["uid"], cpp_proto["deps"])


class OtherGeneratorTest(unittest.TestCase):
    def test_antlr4_and_sproto_generators(self):
        files = with_tools({
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "RUN_ANTLR4_CPP(Expr.g4 -visitor)\n"
                "YMAPS_SPROTO(sp.proto)\n"
                "SRCS(user.cpp)\n"
                "END()\n"
            ),
            "lib/user.cpp": '#include "lib/sp.sproto.h"\n',
            "lib/Expr.g4": "grammar Expr;\n",
            "lib/sp.proto": 'syntax = "proto3";\n',
            "build/platform/java/jdk/jdk17/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(JDK17 sbr:1)\nEND()\n"
            ),
            "maps/libs/sproto/ya.make": LIBRARY_STUB,
            "contrib/libs/antlr4_cpp_runtime/ya.make": LIBRARY_STUB,
            "contrib/libs/antlr4_cpp_runtime/src/antlr4-runtime.h": "",
        }, "maps/libs/sproto/sprotoc")
        graph = lib.make(files, "lib")
        antlr = lib.node_by_output(graph, "$(B)/lib/ExprParser.cpp")
        self.assertEqual(antlr["outputs"], [
            "$(B)/lib/ExprLexer.cpp",
            "$(B)/lib/ExprLexer.h",
            "$(B)/lib/ExprParser.cpp",
            "$(B)/lib/ExprParser.h",
            "$(B)/lib/ExprVisitor.h",
            "$(B)/lib/ExprBaseVisitor.h",
        ])
        sproto = lib.node_by_output(graph, "$(B)/lib/sp.sproto.h")
        self.assertEqual(sproto["cmds"][0]["cmd_args"], [
            "$(B)/maps/libs/sproto/sprotoc/sprotoc",
            "-I=./",
            "-I=$(S)/",
            "-I=$(B)",
            "-I=$(S)/contrib/libs/protobuf/src",
            "--sproto_out=$(B)/",
            "lib/sp.proto",
        ])
        self.assertIn(sproto["uid"], lib.node_by_output(graph, "$(B)/lib/user.cpp.o")["deps"])

    def test_go_cgo_producers(self):
        files = {
            "gol/ya.make": "GO_LIBRARY()\nSRCS(a.go b.c)\nCGO_SRCS(c.go)\nEND()\n",
            "gol/a.go": "package gol\n",
            "gol/b.c": "int b(void){return 0;}\n",
            "gol/c.go": 'package gol\nimport "C"\n',
        }
        for path in ("contrib/go/_std_1.26/src/syscall", "contrib/go/_std_1.26/src/runtime/cgo"):
            files[f"{path}/ya.make"] = "GO_LIBRARY()\nSRCS(x.go)\nEND()\n"
            files[f"{path}/x.go"] = "package x\n"
        for path in (
            "build/internal/platform/clang_toolchain_info",
            "build/platform/lld",
            "build/external_resources/go_tools",
            "build/external_resources/yolint",
        ):
            files[f"{path}/ya.make"] = "RESOURCES_LIBRARY()\nEND()\n"
        graph = lib.make(files, "gol")
        copy = lib.node_by_output(graph, "$(B)/gol/b.c")
        self.assertEqual(copy["kv"]["p"], "CP")
        self.assertIn("$(S)/gol/b.c", copy["inputs"])
        cgo = lib.node_by_output(graph, "$(B)/gol/c.cgo1.go")
        self.assertEqual(cgo["outputs"], [
            "$(B)/gol/c.cgo1.go",
            "$(B)/gol/c.cgo2.c",
            "$(B)/gol/_cgo_export.h",
            "$(B)/gol/_cgo_export.c",
            "$(B)/gol/_cgo_gotypes.go",
            "$(B)/gol/_cgo_main.c",
        ])
        self.assertIn("$(S)/gol/c.go", cgo["inputs"])
        for output in ("$(B)/gol/b.c.o", "$(B)/gol/c.cgo2.c.o", "$(B)/gol/_cgo_export.c.o"):
            lib.node_by_output(graph, output)

    def test_cython_and_swig_sources(self):
        files = {
            "cy/ya.make": f"LIBRARY()\n{BARE}BUILDWITH_CYTHON_CPP(cy.pyx)\nEND()\n",
            "cy/cy.pyx": "",
            "sw/ya.make": "PY3_LIBRARY()\nPY_SRCS(SWIG_C sw.swg)\nEND()\n",
            "sw/sw.swg": "%module sw\n",
            "contrib/libs/python/ya.make": LIBRARY_STUB,
            "contrib/tools/cython/Cython/ya.make": LIBRARY_STUB,
            "library/cpp/resource/ya.make": LIBRARY_STUB,
        }
        for name in CYTHON_UTILITY:
            files[f"contrib/tools/cython/Cython/Utility/{name}"] = ""
        for path in CYTHON_HEADERS:
            files[path] = ""
        for name in SWIG_LIBRARY:
            files[f"contrib/tools/swig/Lib/{name}"] = ""
        with_tools(
            files,
            "contrib/tools/swig",
            "tools/archiver",
            "tools/py3cc",
            "tools/py3cc/slow",
            "tools/rescompiler",
            "tools/rescompressor",
        )
        cython = lib.make(files, "cy")
        node = lib.node_by_output(cython, "$(B)/cy/cy.pyx.cpp")
        self.assertEqual(node["kv"]["p"], "CY")
        self.assertEqual(node["cmds"][0]["cmd_args"][-3:], [
            "$(S)/cy/cy.pyx", "-o", "$(B)/cy/cy.pyx.cpp",
        ])
        self.assertIn(
            "$(B)/cy/cy.pyx.cpp.o",
            lib.node_by_output(cython, "$(B)/cy/libcy.a")["inputs"],
        )

        swig = lib.make(files, "sw")
        node = lib.node_by_output(swig, "$(B)/sw/sw.swg.c")
        self.assertEqual(node["kv"]["p"], "SW")
        self.assertEqual(node["outputs"], ["$(B)/sw/sw.swg.c", "$(B)/sw/sw.py"])
        self.assertIn(
            "$(B)/sw/sw.swg.c.o",
            lib.node_by_output(swig, "$(B)/sw/libpy3sw.a")["inputs"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
