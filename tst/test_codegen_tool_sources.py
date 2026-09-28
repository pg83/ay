import unittest

import lib


class ToolDrivenSourcesTest(unittest.TestCase):
    def test_asp_sc_sfdl_and_fml_sources(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(page.asp scheme.sc calc.sfdl model.fml use.cpp)\nEND()\n"
            ),
            "mod/page.asp": '#include "inc.h"\n<html/>\n',
            "mod/inc.h": "",
            "mod/scheme.sc": "struct X {};\n",
            "mod/calc.sfdl": '#include "sfinc.h"\n',
            "mod/sfinc.h": "",
            "mod/model.fml": "",
            "mod/use.cpp": (
                '#include "scheme.sc.h"\n#include "calc"\n#include "model.fml.inc"\n'
            ),
            "library/cpp/domscheme/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"
            ),
            "library/cpp/domscheme/runtime.h": '#include "rt2.h"\n',
            "library/cpp/domscheme/rt2.h": "",
        }
        lib.tool_program(files, "tools/html2cpp", "html2cpp")
        lib.tool_program(files, "tools/domschemec", "domschemec")
        lib.tool_program(files, "tools/calcstaticopt", "calcstaticopt")
        lib.tool_program(files, "tools/relev_fml_codegen", "relev_fml_codegen")
        graph = lib.make(files, "mod")

        def tool_uid(path):
            return lib.node_by_output(graph, path)["uid"]

        html = lib.node_by_output(graph, "$(B)/mod/page.asp.cpp")
        self.assertEqual(html["kv"], {"p": "HT", "pc": "yellow"})
        self.assertEqual(html["cmds"][0]["cmd_args"], [
            "$(B)/tools/html2cpp/html2cpp", "$(S)/mod/page.asp", "$(B)/mod/page.asp.cpp",
        ])
        self.assertEqual(html["inputs"], [
            "$(B)/tools/html2cpp/html2cpp", "$(S)/mod/page.asp", "$(S)/mod/inc.h",
        ])
        self.assertEqual(html["foreign_deps"], {"tool": [tool_uid("$(B)/tools/html2cpp/html2cpp")]})
        html_compile = lib.node_by_output(graph, "$(B)/mod/page.asp.cpp.o")
        self.assertEqual(sorted(html_compile["inputs"]), sorted([
            "$(B)/mod/page.asp.cpp", "$(S)/mod/inc.h", "$(S)/mod/page.asp",
        ]))
        self.assertEqual(html_compile["deps"], [html["uid"]])

        scheme = lib.node_by_output(graph, "$(B)/mod/scheme.sc.h")
        self.assertEqual(scheme["kv"], {"p": "SC", "pc": "yellow"})
        self.assertEqual(scheme["cmds"][0]["cmd_args"], [
            "$(B)/tools/domschemec/domschemec",
            "--in", "$(S)/mod/scheme.sc", "--out", "$(B)/mod/scheme.sc.h",
        ])
        self.assertEqual(scheme["inputs"], [
            "$(B)/tools/domschemec/domschemec",
            "$(S)/mod/scheme.sc",
            "$(S)/library/cpp/domscheme/runtime.h",
            "$(S)/library/cpp/domscheme/rt2.h",
        ])

        calc = lib.node_by_output(graph, "$(B)/mod/calc")
        self.assertEqual(calc["kv"], {"p": "SF", "pc": "yellow"})
        self.assertEqual(calc["outputs"], ["$(B)/mod/calc.sfdl.tmp", "$(B)/mod/calc"])
        self.assertEqual(calc["cmds"][0]["cmd_args"][-8:], [
            "-E", "-C", "-x", "c++", "-Qunused-arguments",
            "-o", "$(B)/mod/calc.sfdl.tmp", "$(S)/mod/calc.sfdl",
        ])
        self.assertEqual(calc["cmds"][1]["cmd_args"], [
            "$(B)/tools/calcstaticopt/calcstaticopt",
            "-i", "$(B)/mod/calc.sfdl.tmp", "-a", "$(S)",
        ])
        self.assertEqual(calc["cmds"][1]["stdout"], "$(B)/mod/calc")
        self.assertEqual(calc["inputs"], [
            "$(B)/tools/calcstaticopt/calcstaticopt", "$(S)/mod/calc.sfdl",
        ])

        fml = lib.node_by_output(graph, "$(B)/mod/model.fml.inc")
        self.assertEqual(fml["kv"], {"p": "FM", "pc": "yellow"})
        self.assertEqual(fml["cmds"][0]["cmd_args"], [
            "$(B)/tools/relev_fml_codegen/relev_fml_codegen",
            "-b", "-o", "$(B)/mod/model.fml.inc", "-T", "$(S)/mod/model.fml",
        ])

        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        for path in (
            "$(B)/mod/scheme.sc.h", "$(S)/mod/scheme.sc",
            "$(S)/library/cpp/domscheme/runtime.h",
            "$(B)/mod/calc", "$(B)/mod/calc.sfdl.tmp", "$(S)/mod/calc.sfdl",
            "$(B)/mod/model.fml.inc", "$(S)/mod/model.fml",
        ):
            self.assertIn(path, use["inputs"])
        self.assertEqual(sorted(use["deps"]), sorted([scheme["uid"], calc["uid"], fml["uid"]]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
