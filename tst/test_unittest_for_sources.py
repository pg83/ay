import unittest

import lib
from test_proto_cpp import pb_node, proto_files


SUBJECT = "saffron/topic"
SUITE = SUBJECT + "/check"


def fixture(body, source):
    files = proto_files()
    files[f"{SUBJECT}/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
    files["library/cpp/testing/unittest_main/ya.make"] = (
        f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
    )
    files[f"{SUITE}/ya.make"] = (
        f"UNITTEST_FOR({SUBJECT})\n{lib.BARE_MODULE}{body}\n"
        f"SRCS({source} ${{ARCADIA_ROOT}}/schemas/branch.proto)\nEND()\n"
    )
    return files


class UnittestForSourcesTest(unittest.TestCase):
    def check_proto(self, files, selected):
        graph = lib.make(files, SUITE, "--sandboxing")
        header = f"$(B)/{selected}/shape.pb.h"
        generated = pb_node(graph, header)
        self.assertIn(f"{selected}/shape.proto", generated["cmds"][0]["cmd_args"])
        self.assertIn(f"$(S)/{selected}/shape.proto", generated["inputs"])
        self.assertIn("$(S)/schemas/branch.proto", generated["inputs"])
        compiled = next(node for node in graph["graph"]
                        if node.get("kv", {}).get("p") == "CC"
                        and header in node["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        binary = next(node for node in graph["graph"]
                      if node.get("kv", {}).get("p") == "LD"
                      and compiled["outputs"][0] in node["inputs"])
        self.assertIn(compiled["uid"], binary["deps"])
        self.assertFalse(any("/check/check/" in output
                             for node in graph["graph"] for output in node["outputs"]))
        return graph, generated

    def test_inherited_source_directory_reaches_proto_producer_and_consumers(self):
        files = fixture("", "check/shape.proto")
        files[f"{SUITE}/shape.proto"] = 'import "schemas/branch.proto";\n'
        files["schemas/branch.proto"] = 'message Branch {}\n'
        self.check_proto(files, SUITE)

    def test_inherited_source_directory_keeps_priority_over_explicit_directories(self):
        files = fixture("SRCDIR(alternate)", "shape.proto shared.cpp")
        files[f"{SUBJECT}/shape.proto"] = 'import "schemas/branch.proto";\n'
        files[f"{SUBJECT}/shared.cpp"] = "int selected;\n"
        files["alternate/shape.proto"] = 'import "absent.proto";\n'
        files["alternate/shared.cpp"] = '#include "absent.h"\n'
        files["schemas/branch.proto"] = 'message Branch {}\n'
        graph, generated = self.check_proto(files, SUBJECT)
        self.assertNotIn("$(S)/alternate/shape.proto", generated["inputs"])
        compiled = next(node for node in graph["graph"]
                        if node.get("kv", {}).get("p") == "CC"
                        and f"$(S)/{SUBJECT}/shared.cpp" in node["inputs"])
        self.assertNotIn("$(S)/alternate/shared.cpp", compiled["inputs"])

    def test_explicit_source_root_keeps_the_named_source(self):
        files = fixture("SRCDIR(alternate)", "${CURDIR}/shape.proto")
        files[f"{SUITE}/shape.proto"] = 'import "schemas/branch.proto";\n'
        files["alternate/shape.proto"] = 'import "absent.proto";\n'
        files["schemas/branch.proto"] = 'message Branch {}\n'
        self.check_proto(files, SUITE)


if __name__ == "__main__":
    unittest.main(verbosity=2)
