import unittest

import lib
from test_go_package import go_world


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class ModuleHeaderDependenciesTest(unittest.TestCase):
    def test_unused_listed_header_is_not_a_compilation_input(self):
        graph = lib.make({
            "mod/ya.make": f"LIBRARY()\n{BARE}SRCS(local.cpp unused.h)\nEND()\n",
            "mod/local.cpp": "int local;\n",
            "mod/unused.h": '#include "missing.h"\n',
        }, "mod")
        archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertNotIn("$(S)/mod/unused.h", archive["inputs"])

    def test_cached_header_diagnostics_are_reported_when_compilation_uses_it(self):
        for order in ("public.h local.cpp", "local.cpp public.h"):
            with self.subTest(order=order):
                result = lib.make_process({
                    "mod/ya.make": f"LIBRARY()\n{BARE}SRCS({order})\nEND()\n",
                    "mod/local.cpp": '#include "public.h"\nint local;\n',
                    "mod/public.h": '#include "cycle.h"\n',
                    "mod/cycle.h": '#include "public.h"\n#include "missing.h"\n',
                }, "mod")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("missing-include", result.stderr)
                self.assertIn('"missing.h"', result.stderr)

    def test_unused_header_retains_generated_dependencies_without_reporting_missing_includes(self):
        files = {
            "producer/ya.make": (
                f"LIBRARY()\n{BARE}"
                "GEN_PCH_FILE(producer/private.h ${ARCADIA_BUILD_ROOT}/producer/private.h.pch)\n"
                "SRCS(public.h local.cpp)\nEND()\n"
            ),
            "producer/local.cpp": "int local;\n",
            "producer/public.h": '#include "producer/private.h.pch"\n#include "missing.h"\n',
            "producer/private.h": "#pragma once\n",
            "consumer/ya.make": f"PROGRAM()\n{BARE}PEERDIR(producer)\nSRCS(main.cpp)\nEND()\n",
            "consumer/main.cpp": "int main() {}\n",
        }
        lib.tool_program(files, "sdg/tier0/tools/make_pch_wrapper", "make_pch_wrapper")
        graph = lib.make(files, "consumer")
        pch = lib.node_by_output(graph, "$(B)/producer/private.h.pch")
        link = lib.node_by_output(graph, "$(B)/consumer/consumer")
        self.assertIn("$(B)/producer/private.h.pch", link["inputs"])
        self.assertIn(pch["uid"], link["deps"])

    def test_exported_generated_includes_do_not_expand_into_link_source_inputs(self):
        files = {
            "producer/ya.make": (
                f"LIBRARY()\n{BARE}"
                "COPY_FILE(src.h generated.h OUTPUT_INCLUDES leaf.h)\n"
                "SRCS(local.cpp public.h)\nEND()\n"
            ),
            "producer/local.cpp": '#include "public.h"\nint local;\n',
            "producer/public.h": '#include "generated.h"\n',
            "producer/src.h": "#pragma once\n",
            "producer/leaf.h": "#pragma once\n",
            "consumer/ya.make": f"PROGRAM()\n{BARE}PEERDIR(producer)\nSRCS(main.cpp)\nEND()\n",
            "consumer/main.cpp": "int main() {}\n",
        }
        graph = lib.make(files, "consumer")
        copy = lib.node_by_output(graph, "$(B)/producer/generated.h")
        compiled = lib.node_by_output(graph, "$(B)/producer/local.cpp.o")
        link = lib.node_by_output(graph, "$(B)/consumer/consumer")
        self.assertIn("$(S)/producer/leaf.h", compiled["inputs"])
        self.assertIn("$(B)/producer/generated.h", link["inputs"])
        self.assertIn(copy["uid"], link["deps"])
        self.assertNotIn("$(S)/producer/leaf.h", link["inputs"])
        self.assertNotIn("$(S)/producer/src.h", link["inputs"])


        for module, output in (("GO_LIBRARY", "consumer.a"), ("GO_PROGRAM", "consumer")):
            with self.subTest(module=module):
                go_files = go_world()
                go_files.update(files)
                go_files["consumer/ya.make"] = f"{module}()\n{BARE}PEERDIR(producer)\nSRCS(main.go)\nEND()\n"
                go_files["consumer/main.go"] = "package main\nfunc main() {}\n"
                go_graph = lib.make(go_files, "consumer")
                go_node = lib.node_by_output(go_graph, f"$(B)/consumer/{output}")
                go_copy = lib.node_by_output(go_graph, "$(B)/producer/generated.h")
                self.assertIn("$(B)/producer/generated.h", go_node["inputs"])
                self.assertIn(go_copy["uid"], go_node["deps"])
                self.assertNotIn("$(S)/producer/leaf.h", go_node["inputs"])
                self.assertNotIn("$(S)/producer/src.h", go_node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
