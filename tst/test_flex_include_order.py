import unittest

import lib
from test_codegen_bison_flex import BISON_SKELETONS


MODULE = "orchard/parser"
FLEX_DIR = "contrib/tools/flex-old"
SELF = f"-I$(S)/{MODULE}"
FLEX = f"-I$(S)/{FLEX_DIR}"
HEADER_DIR = f"-I$(B)/{MODULE}/private"
ROOTS = ["-I$(B)", "-I$(S)"]
SOURCES = "SRCS(src/plain.cpp private/grammar.y private/scan.l)\n"


def native_world(body):
    files = {
        f"{MODULE}/ya.make": f"LIBRARY(parser)\n{lib.BARE_MODULE}CFLAGS(-DMODULE_FLAG)\n" + body + "END()\n",
        f"{MODULE}/private/grammar.y": '%{\n#include <choice.h>\n%}\n%%\n',
        f"{MODULE}/private/scan.l": '%{\n#include <choice.h>\n%}\n%%\n',
        f"{MODULE}/src/plain.cpp": '#include <choice.h>\n',
        f"{MODULE}/src/special.cpp": '#include <choice.h>\n',
        f"{MODULE}/choice.h": "// the module's header\n",
        f"{FLEX_DIR}/choice.h": "// the Flex tool directory's header\n",
        "util/system/compiler.h": "",
        "build/scripts/preprocess.py": "",
        "build/induced/by_bison/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
    }
    for skeleton in BISON_SKELETONS:
        files[f"contrib/tools/bison/data/{skeleton}"] = ""
    for tool, binary in ((FLEX_DIR, "flex"), ("contrib/tools/bison", "bison"), ("contrib/tools/m4", "m4")):
        lib.tool_program(files, tool, binary)
    return files


def module_compiles(graph, directory=MODULE):
    return [n for n in graph["graph"] if n.get("kv", {}).get("p") == "CC"
            and any(o.startswith(f"$(B)/{directory}/") for o in n["outputs"])]


def includes(node):
    return [a for a in node["cmds"][0]["cmd_args"] if a.startswith("-I")]


