import unittest

import lib
from test_python_cython import CYTHON, args, python_base


SUPPORTED = (
    ('"fern.h"', "fern.h"),
    ("'fern.h'", "fern.h"),
    ('r"fern.h"', "fern.h"),
    ("r'fern.h'", "fern.h"),
    ('"<fern.h>"', "fern.h"),
    ("'<fern.h>'", "fern.h"),
    ('r"<fern.h>"', "fern.h"),
    ("r'<fern.h>'", "fern.h"),
    ('r"<fe"rn.h>"', 'fe"rn.h'),
    ('r"fern.h\\"other.h"', "fern.h\\"),
)
# Empty captures are grammatical, but the pinned processor discards them.
NO_HEADERS = (
    'R"fern.h"', 'u"fern.h"', 'b"fern.h"', 'rr"fern.h"',
    'br"fern.h"', 'r "fern.h"', 'r', '<fern.h>', 'r<fern.h>',
    'r"fern.h', 'r""', 'r"<>"', 'r"fern<h>"',
    'r"<fern.h"', 'r"<<fern.h>>"', 'r"<fern.h>tail"',
)


def fixture(source, mode="CYTHON_CPP"):
    files = python_base()
    files.update({
        "orchid/ya.make": (
            f"PY3_LIBRARY(bundle)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
            f"ADDINCL(headers FOR cython shared)\nPY_SRCS({mode} packet.pyx)\nEND()\n"
        ),
        "orchid/packet.pyx": source,
        "orchid/fern.h": '#include "decoy.h"\n',
        "orchid/decoy.h": "#pragma once\n",
        "headers/fern.h": "#include <deeper.h>\n",
        'headers/fe"rn.h': "#include <deeper.h>\n",
        "headers/fern.h\\": "#include <deeper.h>\n",
        "headers/deeper.h": '#include "bottom.h"\n',
        "headers/bottom.h": "#pragma once\n",
        "shared/nested.pxi": 'cdef extern from r"fern.h":\n    pass\n',
    })
    return files


def generated_nodes(graph, output="$(B)/orchid/packet.pyx.cpp"):
    cy = lib.node_by_output(graph, output)
    cc = lib.node_by_output(graph, output + ".o")
    return cy, cc


