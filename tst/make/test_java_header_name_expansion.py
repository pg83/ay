import unittest

from tst import lib
from tst.make.test_java import writer_args


MAIN = 'class Main {}\n'


def fixture(mode):
    files = lib.java_tree(**{"build/platform/java/scriptgen/ya.make": lib.resource_lib("SCRIPTGEN", 11)})
    targets = ["grove/single"]
    declarations = {
        "literal": 'JAVA_LIBRARY_NON_SPLIT(poplar)',
        "default": 'JAVA_LIBRARY_NON_SPLIT()',
        "bound": 'SET(STEM poplar)\nJAVA_LIBRARY_NON_SPLIT(${STEM})',
        "bare": 'SET(STEM poplar)\nJAVA_LIBRARY_NON_SPLIT($STEM)',
        "empty": 'JAVA_LIBRARY_NON_SPLIT(${UNDEFINED})',
        "program": 'SET(STEM cedar)\nJAVA_PROGRAM(${STEM})',
        "words": 'SET(STEM elm)\nSET(HEADER "${STEM}  PREFIX   pre-")\nJAVA_LIBRARY_NON_SPLIT(${HEADER})',
    }
    expected = {"literal": "poplar.jar", "default": "grove-single.jar", "bound": "poplar.jar",
                "bare": "$STEM.jar", "empty": "grove-single.jar", "program": "cedar.jar",
                "words": "pre-elm.jar"}
    if mode == "shared":
        files.update({
            "common/outer.inc": 'SET(LEAF ${PART})\nINCLUDE(inner.inc)\n',
            "common/inner.inc": ('JAVA_LIBRARY_NON_SPLIT(${STEM}_${LEAF})\n'
                                 'SET(STEM changed_after_header)\nJAVA_SRCS(Main.java)\nEND()\n'),
        })
        outputs = []
        for directory, stem, part in (("grove/left", "cedar", "north"), ("grove/right", "birch", "south")):
            files[directory + "/ya.make"] = f'SET(STEM {stem})\nSET(PART {part})\nINCLUDE(${{ARCADIA_ROOT}}/common/outer.inc)\n'
            files[directory + "/Main.java"] = MAIN
            outputs.append(f"$(B)/{directory}/{stem}_{part}.jar")
        return files, ["grove/left", "grove/right"], outputs
    if mode == "split":
        files.update({
            "grove/leaf/ya.make": 'SET(LIBRARY_NAME willow)\nJAVA_LIBRARY(${LIBRARY_NAME})\nJAVA_SRCS(Main.java)\nEND()\n',
            "grove/leaf/Main.java": MAIN,
            "grove/consumer/ya.make": 'SET(PROGRAM_NAME alder)\nJAVA_PROGRAM(${PROGRAM_NAME})\nPEERDIR(grove/leaf)\nJAVA_SRCS(Main.java)\nEND()\n',
            "grove/consumer/Main.java": MAIN,
        })
        lib.tool_program(files, "contrib/tools/ijar", "ijar")
        return files, ["grove/consumer"], ["$(B)/grove/consumer/alder.jar", "$(B)/grove/leaf/willow.jar", "$(B)/grove/leaf/willow-interface.jar"]
    if mode == "schema":
        from tst.make.test_java import contrib
        files.update(contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=41))
        files.update({
            "grove/single/ya.make": 'SET(STEM willow)\nPROTO_SCHEMA(${STEM})\nONLY_TAGS(JAVA_PROTO_FROM_SCHEMA)\nSRCS(api.proto)\nEND()\n',
            "contrib/libs/protobuf/src/.keep": "",
            "grove/single/api.proto": 'syntax = "proto3"; message Api {}\n',
            "grove/reader/ya.make": 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(grove/single)\nEND()\n',
        })
        return files, ["grove/reader"], ["$(B)/grove/single/willow.jar", "$(B)/grove/single/api.jsrc"]
    if mode == "generated":
        files.update({
            "grove/single/ya.make": 'SET(STEM larch)\nJAVA_LIBRARY_NON_SPLIT(${STEM})\nRUN_PYTHON3(common/generate.py OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n',
            "common/generate.py": 'from pathlib import Path\nPath("Generated.java").write_text("class Generated {}\\n")\n',
        })
        return files, targets, ["$(B)/grove/single/larch.jar", "$(B)/grove/single/Generated.java"]
    files["grove/single/ya.make"] = declarations[mode] + '\nSET(STEM changed_after_header)\nJAVA_SRCS(Main.java)\nEND()\n'
    files["grove/single/Main.java"] = MAIN
    return files, targets, ["$(B)/grove/single/" + expected[mode]]


class JavaHeaderNameExpansionTest(unittest.TestCase):
    def test_java_headers_expand_at_their_source_position(self):
        for mode in ("literal", "default", "bound", "bare", "empty", "program", "words", "shared"):
            with self.subTest(mode=mode):
                files, targets, outputs = fixture(mode)
                graph = lib.make(files, *targets)
                for output in outputs:
                    jar = lib.node_by_output(graph, output)
                    directory = output.rsplit("/", 1)[0].replace("$(B)", "$(S)")
                    self.assertIn(directory + "/Main.java", jar["inputs"])
                self.assertTrue(graph["result"])

    def test_split_module_contexts_keep_named_producer_and_peer_links(self):
        files, targets, outputs = fixture("split")
        graph = lib.make(files, *targets)
        program, full, interface = [lib.node_by_output(graph, output) for output in outputs]
        self.assertIn(full["uid"], interface["deps"])
        self.assertIn(full["outputs"][0], interface["inputs"])
        self.assertIn(interface["uid"], program["deps"])
        self.assertEqual(writer_args(program, "bfg.txt"), ["grove/leaf/willow-interface.jar"])

    def test_schema_java_context_retains_its_header_name_and_generated_proto(self):
        files, targets, outputs = fixture("schema")
        graph = lib.make(files, *targets)
        jar, generated = [lib.node_by_output(graph, output) for output in outputs]
        self.assertIn("$(S)/grove/single/api.proto", generated["inputs"])
        self.assertIn(generated["uid"], jar["deps"])
        self.assertIn(generated["outputs"][0], jar["inputs"])

    def test_generated_java_source_retains_its_producer_and_ownership(self):
        files, targets, outputs = fixture("generated")
        graph = lib.make(files, *targets)
        jar, generated = [lib.node_by_output(graph, output) for output in outputs]
        self.assertIn("$(S)/common/generate.py", generated["inputs"])
        self.assertIn(generated["uid"], jar["deps"])
        self.assertIn(generated["outputs"][0], jar["inputs"])
        self.assertNotIn("$(S)/grove/single/Generated.java", jar["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
