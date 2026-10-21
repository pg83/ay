import json
import unittest

import lib
from test_go_cgo import cgo_world, NO_PLATFORM


PACKAGE = "lib/binding"
BUILD = "$(B)/" + PACKAGE
SOURCE = "$(S)/" + PACKAGE


def binding(bridge, extra="", source_dir=PACKAGE):
    files = cgo_world()
    files.update({
        f"{PACKAGE}/ya.make": "GO_LIBRARY()\n" + NO_PLATFORM + extra + "SRCS(CGO_EXPORT bridge.c)\nCGO_SRCS(bind.go)\nEND()\n",
        f"{PACKAGE}/bind.go": 'package binding\nimport "C"\n',
        f"{source_dir}/bridge.c": bridge,
    })
    return files


def copied(graph):
    return lib.node_by_output(graph, BUILD + "/bridge.c")


def compiled(graph):
    return lib.node_by_output(graph, BUILD + "/bridge.c.o")


class GoCopyContextTest(unittest.TestCase):
    def test_bare_generated_include_is_output_context_only(self):
        graph = lib.make(binding('#include "_cgo_export.h"\n'), PACKAGE)
        header = BUILD + "/_cgo_export.h"
        producer = lib.node_by_output(graph, header)
        self.assertNotIn(header, copied(graph)["inputs"])
        self.assertNotIn(producer["uid"], copied(graph)["deps"])
        self.assertIn(header, compiled(graph)["inputs"])
        self.assertIn(producer["uid"], compiled(graph)["deps"])

    def test_rooted_generated_include_remains_an_input_dependency(self):
        graph = lib.make(binding(f'#include "{PACKAGE}/_cgo_export.h"\n'), PACKAGE)
        header = BUILD + "/_cgo_export.h"
        producer = lib.node_by_output(graph, header)
        for node in (copied(graph), compiled(graph)):
            self.assertIn(header, node["inputs"])
            self.assertIn(producer["uid"], node["deps"])

    def test_explicit_build_search_and_configure_addincl_preserve_generated_inputs(self):
        for extra in (
            "ADDINCL(${ARCADIA_BUILD_ROOT}/" + PACKAGE + ")\n",
            "CONFIGURE_FILE(seed.h.in generated.h)\n",
        ):
            with self.subTest(extra=extra):
                files = binding('#include "_cgo_export.h"\n', extra)
                files[f"{PACKAGE}/seed.h.in"] = ""
                graph = lib.make(files, PACKAGE)
                header = BUILD + "/_cgo_export.h"
                producer = lib.node_by_output(graph, header)
                self.assertIn(header, copied(graph)["inputs"])
                self.assertIn(producer["uid"], copied(graph)["deps"])

    def test_copied_source_keeps_original_include_directory(self):
        files = binding('#include "which.h"\n', "SRCDIR(shared)\n", "shared")
        files["shared/which.h"] = ""
        files[f"{PACKAGE}/which.h"] = ""
        graph = lib.make(files, PACKAGE)
        for node in (copied(graph), compiled(graph)):
            self.assertIn("$(S)/shared/which.h", node["inputs"])
            self.assertNotIn(SOURCE + "/which.h", node["inputs"])

    def test_unprovided_include_is_still_reported_by_compilation(self):
        for header in ("missing.h", "private/_cgo_export.h"):
            with self.subTest(header=header):
                result = lib.make_process(binding(f'#include "{header}"\n'), PACKAGE)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f'unresolved include "{header}"', result.stderr)

    def test_copy_source_header_diagnostics_remain_visible_to_compilation(self):
        files = binding('#include "public.h"\n')
        files[f"{PACKAGE}/public.h"] = '#include "missing.h"\n'
        result = lib.make_process(files, PACKAGE)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f'{SOURCE}/public.h: unresolved include "missing.h"', result.stderr)

    def test_original_native_consumer_reports_cached_copy_source_diagnostics(self):
        files = binding('#include "_cgo_export.h"\n')
        files["native/ya.make"] = "LIBRARY()\n" + NO_PLATFORM + "SRCDIR(" + PACKAGE + ")\nSRCS(bridge.c)\nEND()\n"
        result = lib.make_process(files, "native", PACKAGE, "-k")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f'{SOURCE}/bridge.c: unresolved include "_cgo_export.h"', result.stderr)
        graph = json.loads(result.stdout)
        native = next(node for node in graph["graph"] if node["kv"].get("p") == "CC" and node["outputs"][0].startswith("$(B)/native/"))
        self.assertLess(copied(graph)["uid"], native["uid"])
        self.assertIn(BUILD + "/_cgo_export.h", compiled(graph)["inputs"])
        self.assertNotIn(BUILD + "/_cgo_export.h", native["inputs"])

    def test_native_source_requires_a_declared_build_search_directory(self):
        for include in ("", "ADDINCL(${ARCADIA_BUILD_ROOT}/native)\n"):
            with self.subTest(include=include):
                files = {
                    "native/ya.make": "LIBRARY()\n" + NO_PLATFORM + include + "COPY_FILE(seed.txt generated.h)\nSRCS(main.cpp)\nEND()\n",
                    "native/main.cpp": '#include "generated.h"\n',
                    "native/seed.txt": "",
                    "build/scripts/fs_tools.py": "",
                }
                graph = lib.make(files, "native", "-k")
                native = lib.node_by_output(graph, "$(B)/native/main.cpp.o")
                header = "$(B)/native/generated.h"
                self.assertEqual(header in native["inputs"], bool(include))
                if include:
                    producer = lib.node_by_output(graph, header)
                    self.assertIn(producer["uid"], native["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