class CythonCdefHeadersTest(unittest.TestCase):
    def assert_native_domain(self, graph, cy, cc, header):
        self.assertIn("$(S)/orchid/packet.pyx", cy["inputs"])
        self.assertIn(CYTHON, cy["inputs"])
        self.assertIn("$(S)/contrib/tools/cython/Cython/Utility/CppSupport.cpp", cy["inputs"])
        self.assertIn(cy["outputs"][0], cc["inputs"])
        self.assertIn(cy["uid"], cc["deps"])
        self.assertIn("-I$(S)/headers", args(cc))
        self.assertNotIn("-I$(S)/headers", args(cy))
        for path in ("fern.h", 'fe"rn.h', "fern.h\\"):
            full = "$(S)/headers/" + path
            self.assertNotIn(full, cy["inputs"])
            self.assertEqual(full in cc["inputs"], path == header)
        for name in ("deeper.h", "bottom.h"):
            full = "$(S)/headers/" + name
            self.assertNotIn(full, cy["inputs"])
            self.assertEqual(full in cc["inputs"], header is not None)
        self.assertNotIn("$(S)/orchid/fern.h", cc["inputs"])
        self.assertNotIn("$(S)/orchid/decoy.h", cc["inputs"])
        archive = lib.node_by_output(graph, "$(B)/orchid/libpy3bundle.a")
        self.assertIn(cc["outputs"][0], archive["inputs"])
        self.assertIn(cc["uid"], archive["deps"])

    def test_supported_raw_quote_and_angle_captures_feed_only_native_closure(self):
        for expression, capture in SUPPORTED:
            with self.subTest(expression=expression):
                graph = lib.make(fixture("cdef extern from " + expression + ":\n    pass\n"), "orchid")
                self.assert_native_domain(graph, *generated_nodes(graph), capture)

    def test_invalid_forms_and_empty_captures_do_not_add_headers(self):
        for expression in NO_HEADERS:
            with self.subTest(expression=expression):
                graph = lib.make(fixture("cdef extern from " + expression + ":\n    pass\n"), "orchid")
                self.assert_native_domain(graph, *generated_nodes(graph), None)

    def test_cdef_whitespace_and_tail_follow_the_pinned_grammar(self):
        graph = lib.make(fixture('\t cdef\vextern\ffrom\tr"fern.h" nogil: # opaque tail\n    pass\n'), "orchid")
        self.assert_native_domain(graph, *generated_nodes(graph), "fern.h")

    def test_disabled_resolution_does_not_discover_raw_headers(self):
        files = fixture('cdef extern from r"fern.h":\n    pass\n')
        files["orchid/ya.make"] = files["orchid/ya.make"].replace("PY_SRCS(", "SET(DONT_RESOLVE_INCLUDES yes)\nPY_SRCS(")
        graph = lib.make(files, "orchid")
        self.assert_native_domain(graph, *generated_nodes(graph), None)

    def test_include_keeps_its_separate_prefix_grammar_and_cython_domain(self):
        for prefix in ("", "r"):
            with self.subTest(prefix=prefix):
                graph = lib.make(fixture('include ' + prefix + '"nested.pxi"\n'), "orchid")
                cy, cc = generated_nodes(graph)
                self.assertEqual("$(S)/shared/nested.pxi" in cy["inputs"], not prefix)
                self.assert_native_domain(graph, cy, cc, None if prefix else "fern.h")

    def test_generated_header_keeps_build_owner_main_output_and_producer_edges(self):
        files = fixture('cdef extern from r"<generated/bridge.h>":\n    pass\n')
        files["orchid/ya.make"] = files["orchid/ya.make"].replace("PY_SRCS(", "PEERDIR(generated)\nPY_SRCS(")
        files.update({
            "generated/ya.make": (
                "GEN_LIBRARY()\nRUN_PROGRAM(tools/forge IN seed.txt "
                "OUT_NOAUTO proof.bin bridge.h OUTPUT_INCLUDES ${ARCADIA_ROOT}/headers/deeper.h)\nEND()\n"
            ),
            "generated/seed.txt": "real generator input\n",
            "generated/bridge.h": '#include "wrong.h"\n',
            "generated/wrong.h": "#pragma once\n",
        })
        lib.tool_program(files, "tools/forge", "forge")
        graph = lib.make(files, "orchid")
        cy, cc = generated_nodes(graph)
        producer = lib.node_by_output(graph, "$(B)/generated/bridge.h")
        tool = lib.node_by_output(graph, "$(B)/tools/forge/forge")
        self.assertEqual(producer["outputs"], ["$(B)/generated/proof.bin", "$(B)/generated/bridge.h"])
        self.assertIn("$(S)/generated/seed.txt", producer["inputs"])
        self.assertIn(tool["outputs"][0], producer["inputs"])
        self.assertIn(tool["uid"], producer["foreign_deps"]["tool"])
        self.assertIn(cy["uid"], cc["deps"])
        self.assertIn(producer["uid"], cc["deps"])
        self.assertNotIn(producer["uid"], cy["deps"])
        for output in producer["outputs"]:
            self.assertIn(output, cc["inputs"])
            self.assertNotIn(output, cy["inputs"])
        for name in ("deeper.h", "bottom.h"):
            self.assertIn("$(S)/headers/" + name, cc["inputs"])
            self.assertNotIn("$(S)/headers/" + name, cy["inputs"])
        self.assertNotIn("$(S)/generated/bridge.h", cc["inputs"])
        self.assertNotIn("$(S)/generated/wrong.h", cc["inputs"])
        archive = lib.node_by_output(graph, "$(B)/orchid/libpy3bundle.a")
        self.assertIn(cc["uid"], archive["deps"])

    def test_ordinary_and_public_c_and_cpp_outputs_keep_their_compile_edges(self):
        for mode, outputs in (
            ("CYTHON_C", ["packet.pyx.c"]),
            ("CYTHON_CPP", ["packet.pyx.cpp"]),
            ("CYTHON_C_H", ["packet.c", "packet.h"]),
            ("CYTHON_CPP_API_H", ["packet.cpp", "packet.h", "packet_api.h"]),
        ):
            with self.subTest(mode=mode):
                graph = lib.make(fixture('cdef extern from r"fern.h":\n    pass\n', mode), "orchid")
                full = ["$(B)/orchid/" + output for output in outputs]
                cy, cc = generated_nodes(graph, full[0])
                self.assertEqual(cy["outputs"], full)
                self.assertEqual(args(cy)[-2:], ["-o", full[0]])
                self.assert_native_domain(graph, cy, cc, "fern.h")

    def test_shared_lexical_include_keeps_each_compiler_context_in_both_orders(self):
        files = fixture("")
        files["shared/context.pxi"] = 'cdef extern from r"selected.h":\n    pass\n'
        for owner, color in (("blue_owner", "blue"), ("green_owner", "green")):
            files[owner + "/ya.make"] = (
                f"PY3_LIBRARY(bundle)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
                f"ADDINCL({color} FOR cython shared)\nPY_SRCS(CYTHON_CPP packet.pyx)\nEND()\n"
            )
            files[owner + "/packet.pyx"] = 'include "context.pxi"\n'
            files[color + "/selected.h"] = f'#include "{color}_leaf.h"\n'
            files[color + "/" + color + "_leaf.h"] = "#pragma once\n"
        for targets in (("blue_owner", "green_owner"), ("green_owner", "blue_owner")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for owner, color, other in (("blue_owner", "blue", "green"), ("green_owner", "green", "blue")):
                    cy, cc = generated_nodes(graph, f"$(B)/{owner}/packet.pyx.cpp")
                    self.assertIn("$(S)/shared/context.pxi", cy["inputs"])
                    self.assertIn(cy["uid"], cc["deps"])
                    self.assertIn(cy["outputs"][0], cc["inputs"])
                    self.assertIn(f"$(S)/{color}/selected.h", cc["inputs"])
                    self.assertIn(f"$(S)/{color}/{color}_leaf.h", cc["inputs"])
                    self.assertNotIn(f"$(S)/{other}/selected.h", cc["inputs"])
                    self.assertNotIn(f"$(S)/{color}/selected.h", cy["inputs"])
                    archive = lib.node_by_output(graph, f"$(B)/{owner}/libpy3bundle.a")
                    self.assertIn(cc["uid"], archive["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
