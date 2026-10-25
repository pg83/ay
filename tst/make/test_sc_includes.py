import json
import unittest

from tst import lib
from tst.make.test_include_resolve import make_process
from tst.make.test_include_domains import padded_to_boundary


TOOL = "$(B)/tools/domschemec/schema_driver"
RUNTIME = "$(S)/library/cpp/domscheme/runtime.h"


def library(body):
    return f"LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n"


def sc_files(body="", schema='include "branch/child.inc";\n'):
    files = {
        "ochre/parcel/ya.make": library(body + "\nSRCS(main.sc use.cpp)"),
        "ochre/parcel/main.sc": schema,
        "ochre/parcel/branch/child.inc": 'include "../leaf.sc";\n#include "cpp.h"\n',
        "ochre/parcel/leaf.sc": "struct Leaf {};\n",
        "ochre/parcel/branch/cpp.h": "",
        "ochre/parcel/use.cpp": '#include <ochre/parcel/main.sc.h>\n',
        "library/cpp/domscheme/ya.make": library(""),
        "library/cpp/domscheme/runtime.h": '#include "runtime_tail.h"\n',
        "library/cpp/domscheme/runtime_tail.h": "",
    }
    lib.tool_program(files, "tools/domschemec", "schema_driver")
    return files


def schema_node(graph, output="$(B)/ochre/parcel/main.sc.h"):
    node = lib.node_by_output(graph, output)
    assert node["kv"]["p"] == "SC", node
    return node


