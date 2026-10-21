import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
TOOL = "library/cpp/regex/pire/inline"


class PireInlineTest(unittest.TestCase):
    def test_inline_sources_have_host_tool_and_parsed_generator_inputs(self):
        files = {
            "mod/ya.make": "LIBRARY()\n" + BARE + "PIRE_INLINE(alpha.cpp sub/beta.cc)\nEND()\n",
            "mod/alpha.cpp": '#include "dep.h"\n',
            "mod/sub/beta.cc": '#include "../dep.h"\n',
            "mod/dep.h": '#include "nested.h"\n',
            "mod/nested.h": "",
        }
        lib.tool_program(files, TOOL, "inline")
        graph = lib.make(files, "mod")
        tool = lib.node_by_output(graph, "$(B)/" + TOOL + "/inline")
        for source, output, object_path in (
            ("mod/alpha.cpp", "mod/alpha.cpp", "mod/alpha.cpp.o"),
            ("mod/sub/beta.cc", "mod/_/sub/beta.cc", "mod/_/_/sub/beta.cc.o"),
        ):
            with self.subTest(source=source):
                producer = lib.node_by_output(graph, "$(B)/" + output)
                self.assertEqual(producer["kv"], {"p": "PI", "pc": "yellow"})
                self.assertEqual(producer["cmds"][0]["cmd_args"], [
                    "$(B)/" + TOOL + "/inline", "-o", "$(B)/" + output, "$(S)/" + source,
                ])
                self.assertEqual(producer["foreign_deps"], {"tool": [tool["uid"]]})
                self.assertEqual(sorted(producer["inputs"]), sorted([
                    "$(B)/" + TOOL + "/inline", "$(S)/" + source,
                    "$(S)/mod/dep.h", "$(S)/mod/nested.h",
                ]))
                compile_node = lib.node_by_output(graph, "$(B)/" + object_path)
                self.assertEqual(compile_node["inputs"], ["$(B)/" + output])
                self.assertEqual(compile_node["deps"], [producer["uid"]])
        self.assertEqual(tool["platform"], "default-linux-x86_64")

    def test_direct_macro_uses_source_directory_output_namespace(self):
        files = {
            "mod/ya.make": "LIBRARY()\n" + BARE + "SRCDIR(shared)\nPIRE_INLINE_CMD(alpha.cpp)\nEND()\n",
            "shared/alpha.cpp": '#include "dep.h"\n',
            "shared/dep.h": "",
        }
        lib.tool_program(files, TOOL, "inline")
        graph = lib.make(files, "mod")
        producer = lib.node_by_output(graph, "$(B)/mod/__/shared/alpha.cpp")
        self.assertEqual(producer["cmds"][0]["cmd_args"][-1], "$(S)/shared/alpha.cpp")
        self.assertIn("$(S)/shared/dep.h", producer["inputs"])
        compile_node = lib.node_by_output(graph, "$(B)/mod/__/shared/alpha.cpp.o")
        self.assertEqual(compile_node["inputs"], ["$(B)/mod/__/shared/alpha.cpp"])

    def test_empty_foreach_does_not_generate_a_tool(self):
        graph = lib.make({"mod/ya.make": "LIBRARY()\n" + BARE + "PIRE_INLINE()\nEND()\n"}, "mod")
        self.assertFalse(any(n.get("kv", {}).get("p") == "PI" for n in graph["graph"]))

    def test_direct_macro_argument_validation(self):
        for args in ("", "alpha.cpp beta.cpp"):
            with self.subTest(args=args):
                result = lib.make_process({
                    "mod/ya.make": "LIBRARY()\n" + BARE + "PIRE_INLINE_CMD(" + args + ")\nEND()\n",
                }, "mod")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("expects", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
