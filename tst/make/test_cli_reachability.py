"""Exercise CLI reachability; the canonical graph gate owns upstream parity."""
import unittest

from tst import lib
from tst.make.test_foreign_tool_binding import fixture as foreign_files
from tst.make.test_proto_flatc import flatc_files
from tst.make.test_proto_gztproto import gazetteer_files
from tst.make.test_proto_py2_context import proto_world
from tst.make.test_python_srcs import python_base


class CliReachabilityTest(unittest.TestCase):
    def test_generators_collected_before_the_module_declaration(self):
        commands = (
            "RUN_JAVA_PROGRAM(Main generated.h OUT generated.h CLASSPATH tools/java)",
            "FROM_SANDBOX(FILE 1 OUT_NOAUTO blob.dat)",
            "JOIN_SRCS(combined.cpp first.cpp second.cpp)",
            "ARCHIVE_ASM(NAME payload.inc DONTCOMPRESS data.dat)",
            "GENERATE_ENUM_SERIALIZATION(enums.h)",
            "CUDA_SRCS(kernel.cu)",
            "SPLIT_CODEGEN(tools/codegen gen OUT_NUM 1)",
            "USE_LLVM_BC20()\nLLVM_BC(main.cpp NAME foo)",
        )
        for command in commands:
            with self.subTest(command=command):
                files = {
                    "a/ya.make": command + "\nLIBRARY()\n" + lib.BARE_MODULE + "SRCS(main.cpp)\nEND()\n",
                    "a/main.cpp": "", "a/first.cpp": "", "a/second.cpp": "",
                    "a/data.dat": "payload", "a/enums.h": "enum E { A };\n",
                    "a/kernel.cu": "", "a/gen.in": "",
                    "library/cpp/resource/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "END()\n",
                }
                for tool in ("tools/rescompiler", "tools/rescompressor", "tools/codegen"):
                    lib.tool_program(files, tool, tool.rsplit("/", 1)[1])
                graph = lib.make(files, "a", "-k", "--target-platform", "default-linux-x86_64")
                self.assertIsInstance(graph["graph"], list)

    def test_explicit_schema_sources_in_a_python2_proto_context(self):
        for extension in ("proto", "ev", "gztproto"):
            with self.subTest(extension=extension):
                files = proto_world()
                files["p/ya.make"] = (
                    "PROTO_LIBRARY()\n" + lib.BARE_MODULE
                    + f"ONLY_TAGS(PY_PROTO)\nSRC(a.{extension})\nEND()\n"
                )
                files[f"p/a.{extension}"] = "message A {}\n"
                files["library/cpp/eventlog/proto/ya.make"] = (
                    "PROTO_LIBRARY()\n" + lib.BARE_MODULE + "ONLY_TAGS(PY_PROTO)\nEND()\n"
                )
                if extension == "gztproto":
                    files.update({path: body for path, body in gazetteer_files("").items()
                                  if path.startswith(("kernel/", "dict/"))})
                graph = lib.make(files, "py", "--target-platform", "default-linux-x86_64")
                self.assertTrue(any(node["kv"].get("p") == "PB" for node in graph["graph"]))

    def test_python_group_in_a_dll_descent_with_peer_tests(self):
        files = python_base()
        files.update({
            "p/ya.make": "DLL(p)\n" + lib.BARE_MODULE
                         + "NO_PYTHON_INCLUDES()\nPY_SRCS(note.py)\nSRCS(main.cpp)\nEND()\n",
            "p/note.py": "value = 1\n",
            "p/main.cpp": "int value;\n",
        })
        lib.tool_program(files, "tools/fix_elf", "fix_elf")
        graph = lib.make(files, "p", "-t", "-DADD_PEERDIRS_GEN_TESTS=yes")
        self.assertTrue(graph["result"])

    def test_python_group_disabled_by_go_proto_in_a_cpp_program(self):
        files = python_base()
        files.update({
            "p/ya.make": "PROGRAM()\n" + lib.BARE_MODULE
                         + "SET(GO_PROTO yes)\nPY_SRCS(note.py)\nSRCS(main.cpp)\nEND()\n",
            "p/note.py": "value = 1\n",
            "p/main.cpp": "int main() {}\n",
        })
        self.assertTrue(lib.make(files, "p")["result"])

    def test_gazetteer_python_source_outside_a_proto_module(self):
        files = python_base()
        files.update({
            "p/ya.make": "PY3_LIBRARY()\n" + lib.BARE_MODULE + "PY_SRCS(note.gztproto)\nEND()\n",
            "p/note.gztproto": "message Note {}\n",
        })
        self.assertIsInstance(lib.make(files, "p")["graph"], list)

    def test_test_data_activates_a_planned_producer_after_tool_binding(self):
        files = {
            "test/ya.make": "EXECTEST()\nRUN(true)\nDATA(sbr://123)\nEND()\n",
            "provider/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                                + "RUN_PROGRAM(tools/emitter OUT_NOAUTO "
                                  "${ARCADIA_BUILD_ROOT}/sandbox-storage/123/resource)\nEND()\n",
        }
        lib.tool_program(files, "tools/emitter", "emitter")
        result = lib.make_process(files, "provider", "test", "-t")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("pending node(s) form a dependency cycle", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_weak_header_reaches_an_uncomposed_fbs64_binding(self):
        files = lib.java_tree(**flatc_files())
        files.update({
            "f/ya.make": "FBS_LIBRARY()\n" + lib.BARE_MODULE
                         + "ONLY_TAGS(JAVA_FBS)\nSRCS(a.fbs64)\nEND()\n",
            "f/a.fbs64": "table A {}\n",
            "contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make": "JAVA_CONTRIB()\nEND()\n",
            "holder/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(local.cpp public.h)\nEND()\n",
            "holder/local.cpp": "int local;\n",
            "holder/public.h": '#include "f/a.fbs64.h"\n',
            "app/ya.make": "PROGRAM()\n" + lib.BARE_MODULE + "PEERDIR(holder)\nSRCS(main.cpp)\nEND()\n",
            "app/main.cpp": "int main() {}\n",
        })
        result = lib.make_process(files, "app", "f", "-k")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("pending node(s) form a dependency cycle", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_weak_peer_headers_activate_generated_include_metadata(self):
        for kind in ("enum", "typescript"):
            with self.subTest(kind=kind):
                files = foreign_files()
                if kind == "enum":
                    declaration = "ALICE_GENERATE_NOEXCEPT_ENUM_SERIALIZATION(types.h)\n"
                    header = "types.h_serialized.h"
                    files["grove/generated/types.h"] = "enum E { A };\n"
                    tool = "yandex_io/tools/enum_serializer_generator"
                    lib.tool_program(files, tool, "enum_serializer_generator")
                    files[tool + "/ya.make"] = files[tool + "/ya.make"].replace(
                        "END()", "INDUCED_DEPS(h ${ARCADIA_ROOT}/runtime/dep.h)\nEND()")
                    files["runtime/dep.h"] = ""
                else:
                    declaration = "CONFIGURE_FILE(template.ts.in codegen.h)\n"
                    header = "codegen.h"
                    files["grove/generated/template.ts.in"] = 'import {x} from "./dep";\n'
                    files["grove/generated/dep.ts"] = "export const x = 1;\n"
                files["grove/generated/ya.make"] = (
                    "LIBRARY()\n" + lib.BARE_MODULE + declaration + "SRCS(standalone.cpp)\nEND()\n"
                )
                files["grove/holder/public.h"] = f'#include "grove/generated/{header}"\n'
                self.assertTrue(lib.make(files, "grove/app", "-k")["result"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