class ScIncludesTest(unittest.TestCase):
    def test_nested_opaque_and_cyclic_schema_closure_stays_behind_generator(self):
        files = sc_files()
        files["ochre/parcel/leaf.sc"] = 'include "branch/child.inc";\n'
        graph = lib.make(files, "ochre/parcel", "--sandboxing")
        node = schema_node(graph)
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            TOOL, "--in", "$(S)/ochre/parcel/main.sc", "--out", "$(B)/ochre/parcel/main.sc.h",
        ])
        self.assertEqual(set(node["inputs"]), {
            TOOL, "$(S)/ochre/parcel/main.sc", "$(S)/ochre/parcel/branch/child.inc", "$(S)/ochre/parcel/leaf.sc",
        })
        self.assertEqual(node["foreign_deps"], {"tool": [lib.node_by_output(graph, TOOL)["uid"]]})
        compiled = lib.node_by_output(graph, "$(B)/ochre/parcel/use.cpp.o")
        self.assertEqual(set(compiled["inputs"]), {
            "$(S)/ochre/parcel/use.cpp", "$(B)/ochre/parcel/main.sc.h", RUNTIME,
            "$(S)/library/cpp/domscheme/runtime_tail.h",
        })
        self.assertEqual(compiled["deps"], [node["uid"]])

    def test_exact_line_grammar_does_not_strip_comments(self):
        files = sc_files(schema=(
            '  include\t"one.sc" \v; trailing\r\n/*\ninclude "inside.sc";\n*/\n'
            '#include "cpp.sc"\n// include "line.sc";\nincludeX "keyword.sc";\n'
            'include"tight.sc";\ninclude \'single.sc\';\ninclude "semicolon.sc"\n'
            'include "";\ninclude "unfinished.sc;\ninclude <angle.sc>;\n'
            'include\v"vertical.sc";\ninclude "end.sc";'
        ))
        for name in ("one", "inside", "cpp", "line", "keyword", "tight", "single", "semicolon", "angle", "vertical", "end"):
            files[f"ochre/parcel/{name}.sc"] = ""
        graph = lib.make(files, "ochre/parcel")
        self.assertEqual(set(schema_node(graph)["inputs"]), {
            TOOL, "$(S)/ochre/parcel/main.sc",
            *[f"$(S)/ochre/parcel/{name}.sc" for name in ("one", "inside", "vertical", "end")],
        })

    def test_sc_paths_keep_language_scope_and_declaration_order(self):
        for paths, selected in (
            ("FOR sc palette/first FOR sc palette/second", "first"),
            ("GLOBAL FOR sc palette/second FOR sc palette/first", "second"),
            ("FOR sc GLOBAL palette/second FOR sc LOCAL palette/first", "second"),
        ):
            files = sc_files(f"ADDINCL(FOR asm palette/cpp palette/cpp {paths})", 'include "leaf.sc";\n')
            files.pop("ochre/parcel/leaf.sc")
            for directory in ("cpp", "first", "second"):
                files[f"palette/{directory}/leaf.sc"] = ""
            graph = lib.make(files, "ochre/parcel")
            self.assertEqual(set(schema_node(graph)["inputs"]), {
                TOOL, "$(S)/ochre/parcel/main.sc", f"$(S)/palette/{selected}/leaf.sc",
            })
        files = sc_files("ADDINCL(palette/cpp)", 'include "leaf.sc";\n')
        files.pop("ochre/parcel/leaf.sc")
        files["palette/cpp/leaf.sc"] = ""
        result = make_process(files, "ochre/parcel")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unresolved include "leaf.sc"', result.stderr)

    def test_global_sc_paths_propagate_through_peers_without_cpp_or_private_paths(self):
        files = sc_files("PEERDIR(bridge)", 'include "shared.sc";\n')
        files.update({
            "bridge/ya.make": library("PEERDIR(exporter)"),
            "exporter/ya.make": library(
                "ADDINCL(FOR sc hidden GLOBAL FOR sc public GLOBAL cpp_scope)\nSRCS(own.sc own.cpp)"
            ),
            "exporter/own.sc": 'include "private.sc";\n',
            "exporter/own.cpp": '#include <exporter/own.sc.h>\n',
            "hidden/private.sc": "",
            "hidden/shared.sc": "",
            "public/shared.sc": "",
            "cpp_scope/shared.sc": "",
        })
        files["ochre/parcel/use.cpp"] += '#include <exporter/own.sc.h>\n'
        graph = lib.make(files, "ochre/parcel", "--sandboxing")
        self.assertEqual(set(schema_node(graph)["inputs"]), {
            TOOL, "$(S)/ochre/parcel/main.sc", "$(S)/public/shared.sc",
        })
        own = schema_node(graph, "$(B)/exporter/own.sc.h")
        self.assertIn("$(S)/hidden/private.sc", own["inputs"])
        self.assertNotIn("-I$(S)/public", lib.node_by_output(graph, "$(B)/ochre/parcel/use.cpp.o")["cmds"][0]["cmd_args"])

    def test_later_generated_import_uses_real_producer_and_secondary_main_output(self):
        files = sc_files("ADDINCL(FOR sc ${BINDIR}/generated)\nRUN_PROGRAM(tools/seed OUT_NOAUTO generated/anchor.dat generated/leaf.sc)", 'include "leaf.sc";\n')
        files.pop("ochre/parcel/leaf.sc")
        lib.tool_program(files, "tools/seed", "seed_driver")
        graph = lib.make(files, "ochre/parcel", "--sandboxing")
        producer = lib.node_by_output(graph, "$(B)/ochre/parcel/generated/leaf.sc")
        node = schema_node(graph)
        for path in ("$(B)/ochre/parcel/generated/leaf.sc", "$(B)/ochre/parcel/generated/anchor.dat"):
            self.assertIn(path, node["inputs"])
        self.assertIn(producer["uid"], node["deps"])
        self.assertNotIn(producer["uid"], node["foreign_deps"]["tool"])
        compiled = lib.node_by_output(graph, "$(B)/ochre/parcel/use.cpp.o")
        self.assertNotIn(producer["uid"], compiled["deps"])

    def test_cpp_traversal_switches_to_sc_include_paths(self):
        files = sc_files("ADDINCL(palette/cpp FOR sc palette/schema)", schema="")
        files["ochre/parcel/ya.make"] = files["ochre/parcel/ya.make"].replace("SRCS(main.sc use.cpp)", "SRCS(use.cpp)")
        files["ochre/parcel/use.cpp"] = '#include "child.sc"\n'
        files["ochre/parcel/child.sc"] = 'include "opaque.inc";\n'
        files["ochre/parcel/opaque.inc"] = 'include "leaf.sc";\n#include "cpp_only.h"\n'
        files["palette/cpp/cpp_only.h"] = ""
        files["palette/schema/leaf.sc"] = ""
        files["palette/cpp/leaf.sc"] = ""
        files.pop("ochre/parcel/leaf.sc")
        graph = lib.make(files, "ochre/parcel")
        inputs = lib.node_by_output(graph, "$(B)/ochre/parcel/use.cpp.o")["inputs"]
        self.assertIn("$(S)/palette/schema/leaf.sc", inputs)
        self.assertNotIn("$(S)/palette/cpp/leaf.sc", inputs)
        self.assertNotIn("$(S)/palette/cpp/cpp_only.h", inputs)

    def test_registered_cpp_include_keeps_cpp_grammar_and_paths(self):
        files = sc_files("ADDINCL(palette/cpp FOR sc palette/schema)", 'include "bridge.h";\n')
        files["ochre/parcel/bridge.h"] = '#include "leaf.h"\ninclude "ignored.sc";\n'
        files["palette/cpp/leaf.h"] = ""
        files["palette/schema/leaf.h"] = ""
        files["ochre/parcel/ignored.sc"] = ""
        graph = lib.make(files, "ochre/parcel")
        inputs = schema_node(graph)["inputs"]
        self.assertIn("$(S)/palette/cpp/leaf.h", inputs)
        self.assertNotIn("$(S)/palette/schema/leaf.h", inputs)
        self.assertNotIn("$(S)/ochre/parcel/ignored.sc", inputs)

    def test_peer_exports_keep_mixed_global_and_one_level_declaration_order(self):
        for order, selected in (
            ("ONE_LEVEL FOR sc palette/first GLOBAL FOR sc palette/second", "first"),
            ("GLOBAL FOR sc palette/second ONE_LEVEL FOR sc palette/first", "second"),
        ):
            files = sc_files("PEERDIR(exporter)", 'include "selected.sc";\n')
            files["exporter/ya.make"] = library("ADDINCL(" + order + ")")
            files["palette/first/selected.sc"] = ""
            files["palette/second/selected.sc"] = ""
            graph = lib.make(files, "ochre/parcel")
            self.assertEqual(set(schema_node(graph)["inputs"]), {
                TOOL, "$(S)/ochre/parcel/main.sc", f"$(S)/palette/{selected}/selected.sc",
            })

    def test_one_level_sc_exports_stop_after_the_direct_peer(self):
        for transitive in (False, True):
            files = sc_files("PEERDIR(bridge)" if transitive else "PEERDIR(exporter)", 'include "selected.sc";\n')
            files["exporter/ya.make"] = library("ADDINCL(ONE_LEVEL FOR sc palette)")
            files["bridge/ya.make"] = library("PEERDIR(exporter)")
            files["palette/selected.sc"] = ""
            result = make_process(files, "ochre/parcel")
            if transitive:
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('unresolved include "selected.sc"', result.stderr)
            else:
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("$(S)/palette/selected.sc", schema_node(json.loads(result.stdout))["inputs"])

    def test_shared_source_keeps_first_owner_and_text_does_not_claim_it(self):
        for macro, mode, selected in (("RUN_PROGRAM(tools/seed", "IN", "old"), ("RUN_PROGRAM(tools/seed", "IN_NOPARSE", "new"), ("RUN_PYTHON3(script.py", "IN", "old"), ("RUN_PYTHON3(script.py", "IN_NOPARSE", "new")):
            files = sc_files("ADDINCL(FOR sc palette/new)\nPEERDIR(early)", 'include "shared/root.sc";\n')
            files["early/ya.make"] = library(
                "ADDINCL(FOR sc palette/old)\n" + macro + " " + mode + " shared/root.sc OUT_NOAUTO unused.dat)"
            )
            files["early/script.py"] = ""
            files["shared/root.sc"] = 'include "selected.sc";\n'
            files["palette/old/selected.sc"] = ""
            files["palette/new/selected.sc"] = ""
            lib.tool_program(files, "tools/seed", "seed_driver")
            graph = lib.make(files, "ochre/parcel")
            inputs = schema_node(graph)["inputs"]
            self.assertIn(f"$(S)/palette/{selected}/selected.sc", inputs)
            other = "new" if selected == "old" else "old"
            self.assertNotIn(f"$(S)/palette/{other}/selected.sc", inputs)
            self.assertFalse(any("$(B)/early/unused.dat" in node["outputs"] for node in graph["graph"]))

    def test_incomplete_sc_scope_cannot_resolve_an_import(self):
        files = sc_files("ADDINCL(FOR sc)", 'include "absent.sc";\n')
        result = make_process(files, "ochre/parcel")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unresolved include "absent.sc"', result.stderr)

    def test_generated_primary_schema_keeps_output_includes_and_main_output(self):
        files = sc_files("RUN_PROGRAM(tools/seed OUT_NOAUTO anchor.dat made.sc OUTPUT_INCLUDES ${CURDIR}/dependency.sc)")
        files["ochre/parcel/ya.make"] = files["ochre/parcel/ya.make"].replace("SRCS(main.sc use.cpp)", "SRCS(${BINDIR}/made.sc use.cpp)")
        files["ochre/parcel/use.cpp"] = '#include <ochre/parcel/made.sc.h>\n'
        files["ochre/parcel/dependency.sc"] = 'include "tail.sc";\n'
        files["ochre/parcel/tail.sc"] = ""
        lib.tool_program(files, "tools/seed", "seed_driver")
        graph = lib.make(files, "ochre/parcel", "--sandboxing")
        node = schema_node(graph, "$(B)/ochre/parcel/made.sc.h")
        self.assertEqual(set(node["inputs"]), {
            TOOL, "$(B)/ochre/parcel/made.sc", "$(B)/ochre/parcel/anchor.dat",
            "$(S)/ochre/parcel/dependency.sc", "$(S)/ochre/parcel/tail.sc",
        })
        producer = lib.node_by_output(graph, "$(B)/ochre/parcel/anchor.dat")
        self.assertIn(producer["uid"], node["deps"])
        compiled = lib.node_by_output(graph, "$(B)/ochre/parcel/use.cpp.o")
        self.assertNotIn("$(S)/ochre/parcel/dependency.sc", compiled["inputs"])
        self.assertEqual(compiled["deps"], [node["uid"]])

    def test_explicit_source_and_build_schemas_keep_separate_shadow_closures(self):
        for root, imported in (("CURDIR", "physical"), ("BINDIR", "produced")):
            files = sc_files("RUN_PROGRAM(tools/seed OUT_NOAUTO anchor.dat main.sc OUTPUT_INCLUDES ${CURDIR}/produced.sc)", 'include "physical.sc";\n')
            files["ochre/parcel/ya.make"] = files["ochre/parcel/ya.make"].replace("SRCS(main.sc use.cpp)", "SRCS(${" + root + "}/main.sc use.cpp)")
            files["ochre/parcel/physical.sc"] = ""
            files["ochre/parcel/produced.sc"] = ""
            lib.tool_program(files, "tools/seed", "seed_driver")
            graph = lib.make(files, "ochre/parcel", "--sandboxing")
            node = schema_node(graph)
            prefix = "$(S)" if root == "CURDIR" else "$(B)"
            self.assertIn(prefix + "/ochre/parcel/main.sc", node["cmds"][0]["cmd_args"])
            self.assertIn(f"$(S)/ochre/parcel/{imported}.sc", node["inputs"])
            other = "produced" if imported == "physical" else "physical"
            self.assertNotIn(f"$(S)/ochre/parcel/{other}.sc", node["inputs"])
            if root == "BINDIR":
                self.assertIn("$(B)/ochre/parcel/anchor.dat", node["inputs"])
                self.assertIn(lib.node_by_output(graph, "$(B)/ochre/parcel/anchor.dat")["uid"], node["deps"])
            else:
                self.assertEqual(node["deps"], [lib.node_by_output(graph, TOOL)["uid"]])

    def test_imports_across_read_chunk_boundaries(self):
        files = sc_files(schema=padded_to_boundary(
            "// schema\r\n", 'include "boundary.sc";\r\n', "// filler\r\n", 7
        ) + 'include "last.sc";')
        files["ochre/parcel/boundary.sc"] = ""
        files["ochre/parcel/last.sc"] = ""
        graph = lib.make(files, "ochre/parcel")
        self.assertEqual(set(schema_node(graph)["inputs"]), {
            TOOL, "$(S)/ochre/parcel/main.sc", "$(S)/ochre/parcel/boundary.sc", "$(S)/ochre/parcel/last.sc",
        })

    def test_srcdir_and_explicit_source_identity_are_preserved(self):
        files = sc_files("SRCDIR(cedar)")
        files["cedar/main.sc"] = 'include "cedar_child.sc";\n'
        files["cedar/cedar_child.sc"] = ""
        files["ochre/parcel/use.cpp"] = '#include <cedar/main.sc.h>\n'
        graph = lib.make(files, "ochre/parcel")
        node = schema_node(graph, "$(B)/cedar/main.sc.h")
        self.assertEqual(set(node["inputs"]), {TOOL, "$(S)/cedar/main.sc", "$(S)/cedar/cedar_child.sc"})
        self.assertIn("$(S)/cedar/main.sc", node["cmds"][0]["cmd_args"])

    def test_run_inputs_select_sc_domain_and_text_suppresses_parsing(self):
        for mode in ("IN", "IN_NOPARSE"):
            files = sc_files("ADDINCL(FOR sc palette)\nRUN_PROGRAM(tools/seed " + mode + " main.sc OUT stamp.h)", 'include "leaf.sc";\n')
            files["ochre/parcel/ya.make"] = files["ochre/parcel/ya.make"].replace("SRCS(main.sc use.cpp)", "SRCS(use.cpp)")
            files["ochre/parcel/use.cpp"] = '#include <ochre/parcel/stamp.h>\n'
            files.pop("ochre/parcel/leaf.sc")
            files["palette/leaf.sc"] = ""
            lib.tool_program(files, "tools/seed", "seed_driver")
            graph = lib.make(files, "ochre/parcel")
            node = lib.node_by_output(graph, "$(B)/ochre/parcel/stamp.h")
            self.assertEqual("$(S)/palette/leaf.sc" in node["inputs"], mode == "IN")

    def test_noresolve_suppresses_source_directives(self):
        files = sc_files(schema='include "absent.sc";\n')
        files["ochre/parcel/ya.make"] = files["ochre/parcel/ya.make"].replace("LIBRARY()", "LIBRARY()\nSET(DONT_RESOLVE_INCLUDES yes)")
        files["reader/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(ochre/parcel)\nSRCS(read.cpp)\nEND()\n"
        files["reader/read.cpp"] = '#include <ochre/parcel/main.sc.h>\n'
        graph = lib.make(files, "reader")
        self.assertEqual(schema_node(graph)["inputs"], [TOOL, "$(S)/ochre/parcel/main.sc"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
