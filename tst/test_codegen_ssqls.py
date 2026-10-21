import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class SsqlsTest(unittest.TestCase):
    def test_generated_cpp_and_header_keep_tool_induced_dependencies(self):
        files = {
            "mod/ya.make": f"LIBRARY()\n{BARE}SRCS(tables/data.ssqls use.cpp)\nEND()\n",
            "mod/tables/data.ssqls": "table data\n",
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
        self.assertNotIn("$(S)/metrika/core/tools/ssqls/templates/table.h", cpp["inputs"])
        self.assertIn("$(S)/metrika/core/tools/ssqls/templates/table.h", header["inputs"])
        self.assertIn("$(S)/metrika/core/tools/ssqls/templates/detail.h", header["inputs"])
        self.assertIn(generate["uid"], cpp["deps"])
        self.assertIn(generate["uid"], header["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
