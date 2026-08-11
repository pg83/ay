import unittest

import lib


def only_kind(graph, kind):
    nodes = [node for node in graph["graph"] if node.get("kv", {}).get("p") == kind]
    if len(nodes) != 1:
        raise AssertionError(f"expected one {kind} node, got {len(nodes)}")
    return nodes[0]


class EmitBaseCodegenTest(unittest.TestCase):
    def test_generated_closure_reaches_consumer(self):
        files = {
            "lib/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "BASE_CODEGEN(tool base_gen)\n"
                "SRCS(GLOBAL ${BINDIR}/base_gen.cpp GLOBAL use.cpp)\n"
                "END()\n"
            ),
            "lib/base_gen.in": "// base codegen input\n",
            "lib/use.cpp": '#include <lib/base_gen.h>\nint use() { return 0; }\n',
        }
        lib.tool_program(files, "tool", "base_gen")
        graph = lib.make(files, "lib")
        compile_node = lib.node_by_output(graph, "$(B)/lib/use.cpp.o")
        self.assertIn("$(B)/lib/base_gen.cpp", compile_node["inputs"])
        self.assertIn("$(S)/lib/base_gen.in", compile_node["inputs"])

    def test_tool_reachability_keeps_hidden_split_codegen(self):
        files = {
            "tool/ya.make": (
                "PROGRAM(tool)\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "PEERDIR(hidden/factors)\n"
                "SRCS(main.cpp)\n"
                "END()\n"
            ),
            "tool/main.cpp": "int main(){return 0;}\n",
            "hidden/factors/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(GLOBAL ${BINDIR}/factors_gen.cpp)\n"
                "SPLIT_CODEGEN(tool2 factors_gen NHidden)\n"
                "END()\n"
            ),
            "hidden/factors/factors_gen.in": "// codegen input\n",
            "consumer/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "BASE_CODEGEN(tool fill_factors)\n"
                "SRCS(GLOBAL ${BINDIR}/fill_factors.cpp)\n"
                "END()\n"
            ),
            "consumer/fill_factors.in": "// base codegen input\n",
        }
        lib.tool_program(files, "tool2", "tool2")
        graph = lib.make(files, "consumer")
        base_codegen = only_kind(graph, "BC")
        self.assertEqual(base_codegen["kv"]["pc"], "yellow")
        self.assertEqual(base_codegen["outputs"], [
            "$(B)/consumer/fill_factors.cpp",
            "$(B)/consumer/fill_factors.h",
        ])
        self.assertEqual(base_codegen["cmds"][0]["cmd_args"], [
            "$(B)/tool/tool",
            "$(S)/consumer/fill_factors.in",
            "$(B)/consumer/fill_factors.cpp",
            "$(B)/consumer/fill_factors.h",
        ])
        tool = lib.node_by_output(graph, "$(B)/tool/tool")
        self.assertIn("$(B)/tool/tool", base_codegen["inputs"])
        self.assertIn(tool["uid"], base_codegen["deps"])

        split = lib.node_by_output(graph, "$(B)/hidden/factors/factors_gen.h")
        self.assertEqual(split["kv"]["p"], "SC")
        compile_node = lib.node_by_output(
            graph,
            "$(B)/hidden/factors/factors_gen.0.cpp.pic.o",
        )
        self.assertIn(split["uid"], compile_node["deps"])

    def test_struct_codegen_producer_and_output_closure(self):
        files = {
            "kernel/struct_codegen/metadata/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(metadata.cpp)\nEND()\n"
            ),
            "kernel/struct_codegen/metadata/metadata.cpp": (
                "int metadata(){return 0;}\n"
            ),
            "kernel/struct_codegen/reflection/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(reflection.cpp)\nEND()\n"
            ),
            "kernel/struct_codegen/reflection/reflection.cpp": (
                "int reflection(){return 0;}\n"
            ),
            "kernel/struct_codegen/reflection/reflection.h": "#pragma once\n",
            "kernel/struct_codegen/reflection/floats.h": "#pragma once\n",
            "lib/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "STRUCT_CODEGEN(gen)\n"
                "SRCS(use.cpp)\n"
                "END()\n"
            ),
            "lib/gen.in": "// struct codegen input\n",
            "lib/use.cpp": '#include <lib/gen.h>\nint use(){return 0;}\n',
            "app/ya.make": (
                "PROGRAM()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "PEERDIR(lib)\n"
                "SRCS(main.cpp)\n"
                "END()\n"
            ),
            "app/main.cpp": "int main(){return 0;}\n",
        }
        lib.tool_program(files, "kernel/struct_codegen/codegen_tool", "codegen_tool")
        for header in (
            "util/generic/singleton.h",
            "util/generic/strbuf.h",
            "util/generic/vector.h",
            "util/generic/ptr.h",
            "util/generic/yexception.h",
        ):
            files[header] = "#pragma once\n"

        graph = lib.make(files, "app")
        base_codegen = only_kind(graph, "BC")
        self.assertEqual(base_codegen["outputs"], [
            "$(B)/lib/gen.cpp",
            "$(B)/lib/gen.h",
        ])
        self.assertEqual(base_codegen["cmds"][0]["cmd_args"], [
            "$(B)/kernel/struct_codegen/codegen_tool/codegen_tool",
            "$(S)/lib/gen.in",
            "$(B)/lib/gen.cpp",
            "$(B)/lib/gen.h",
        ])
        tool = lib.node_by_output(
            graph,
            "$(B)/kernel/struct_codegen/codegen_tool/codegen_tool",
        )
        self.assertIn(tool["uid"], base_codegen["deps"])

        compile_node = lib.node_by_output(graph, "$(B)/lib/use.cpp.o")
        for expected in (
            "$(S)/util/generic/singleton.h",
            "$(S)/kernel/struct_codegen/reflection/reflection.h",
            "$(S)/kernel/struct_codegen/reflection/floats.h",
        ):
            self.assertIn(expected, compile_node["inputs"])
        lib.node_by_output(
            graph,
            "$(B)/kernel/struct_codegen/metadata/metadata.cpp.o",
        )
        lib.node_by_output(
            graph,
            "$(B)/kernel/struct_codegen/reflection/reflection.cpp.o",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
