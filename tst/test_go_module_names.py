import json
import unittest

import lib
from test_go_tests import STD, go_module, go_tool_args, go_world


TOOL = "$(B)/tools/inkgen/ink-driver"
PROBE = """RUN_PROGRAM(tools/inkgen
    --desc ${BINDIR}/seed.desc
    name=${REALPRJNAME} header=${GO_PACKAGE_VALUE}
    prefix=${MODULE_PREFIX} package=${_GO_PACKAGE_VALUE}
    IN_NOPARSE ${BINDIR}/seed.desc STDOUT named.go)
"""


def world():
    files = go_world()
    files["shared/names.inc"] = PROBE
    files["build/scripts/fs_tools.py"] = ""
    lib.tool_program(files, "tools/inkgen", "ink-driver")
    lib.tool_program(files, "library/go/core/resource/cc", "cc")
    go_module(files, "library/go/core/resource")
    for peer in ("os", "testing", "testing/internal/testdeps", "internal/coverage/cfile", "runtime/coverage"):
        go_module(files, f"{STD}/{peer}")
    go_module(files, "library/go/test/yatest")
    go_module(files, "tools/go_test_miner", module="GO_PROGRAM")
    return files


def module(files, directory, header="GO_LIBRARY()", before="", body=""):
    files[f"{directory}/ya.make"] = (
        f"{before}{header}\nCOPY_FILE(seed.in seed.desc)\n"
        "INCLUDE(${ARCADIA_ROOT}/shared/names.inc)\n"
        f"{body}END()\n"
    )
    files[f"{directory}/seed.in"] = "descriptor\n"
    files[f"{directory}/payload.txt"] = "first\n"
    files[f"{directory}/second.txt"] = "second\n"


