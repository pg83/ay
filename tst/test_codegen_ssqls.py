import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class SsqlsTest(unittest.TestCase):
    def test_generated_cpp_and_header_keep_tool_induced_dependencies(self):
        files = {
            "mod/ya.make": f"LIBRARY()\n{BARE}SRCS(tables/data.ssqls use.cpp)\nEND()\n",
            "mod/tables/data.ssqls": "<DbObject/>\n",
            "mod/use.cpp": '#include "mod/tables/data.h"\n',
            "metrika/core/tools/ssqls/templates/table.h": '#include "detail.h"\n',
            "metrika/core/tools/ssqls/templates/detail.h": "#pragma once\n",
            "metrika/core/tools/ssqls/templates/table.cpp": "int generated;\n",
        }
        lib.tool_program(files, "metrika/core/tools/ssqls", "ssqls")
        files["metrika/core/tools/ssqls/ya.make"] = files["metrika/core/tools/ssqls/ya.make"].replace(
            "END()", "INDUCED_DEPS(h ${ARCADIA_ROOT}/metrika/core/tools/ssqls/templates/table.h)\n"
            "INDUCED_DEPS(cpp ${ARCADIA_ROOT}/metrika/core/tools/ssqls/templates/table.cpp)\nEND()"
        )
        graph = lib.make(files, "mod")
        generate = lib.node_by_output(graph, "$(B)/mod/tables/data.cpp")
        self.assertEqual(generate["kv"], {"p": "SS", "pc": "yellow"})
        self.assertEqual(generate["outputs"], ["$(B)/mod/tables/data.cpp", "$(B)/mod/tables/data.h"])
        self.assertEqual(generate["cmds"][0]["cmd_args"], [
            "$(B)/metrika/core/tools/ssqls/ssqls", "$(S)/mod/tables/data.ssqls", "-S", "$(S)", "-B", "$(B)",
        ])
        cpp = lib.node_by_output(graph, "$(B)/mod/_/tables/data.cpp.o")
        header = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn("$(S)/metrika/core/tools/ssqls/templates/table.cpp", cpp["inputs"])
        self.assertIn("$(S)/metrika/core/tools/ssqls/templates/table.h", cpp["inputs"])
        self.assertIn("$(B)/mod/tables/data.h", cpp["inputs"])
        self.assertIn("$(S)/metrika/core/tools/ssqls/templates/table.h", header["inputs"])
        self.assertIn("$(S)/metrika/core/tools/ssqls/templates/detail.h", header["inputs"])
        self.assertIn(generate["uid"], cpp["deps"])
        self.assertIn(generate["uid"], header["deps"])

    def test_xml_inputs_and_header_includes_remain_separate(self):
        files = {
            "mod/ya.make": f"LIBRARY()\n{BARE}RUN_PYTHON3(gen.py OUT_NOAUTO generated.h OUTPUT_INCLUDES mod/generated-runtime.h)\nSRCS(tables/data.ssqls tables/base.ssqls)\nEND()\n",
            "mod/tables/data.ssqls": (
                '<DbObject><group><include path="fragment.ssqls">&lt;mod/api.h&gt;</include>'
                '<include path="">&quot;mod/api.h&quot;</include>'
                '<include path="$S/mod/tables/root.ssqls">&quot;$S/mod/explicit.h&quot;</include>'
                '<include>&quot;$B/mod/generated.h&quot;</include></group>'
                '<ancestors><ancestor path="base.ssqls"/><ancestor/></ancestors></DbObject>\n'
            ),
            "mod/tables/base.ssqls": '<DbObject><include>&quot;mod/base.h&quot;</include></DbObject>\n',
            "mod/tables/fragment.ssqls": '<DbObject><include path="nested.ssqls">mod/fragment.h</include></DbObject>\n',
            "mod/tables/nested.ssqls": '<DbObject/>\n',
            "mod/tables/root.ssqls": '<DbObject/>\n',
            "mod/explicit.h": "", "mod/gen.py": "", "mod/generated-runtime.h": "",
            "mod/api.h": '#include "detail.h"\n', "mod/detail.h": "",
            "mod/base.h": "", "mod/fragment.h": "",
        }
        lib.tool_program(files, "metrika/core/tools/ssqls", "ssqls")
        graph = lib.make(files, "mod")
        data = lib.node_by_output(graph, "$(B)/mod/tables/data.cpp")
        self.assertEqual(sorted(data["inputs"]), sorted([
            "$(B)/metrika/core/tools/ssqls/ssqls", "$(S)/mod/tables/data.ssqls",
            "$(S)/mod/tables/fragment.ssqls", "$(S)/mod/tables/nested.ssqls",
            "$(S)/mod/tables/base.ssqls", "$(S)/mod/tables/root.ssqls",
        ]))
        compiled = lib.node_by_output(graph, "$(B)/mod/_/tables/data.cpp.o")
        for path in ("$(B)/mod/tables/data.h", "$(B)/mod/tables/base.h",
                     "$(S)/mod/api.h", "$(S)/mod/detail.h", "$(S)/mod/base.h",
                     "$(S)/mod/explicit.h", "$(B)/mod/generated.h", "$(S)/mod/generated-runtime.h"):
            self.assertIn(path, compiled["inputs"])
            self.assertEqual(compiled["inputs"].count(path), 1)
        for path in ("$(S)/mod/tables/data.ssqls", "$(S)/mod/tables/fragment.ssqls",
                     "$(S)/mod/tables/nested.ssqls", "$(S)/mod/tables/root.ssqls", "$(S)/mod/fragment.h"):
            self.assertNotIn(path, compiled["inputs"])
        base = lib.node_by_output(graph, "$(B)/mod/tables/base.cpp")
        self.assertIn(base["uid"], compiled["deps"])
        generated = lib.node_by_output(graph, "$(B)/mod/generated.h")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertNotIn(generated["uid"], data["deps"])

    def test_xml_selectors_match_exact_names_and_nested_includes(self):
        files = {
            "mod/ya.make": f"LIBRARY()\n{BARE}SRCS(tables/data.ssqls)\nEND()\n",
            "mod/tables/data.ssqls": (
                '<DbObject xmlns:ns="urn:prefix"><include path="outer.ssqls">mod/outer.h'
                '<include path="inner.ssqls">mod/inner.h</include>ignored</include>'
                '<ns:include path="wrong.ssqls">mod/wrong.h</ns:include>'
                '<include ns:path="wrong.ssqls" path="good.ssqls">mod/api.h</include>'
                '<ancestors><ancestor ns:path="wrong.ssqls"/><ns:ancestor path="wrong.ssqls"/></ancestors>'
                '<ns:ancestors><ancestor path="wrong.ssqls"/></ns:ancestors>'
                '<group xmlns="urn:default"><include>mod/default.h</include></group>'
                '<include><![CDATA[mod/cdata.h]]></include></DbObject>\n'
            ),
            "mod/tables/outer.ssqls": "<DbObject/>",
            "mod/tables/inner.ssqls": "<DbObject/>",
            "mod/tables/good.ssqls": "<DbObject/>",
            "mod/outer.h": "", "mod/inner.h": "", "mod/api.h": "",
            "mod/default.h": "", "mod/cdata.h": "",
        }
        lib.tool_program(files, "metrika/core/tools/ssqls", "ssqls")
        graph = lib.make(files, "mod")
        generator = lib.node_by_output(graph, "$(B)/mod/tables/data.cpp")
        for name in ("outer", "inner", "good"):
            self.assertIn("$(S)/mod/tables/" + name + ".ssqls", generator["inputs"])
        compiled = lib.node_by_output(graph, "$(B)/mod/_/tables/data.cpp.o")
        for name in ("outer", "inner", "api", "default", "cdata"):
            self.assertIn("$(S)/mod/" + name + ".h", compiled["inputs"])

    def test_source_directory_and_rooted_input_use_source_based_output_paths(self):
        for setup, input_path in (("SRCDIR(schemas)\n", "data.ssqls"),
                                  ("", "${ARCADIA_ROOT}/schemas/data.ssqls")):
            with self.subTest(input_path=input_path):
                files = {
                    "mod/ya.make": f"LIBRARY()\n{BARE}" + setup + "SRCS(" + input_path + ")\nEND()\n",
                    "schemas/data.ssqls": '<DbObject><include>mod/api.h</include></DbObject>\n',
                    "mod/api.h": "",
                }
                lib.tool_program(files, "metrika/core/tools/ssqls", "ssqls")
                graph = lib.make(files, "mod")
                generated = lib.node_by_output(graph, "$(B)/schemas/data.h")
                self.assertEqual(generated["outputs"], ["$(B)/schemas/data.cpp", "$(B)/schemas/data.h"])
                compiled = lib.node_by_output(graph, "$(B)/mod/__/schemas/data.cpp.o")
                self.assertIn("$(B)/schemas/data.h", compiled["inputs"])
                self.assertIn("$(S)/mod/api.h", compiled["inputs"])
                self.assertIn(generated["uid"], compiled["deps"])

    def test_generated_input_uses_producer_without_parsing_source_bytes(self):
        statements = [
            "RUN_PYTHON3(gen.py OUT_NOAUTO tables/data.ssqls)",
            "SRCS(${ARCADIA_BUILD_ROOT}/mod/tables/data.ssqls)",
        ]
        for order in (statements, list(reversed(statements))):
            with self.subTest(order=order):
                files = {
                    "mod/ya.make": f"LIBRARY()\n{BARE}" + "\n".join(order) + "\nEND()\n",
                    "mod/gen.py": "",
                }
                lib.tool_program(files, "metrika/core/tools/ssqls", "ssqls")
                graph = lib.make(files, "mod")
                input_node = lib.node_by_output(graph, "$(B)/mod/tables/data.ssqls")
                generator = lib.node_by_output(graph, "$(B)/mod/tables/data.cpp")
                self.assertIn(input_node["uid"], generator["deps"])
                self.assertIn("$(B)/mod/tables/data.ssqls", generator["inputs"])
                self.assertNotIn("$(S)/mod/tables/data.ssqls", generator["inputs"])
                compiled = lib.node_by_output(graph, "$(B)/mod/_/tables/data.cpp.o")
                self.assertEqual(compiled["inputs"], ["$(B)/mod/tables/data.cpp"])
                self.assertEqual(compiled["deps"], [generator["uid"]])

    def test_missing_source_is_reported(self):
        files = {"mod/ya.make": f"LIBRARY()\n{BARE}SRCS(missing.ssqls)\nEND()\n"}
        lib.tool_program(files, "metrika/core/tools/ssqls", "ssqls")
        result = lib.make_process(files, "mod")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SSQLS requires an existing source", result.stderr)

    def test_malformed_xml_is_reported(self):
        for text in ("<DbObject><include></DbObject>", "<DbObject><include>",
                     '<DbObject attr="unterminated>', ""):
            with self.subTest(text=text):
                files = {
                    "mod/ya.make": f"LIBRARY()\n{BARE}SRCS(data.ssqls)\nEND()\n",
                    "mod/data.ssqls": text,
                }
                lib.tool_program(files, "metrika/core/tools/ssqls", "ssqls")
                result = lib.make_process(files, "mod")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("XML", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
