import unittest

from tst import lib


BARE = lib.BARE_MODULE


def fixture(mode):
    if mode == "shared":
        files = {
            "common/outer.inc": 'SET(LEAF ${PART})\nINCLUDE(inner.inc)\n',
            "common/inner.inc": ('PROGRAM(${STEM}_${LEAF})\n' + BARE
                                 + 'SET(STEM changed_after_declaration)\nSRCS(main.cpp)\nEND()\n'),
        }
        for directory, stem, part in (("grove/left", "cedar", "north"), ("grove/right", "birch", "south")):
            files[directory + "/ya.make"] = (
                f'SET(STEM {stem})\nSET(PART {part})\nINCLUDE(${{ARCADIA_ROOT}}/common/outer.inc)\n'
            )
            files[directory + "/main.cpp"] = 'int main() {return 0;}\n'
        return files, ["grove/left", "grove/right"], ["$(B)/grove/left/cedar_north", "$(B)/grove/right/birch_south"]
    if mode == "producer":
        files = {
            "common/program.inc": 'PROGRAM(${STEM}_${PART})\n' + BARE,
            "grove/reader/ya.make": ('SET(STEM alder)\nSET(PART reader)\n'
                                    'INCLUDE(${ARCADIA_ROOT}/common/program.inc)\n'
                                    'RUN_PROGRAM(grove/writer OUT main.cpp)\nEND()\n'),
            "grove/writer/ya.make": ('SET(STEM willow)\nSET(PART writer)\n'
                                    'INCLUDE(${ARCADIA_ROOT}/common/program.inc)\nSRCS(main.cpp)\nEND()\n'),
            "grove/writer/main.cpp": 'int main() {return 0;}\n',
        }
        return files, ["grove/reader"], ["$(B)/grove/reader/alder_reader", "$(B)/grove/writer/willow_writer"]
    declarations = {
        "literal": 'PROGRAM(explicit_name)',
        "default": 'PROGRAM()',
        "bound": 'SET(STEM poplar)\nPROGRAM(${STEM})',
        "bare": 'SET(STEM poplar)\nPROGRAM($STEM)',
        "escaped": r'SET(STEM poplar)' + '\n' + r'PROGRAM(\${STEM})',
        "environment": 'PROGRAM($ENV{T153_MODULE_NAME})',
        "unknown": 'PROGRAM(prefix_${UNDEFINED})',
        "empty": 'PROGRAM(${UNDEFINED})',
        "library": 'SET(STEM maple)\nLIBRARY(${STEM}_leaf)',
        "list": 'SET(DECL_NAME elm)\nSET(DECL_OPTIONS "${DECL_NAME}  PREFIX   plug_")\nDLL(${DECL_OPTIONS})',
    }
    expected = {"literal": "explicit_name", "default": "single", "bound": "poplar", "unknown": "prefix_",
                "bare": "$STEM", "escaped": "${STEM}", "environment": "pine_from_process",
                "empty": "single", "library": "libmaple_leaf.a", "list": "plug_elm.so"}[mode]
    files = {"grove/single/ya.make": declarations[mode] + '\n' + BARE + 'SRCS(main.cpp)\nEND()\n',
             "grove/single/main.cpp": 'int main() {return 0;}\n',
             "grove/single/symbols.exports": 'C foo\n'}
    if mode == "list":
        lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files, ["grove/single"], ["$(B)/grove/single/" + expected]


class ModuleNameExpansionTest(unittest.TestCase):
    def test_module_declaration_arguments_expand_in_the_current_source_environment(self):
        for mode in ("literal", "default", "bound", "bare", "escaped", "environment", "unknown", "empty", "library", "list", "shared"):
            with self.subTest(mode=mode):
                files, targets, outputs = fixture(mode)
                graph = lib.make(files, *targets, env={"T153_MODULE_NAME": "pine_from_process"})
                for output in outputs:
                    lib.node_by_output(graph, output)
                if mode == "shared":
                    for directory in ("left", "right"):
                        node = lib.node_by_output(graph, f"$(B)/grove/{directory}/main.cpp.o")
                        self.assertIn(f"$(S)/grove/{directory}/main.cpp", node["inputs"])
                if mode == "list":
                    link = lib.node_by_output(graph, outputs[0])
                    self.assertIn("$(B)/grove/single/main.cpp.pic.o", link["inputs"])

    def test_shared_declaration_keeps_tool_and_generated_source_producer_ownership(self):
        files, targets, outputs = fixture("producer")
        graph = lib.make(files, *targets)
        reader, writer = [lib.node_by_output(graph, output) for output in outputs]
        generated = lib.node_by_output(graph, "$(B)/grove/reader/main.cpp")
        compiled = lib.node_by_output(graph, "$(B)/grove/reader/main.cpp.o")
        self.assertEqual(generated["cmds"][0]["cmd_args"], [outputs[1]])
        self.assertIn(writer["uid"], generated["deps"])
        self.assertIn(writer["outputs"][0], generated["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(generated["outputs"][0], compiled["inputs"])
        self.assertIn(compiled["uid"], reader["deps"])
        self.assertNotIn("$(S)/grove/reader/main.cpp", compiled["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