class GoModuleNamesTest(unittest.TestCase):
    def probe(self, graph, directory, name, header="", prefix="", package="", output="named.go"):
        build = f"$(B)/{directory}"
        node = lib.node_by_output(graph, f"{build}/{output}")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            TOOL, "--desc", f"{build}/seed.desc", f"name={name}",
            f"header={header}", f"prefix={prefix}", f"package={package}",
        ])
        for dependency in (TOOL, f"{build}/seed.desc"):
            self.assertIn(dependency, node["inputs"])
            self.assertIn(lib.node_by_output(graph, dependency)["uid"], node["deps"])
        return node

    def consumer(self, graph, directory, output, producer):
        node = lib.node_by_output(graph, f"$(B)/{directory}/{output}")
        self.assertIn(producer["outputs"][0], node["inputs"])
        self.assertIn(producer["uid"], node["deps"])
        self.assertIn(producer["outputs"][0], go_tool_args(node))
        return node

    def resource_packages(self, graph, directory):
        nodes = [node for node in graph["graph"]
                 if node["outputs"][0].startswith(f"$(B)/{directory}/resource.")]
        return sorted(node["cmds"][0]["cmd_args"][2] for node in nodes)

    def test_shared_lexical_include_has_each_module_name_and_real_producers(self):
        files = world()
        module(files, "orchard/plum")
        module(files, "orchard/pear", "GO_LIBRARY(CustomPear)")
        graph = lib.make(files, "orchard/plum", "orchard/pear")
        for directory, name, header in (("orchard/plum", "plum", ""), ("orchard/pear", "CustomPear", "CustomPear")):
            producer = self.probe(graph, directory, name, header)
            self.consumer(graph, directory, name + ".a", producer)

    def test_prefix_is_separate_and_first_name_wins(self):
        files = world()
        module(files, "grove/elm", "GO_LIBRARY(PREFIX first_ Canopy ignored PREFIX ${STEM})", "SET(STEM last_)\n")
        graph = lib.make(files, "grove/elm")
        producer = self.probe(graph, "grove/elm", "Canopy", "Canopy", "last_")
        self.consumer(graph, "grove/elm", "last_Canopy.a", producer)

    def test_prefix_only_header_uses_basename_and_missing_prefix_warns(self):
        files = world()
        module(files, "grove/ash", "GO_LIBRARY(PREFIX tagged_)")
        module(files, "grove/oak", "GO_LIBRARY(PREFIX)")
        result = lib.make_process(files, "grove/ash", "grove/oak")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("prefix not specified after PREFIX keyword", result.stderr)
        graph = json.loads(result.stdout)
        producer = self.probe(graph, "grove/ash", "ash", prefix="tagged_")
        self.consumer(graph, "grove/ash", "tagged_ash.a", producer)
        producer = self.probe(graph, "grove/oak", "oak")
        self.consumer(graph, "grove/oak", "oak.a", producer)

    def test_name_bound_before_header_wins_over_header_argument(self):
        files = world()
        module(files, "grove/pine", "GO_LIBRARY(Ignored PREFIX p_)", "SET(REALPRJNAME Evergreen)\n")
        graph = lib.make(files, "grove/pine")
        producer = self.probe(graph, "grove/pine", "Evergreen", "Evergreen", "p_")
        self.consumer(graph, "grove/pine", "p_Evergreen.a", producer)

    def test_body_set_changes_subsequent_expansion_and_package_but_keeps_output_name(self):
        files = world()
        files["shared/after.inc"] = PROBE.replace("named.go", "after.go")
        module(files, "garden/lily", "GO_LIBRARY(Blossom)", body="""
RESOURCE(payload.txt first)
SET(REALPRJNAME Flower)
SET(MODULE_PREFIX branch_)
INCLUDE(${ARCADIA_ROOT}/shared/after.inc)
RESOURCE(second.txt second)
""")
        graph = lib.make(files, "garden/lily")
        self.probe(graph, "garden/lily", "Blossom", "Blossom")
        producer = self.probe(graph, "garden/lily", "Flower", "Blossom", prefix="branch_", output="after.go")
        self.consumer(graph, "garden/lily", "Blossom.a", producer)
        self.assertEqual(self.resource_packages(graph, "garden/lily"), ["Blossom", "Flower"])

    def test_package_override_is_distinct_from_real_name_and_header_name(self):
        files = world()
        files["shared/after.inc"] = PROBE.replace("named.go", "after.go")
        module(files, "garden/iris", "GO_LIBRARY(Petal)", body="""
GO_PACKAGE_NAME(${REALPRJNAME}_pkg)
SET(REALPRJNAME Stem)
INCLUDE(${ARCADIA_ROOT}/shared/after.inc)
RESOURCE(payload.txt first)
""")
        graph = lib.make(files, "garden/iris")
        self.probe(graph, "garden/iris", "Petal", "Petal")
        producer = self.probe(graph, "garden/iris", "Stem", "Petal", package="Petal_pkg", output="after.go")
        self.consumer(graph, "garden/iris", "Petal.a", producer)
        self.assertEqual(self.resource_packages(graph, "garden/iris"), ["Petal_pkg"])

    def test_program_package_stays_main_and_test_header_package_survives_body_set(self):
        for kind, directory, package in (("GO_PROGRAM", "tools/fern", "main"), ("GO_TEST", "checks/reed", "Suite")):
            with self.subTest(kind=kind):
                files = world()
                imports = "SET(GO_TEST_IMPORT_PATH original/imported)\n" if kind == "GO_TEST" else ""
                module(files, directory, f"{kind}(Suite)", body=imports + "SET(REALPRJNAME BodyName)\nRESOURCE(payload.txt first)\n")
                graph = lib.make(files, directory)
                producer = self.probe(graph, directory, "Suite", "Suite")
                self.consumer(graph, directory, "Suite", producer)
                self.assertEqual(self.resource_packages(graph, directory), [package])

    def test_empty_package_override_and_import_path_fall_back_to_current_real_name(self):
        files = world()
        files["shared/after.inc"] = PROBE.replace("named.go", "after.go")
        module(files, "garden/rose", "GO_LIBRARY(Root)", body="""
GO_PACKAGE_NAME(Bud)
RESOURCE(payload.txt first)
SET(_GO_PACKAGE_VALUE)
SET(GO_TEST_IMPORT_PATH)
SET(REALPRJNAME Bloom)
INCLUDE(${ARCADIA_ROOT}/shared/after.inc)
RESOURCE(second.txt second)
""")
        graph = lib.make(files, "garden/rose")
        producer = self.probe(graph, "garden/rose", "Bloom", "Root", output="after.go")
        self.consumer(graph, "garden/rose", "Root.a", producer)
        self.assertEqual(self.resource_packages(graph, "garden/rose"), ["Bloom", "Bud"])

    def test_implicit_test_package_uses_import_path(self):
        files = world()
        module(files, "checks/moss", "GO_TEST()", body="SET(GO_TEST_IMPORT_PATH original/imported)\nRESOURCE(payload.txt first)\n")
        graph = lib.make(files, "checks/moss")
        producer = self.probe(graph, "checks/moss", "moss")
        self.consumer(graph, "checks/moss", "moss", producer)
        self.assertEqual(self.resource_packages(graph, "checks/moss"), ["imported"])

    def test_dll_keeps_header_name_prefix_and_main_package(self):
        files = world()
        module(files, "plugins/pollen", "GO_DLL(Pollen PREFIX flower_)\nGO_PACKAGE_NAME(main)", body="RESOURCE(payload.txt first)\n")
        graph = lib.make(files, "plugins/pollen")
        producer = self.probe(graph, "plugins/pollen", "Pollen", "Pollen", "flower_", "main")
        self.consumer(graph, "plugins/pollen", "flower_Pollen.so", producer)
        self.assertEqual(self.resource_packages(graph, "plugins/pollen"), ["main"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
