import unittest

import lib
from test_go_package import go_world


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class ModuleHeaderDependenciesTest(unittest.TestCase):
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
