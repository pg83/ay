import unittest

import lib


class NlgIncludesTest(unittest.TestCase):
    def files(self, template):
        files = {
            "peer/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "RUN_PROGRAM(tools/gen IN data.nlg OUT dummy.h "
                "OUT_NOAUTO data.nlg.h data.nlg.pb.txt)\nEND()\n"
            ),
            "peer/data.nlg": "",
            "m/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(peer)\n"
                "RUN_PROGRAM(tools/gen IN main.nlg OUT dummy.h "
                "OUT_NOAUTO main.nlg.h main.nlg.pb.txt)\n"
                "SRCS(use.cpp)\nEND()\n"
            ),
            "m/main.nlg": template,
            "m/use.cpp": '#include "main.nlg.h"\n',
        }
        lib.tool_program(files, "tools/gen", "gen")
        return files

    def test_external_imports_consume_generated_data(self):
        for command in (
            'ext_from "peer/data.nlg" import render',
            "ext_import 'peer/data.nlg' as data with context",
            'ext_nlgimport "peer/data.nlg"',
        ):
            with self.subTest(command=command):
                graph = lib.make(self.files("{% " + command + " %}\n"), "m")
                generated = lib.node_by_output(graph, "$(B)/m/dummy.h")
                self.assertIn("$(B)/peer/data.nlg.pb.txt", generated["inputs"])
                self.assertIn("$(B)/peer/dummy.h", generated["inputs"])
                compiled = lib.node_by_output(graph, "$(B)/m/use.cpp.o")
                self.assertIn("$(B)/peer/data.nlg.h", compiled["inputs"])
                self.assertIn("$(B)/peer/data.nlg.pb.txt", compiled["inputs"])

    def test_internal_imports_only_induce_headers(self):
        for command in (
            'from "peer/data.nlg" import render, helper with context',
            "import 'peer/data.nlg' as data",
            'nlgimport "peer/data.nlg"',
        ):
            with self.subTest(command=command):
                graph = lib.make(self.files("{% " + command + " %}\n"), "m")
                generated = lib.node_by_output(graph, "$(B)/m/dummy.h")
                self.assertNotIn("$(B)/peer/data.nlg.pb.txt", generated["inputs"])
                compiled = lib.node_by_output(graph, "$(B)/m/use.cpp.o")
                self.assertIn("$(B)/peer/data.nlg.h", compiled["inputs"])

    def test_multiline_import_lists_use_native_whitespace(self):
        files = self.files("{%\vext_import\n'peer/data.nlg' as data with context,\n"
                           "\t\"peer/data.nlg\"\f%}\n")
        graph = lib.make(files, "m")
        generated = lib.node_by_output(graph, "$(B)/m/dummy.h")
        self.assertIn("$(B)/peer/data.nlg.pb.txt", generated["inputs"])
        self.assertEqual(1, generated["inputs"].count("$(B)/peer/data.nlg.pb.txt"))

    def test_comments_and_invalid_tags_do_not_hide_later_imports(self):
        files = self.files(
            '# {% ext_import "missing/comment.nlg" %}\n'
            '{% ext_import "missing/invalid-name.nlg" %}\n'
            '{% invalid \n{% ext_import "peer/data.nlg" %}\n'
        )
        graph = lib.make(files, "m")
        generated = lib.node_by_output(graph, "$(B)/m/dummy.h")
        self.assertIn("$(B)/peer/data.nlg.pb.txt", generated["inputs"])

    def test_external_imports_are_transitive_between_data_outputs(self):
        files = self.files('{% ext_import "peer/data.nlg" %}\n')
        files["peer/data.nlg"] = '{% ext_from "leaf/data.nlg" import value %}\n'
        files["peer/ya.make"] = files["peer/ya.make"].replace("RUN_PROGRAM", "PEERDIR(leaf)\nRUN_PROGRAM")
        files["leaf/ya.make"] = files["peer/ya.make"].replace("PEERDIR(leaf)\n", "")
        files["leaf/data.nlg"] = ""
        graph = lib.make(files, "m")
        generated = lib.node_by_output(graph, "$(B)/m/dummy.h")
        for module in ("peer", "leaf"):
            self.assertIn(f"$(B)/{module}/data.nlg.pb.txt", generated["inputs"])
            self.assertIn(f"$(B)/{module}/dummy.h", generated["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