class FlexIncludeOrderTest(unittest.TestCase):
    def test_module_rule_and_output_phases_preserve_header_choice(self):
        cases = (
            (SOURCES + "ADDINCLSELF()\n", [SELF, FLEX, HEADER_DIR], MODULE),
            ("ADDINCLSELF()\n" + SOURCES, [SELF, FLEX, HEADER_DIR], MODULE),
            (SOURCES, [FLEX, HEADER_DIR], FLEX_DIR),
            ("SRCS(private/scan.l private/grammar.y src/plain.cpp)\nADDINCLSELF()\n",
             [SELF, FLEX, HEADER_DIR], MODULE),
        )
        for body, expected, winner in cases:
            with self.subTest(body=body):
                files = native_world(body + "SRC(src/special.cpp -DSOURCE_ONLY)\n")
                files[f"{MODULE}/src/plain.cpp"] += '#include "grammar.h"\n'
                graph = lib.make(files, MODULE)
                compiles = module_compiles(graph)
                self.assertEqual(len(compiles), 4)
                for node in compiles:
                    self.assertEqual(includes(node), ROOTS + expected)
                    self.assertIn(f"$(S)/{winner}/choice.h", node["inputs"])
                    loser = FLEX_DIR if winner == MODULE else MODULE
                    self.assertNotIn(f"$(S)/{loser}/choice.h", node["inputs"])
                    command = node["cmds"][0]["cmd_args"]
                    self.assertIn("-DMODULE_FLAG", command)
                    self.assertEqual("-DSOURCE_ONLY" in command, any("special.cpp" in o for o in node["outputs"]))
                header = f"$(B)/{MODULE}/private/grammar.h"
                grammar = f"$(B)/{MODULE}/_/private/grammar.y.cpp"
                scanner = f"$(B)/{MODULE}/_/private/scan.l.cpp"
                bison = lib.node_by_output(graph, header)
                flex = lib.node_by_output(graph, scanner)
                self.assertEqual(bison["outputs"], [header, grammar])
                self.assertEqual(flex["inputs"], [f"$(B)/{FLEX_DIR}/flex", f"$(S)/{MODULE}/private/scan.l"])
                for output in (f"$(B)/{MODULE}/_/src/plain.cpp.o", f"$(B)/{MODULE}/_/_/private/grammar.y.cpp.o"):
                    consumer = lib.node_by_output(graph, output)
                    self.assertIn(header, consumer["inputs"])
                    self.assertIn(bison["uid"], consumer["deps"])
                    self.assertNotIn(f"$(S)/{MODULE}/private/grammar.y", consumer["inputs"])
                lexer_cc = lib.node_by_output(graph, scanner.replace("/_/", "/_/_/") + ".o")
                self.assertIn(flex["uid"], lexer_cc["deps"])
                self.assertIn("-Wno-unused-variable", lexer_cc["cmds"][0]["cmd_args"])

    def test_declared_and_peer_includes_retain_scope_and_output_export_order(self):
        files = native_world(SOURCES + "ADDINCLSELF()\nADDINCL(GLOBAL policy/early)\nPEERDIR(exporter)\n")
        files.update({
            "policy/early/choice.h": "// the declared include wins\n",
            "exporter/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(GLOBAL exported)\nEND()\n",
            "exported/.keep": "",
            "courier/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR({MODULE})\nSRCS(use.cpp)\nEND()\n",
            "courier/use.cpp": '#include <choice.h>\n#include "grammar.h"\n',
        })
        graph = lib.make(files, "courier")
        for node in module_compiles(graph):
            self.assertEqual(includes(node), ROOTS + ["-I$(S)/policy/early", SELF, FLEX, HEADER_DIR, "-I$(S)/exported"])
            self.assertIn("$(S)/policy/early/choice.h", node["inputs"])
        consumer = lib.node_by_output(graph, "$(B)/courier/use.cpp.o")
        self.assertEqual(includes(consumer), ROOTS + ["-I$(S)/policy/early", HEADER_DIR, "-I$(S)/exported"])
        self.assertIn("$(S)/policy/early/choice.h", consumer["inputs"])
        bison = lib.node_by_output(graph, f"$(B)/{MODULE}/private/grammar.h")
        self.assertIn(bison["uid"], consumer["deps"])
        self.assertIn(f"$(B)/{MODULE}/private/grammar.h", consumer["inputs"])

    def test_individual_rules_and_plain_sources_do_not_gain_sibling_includes(self):
        for generated, extra in (("private/grammar.y", [HEADER_DIR]), ("private/scan.l", [FLEX]), ("", [])):
            with self.subTest(generated=generated):
                graph = lib.make(native_world(f"SRCS(src/plain.cpp {generated})\nADDINCLSELF()\n"), MODULE)
                compiles = module_compiles(graph)
                self.assertEqual(len(compiles), 2 if generated else 1)
                for node in compiles:
                    self.assertEqual(includes(node), ROOTS + [SELF, *extra])
                    self.assertIn(f"$(S)/{MODULE}/choice.h", node["inputs"])

    def test_multiple_bison_directories_keep_source_order_after_rule_includes(self):
        files = native_world(
            "SRCS(src/plain.cpp second/b.ypp private/grammar.y private/scan.l)\nADDINCLSELF()\n"
        )
        files[f"{MODULE}/second/b.ypp"] = "%%\n"
        graph = lib.make(files, MODULE)
        for node in module_compiles(graph):
            self.assertEqual(includes(node), ROOTS + [SELF, FLEX, f"-I$(B)/{MODULE}/second", HEADER_DIR])


if __name__ == "__main__":
    unittest.main(verbosity=2)
