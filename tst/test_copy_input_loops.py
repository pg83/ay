import unittest

import lib


def copy_input_loop_files(members=3, source_context=False, built_source=False):
    body = (
        "LIBRARY()\n" + lib.BARE_MODULE
        + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod mod/seed)\n"
    )
    if source_context:
        body += "COPY_FILE_WITH_CONTEXT(mod/seed/a.h a.h)\n"
    elif members:
        outputs = "${BINDIR}/b.h" + (" ${BINDIR}/c.h" if members == 3 else "")
        if built_source:
            body += (
                "COPY_FILE(TEXT mod/seed/a.h base.h)\n"
                "COPY_FILE(TEXT ${ARCADIA_BUILD_ROOT}/mod/base.h a.h OUTPUT_INCLUDES b.h leaf.h)\n"
            )
        else:
            body += "COPY(AUTO WITH_CONTEXT FROM mod/seed a.h OUTPUT_INCLUDES " + outputs + ")\n"
        body += "COPY_FILE(TEXT mod/seed/b.h.txt b.h OUTPUT_INCLUDES a.h leaf.h)\n"
        if members == 3:
            body += "COPY_FILE(TEXT mod/seed/c.h.txt c.h OUTPUT_INCLUDES b.h)\n"
    else:
        body += "COPY(AUTO WITH_CONTEXT FROM mod/seed a.h)\n"
    body += "SRCS(main.cpp)\nEND()\n"
    return {
        "mod/ya.make": body,
        "mod/main.cpp": '#include "a.h"\nint value;\n',
        "mod/seed/a.h": '#include "leaf.h"\n',
        "mod/seed/b.h.txt": "",
        "mod/seed/c.h.txt": "",
        "mod/seed/leaf.h": '#include "nested.h"\n',
        "mod/seed/nested.h": "",
        "build/scripts/fs_tools.py": "import process_command_files\n",
        "build/scripts/process_command_files.py": "",
    }


def source_inputs(node):
    return [path for path in node["inputs"] if path.startswith("$(S)/mod/")]


class CopyInputLoopTest(unittest.TestCase):
    def test_opaque_copy_input_loop_excludes_downstream_header_closure(self):
        for members in (2, 3):
            with self.subTest(members=members):
                graph = lib.make(copy_input_loop_files(members), "mod", "--target-platform", "default-linux-x86_64")
                expected = ["$(S)/mod/seed/a.h", "$(S)/mod/seed/b.h.txt"]
                names = ["a.h", "b.h"]
                if members == 3:
                    expected.append("$(S)/mod/seed/c.h.txt")
                    names.append("c.h")
                for name in names:
                    copy = lib.node_by_output(graph, "$(B)/mod/" + name)
                    self.assertCountEqual(source_inputs(copy), expected)
                compiled = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")
                self.assertIn("$(S)/mod/seed/leaf.h", compiled["inputs"])
                self.assertIn("$(S)/mod/seed/nested.h", compiled["inputs"])

    def test_copy_loop_keeps_a_generated_input_and_its_producer(self):
        graph = lib.make(copy_input_loop_files(2, built_source=True), "mod", "--target-platform", "default-linux-x86_64")
        base = lib.node_by_output(graph, "$(B)/mod/base.h")
        for name in ("a.h", "b.h"):
            copy = lib.node_by_output(graph, "$(B)/mod/" + name)
            self.assertEqual(source_inputs(copy), ["$(S)/mod/seed/b.h.txt"])
            if name == "a.h":
                self.assertIn("$(B)/mod/base.h", copy["inputs"])
                self.assertIn(base["uid"], copy["deps"])
            else:
                self.assertNotIn("$(B)/mod/base.h", copy["inputs"])
                self.assertNotIn(base["uid"], copy["deps"])
        compiled = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")
        self.assertIn("$(S)/mod/seed/leaf.h", compiled["inputs"])
        self.assertIn("$(S)/mod/seed/nested.h", compiled["inputs"])

    def test_acyclic_copy_keeps_output_context_off_the_copy_action(self):
        graph = lib.make(copy_input_loop_files(0), "mod", "--target-platform", "default-linux-x86_64")
        copy = lib.node_by_output(graph, "$(B)/mod/a.h")
        self.assertEqual(source_inputs(copy), ["$(S)/mod/seed/a.h"])
        compiled = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")
        self.assertIn("$(S)/mod/seed/leaf.h", compiled["inputs"])
        self.assertIn("$(S)/mod/seed/nested.h", compiled["inputs"])

    def test_explicit_source_context_keeps_parsed_inputs_on_the_copy_action(self):
        graph = lib.make(copy_input_loop_files(source_context=True), "mod", "--target-platform", "default-linux-x86_64")
        copy = lib.node_by_output(graph, "$(B)/mod/a.h")
        self.assertCountEqual(source_inputs(copy), [
            "$(S)/mod/seed/a.h", "$(S)/mod/seed/leaf.h", "$(S)/mod/seed/nested.h",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
