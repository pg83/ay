"""Source-selected resource peers in ordinary test and import-test contexts."""
import unittest

import lib
from test_python_srcs import python_base
from test_resource_peer_policy import globals_of


VERSIONS = ("11", "17", "21", "22", "23", "24", "25", "26")
COLORS = ("AMBER", "LAVENDER", "COPPER", "AZURE", "IVORY", "UMBER", "CORAL", "TEAL")
SELECTED = dict(zip(VERSIONS, COLORS))
TESTS = ("--target-platform-tests", "--test-size", "small", "--cache-tests", "--dist")


def config():
    text = "JDK_REAL_VERSION=\nJDK_VERSION=\n"
    for i, version in enumerate(VERSIONS):
        text += ('when' if i == 0 else 'elsewhen') + f' ($JDK_VERSION == "{version}") {{\nJDK_REAL_VERSION={version}\n}}\n'
    text += 'elsewhen ($MAPSMOBI_BUILD_TARGET && $OS_ANDROID) {\nJDK_REAL_VERSION=21\n}\notherwise {\nJDK_REAL_VERSION=17\n}\n'
    text += 'when ($USE_SYSTEM_JDK) {\nJDK_RESOURCE_PEERDIR=\n}\notherwise {\n'
    for version, color in SELECTED.items():
        text += f'when ($JDK_REAL_VERSION == "{version}") {{\nJDK_RESOURCE_PEERDIR=palette/{color.lower()}\n}}\n'
    return text + '}\n'


def fixture(body="", macro="PY3TEST", policy=""):
    files = python_base()
    files.update({
        "build/conf/java.conf": config(),
        "library/python/pytest/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "END()\n",
        "orchid/check/ya.make": f"{macro}()\n{lib.BARE_MODULE}{body}PEERDIR(${{JDK_RESOURCE_PEERDIR}})\nTEST_SRCS(test_flower.py)\nEND()\n",
        "orchid/check/test_flower.py": "def test_flower(): pass\n",
        "granite/caller/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(unit.cpp)\nEND()\n",
        "granite/caller/unit.cpp": "int granite;\n",
    })
    for version, color in SELECTED.items():
        files[f"palette/{color.lower()}/ya.make"] = (
            f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE({color} sbr:{version} WITH_{color} sbr:4{version})\n{policy}END()\n"
        )
    return files


def suite(graph, kind):
    return lib.node_by_output(graph, f"$(B)/orchid/check/test-results/{kind}/meta.json")


class TestResourceGlobalsTest(unittest.TestCase):
    def graph(self, files, *flags, roots=()):
        graph = lib.make(files, "orchid/check", *roots, *TESTS, *flags)
        ids = {n["uid"] for n in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertTrue(set(graph["result"]) <= ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))
        return graph

    def check(self, body="", expected="LAVENDER", flags=(), policy="", roots=()):
        graph = self.graph(fixture(body, policy=policy), *flags, roots=roots)
        want = set() if expected is None else {expected, "WITH_" + expected}
        for kind in ("import_test", "py3test"):
            run = suite(graph, kind)
            self.assertEqual(globals_of(run) & (set(COLORS) | {"WITH_" + c for c in COLORS}), want)
            args = run["cmds"][0]["cmd_args"]
            values = [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == "--global-resource"]
            self.assertEqual(values, sorted(set(values)))
            for color in want:
                self.assertIn(f"{color}_RESOURCE_GLOBAL::$(B)/resources/{color}", values)
        for color in want:
            self.assertIsNotNone(lib.node_by_output(graph, "$(B)/resources/" + color))
        return graph

    def test_default_and_each_declared_version_use_source_selected_peers(self):
        with self.subTest(version="default"):
            self.check()
        for version, color in SELECTED.items():
            with self.subTest(version=version):
                self.check(f"JDK_VERSION({version})\n", color)

    def test_default_macro_respects_variable_override_and_explicit_macro_order(self):
        for body, expected, flags in (
            ("DEFAULT_JDK_VERSION(25)\n", "CORAL", ()),
            ("DEFAULT_JDK_VERSION(25)\n", "COPPER", ("-DJDK_VERSION=21 22",)),
            ("DEFAULT_JDK_VERSION(25)\nIF (_JDK_VERSION != \"25\" OR NOT HAS_PER_MODULE_JDK_VERSION)\nJDK_VERSION(11)\nENDIF()\n", "CORAL", ()),
            ("DEFAULT_JDK_VERSION(25)\n", "COPPER", ("-DJDK_VERSION=21",)),
            ("", "COPPER", ("-DMAPSMOBI_BUILD_TARGET=no", "-DOS_ANDROID=0",)),
            ("SET(JDK_VERSION 22)\nDEFAULT_JDK_VERSION(25)\n", "AZURE", ()),
            ("SET(JDK_VERSION 22)\nJDK_VERSION(25)\n", "CORAL", ()),
            ("JDK_VERSION(25)\nSET(JDK_VERSION 22)\n", "CORAL", ()),
            ("JDK_VERSION(25)\nSET(JDK_REAL_VERSION 24)\n", "UMBER", ()),
            ("SET(JDK_REAL_VERSION 24)\nJDK_VERSION(25)\n", "CORAL", ()),
            ("SET(CHOSEN 23)\nJDK_VERSION($CHOSEN)\n", "IVORY", ()),
            ("DEFAULT(JDK_VERSION 26)\n", "LAVENDER", ()),
            ("SET(MAPSMOBI_BUILD_TARGET yes)\nSET(OS_ANDROID yes)\n", "LAVENDER", ()),
        ):
            with self.subTest(body=body, flags=flags):
                self.check(body, expected, flags)

    def test_configuration_recalculates_direct_dependents_in_native_order(self):
        for body, flags, expected in (
            ("", ("-DJDK_REAL_VERSION=23",), "LAVENDER"),
            ("JDK_VERSION(25)\n", ("-DJDK_REAL_VERSION=23",), "CORAL"),
            ("JDK_VERSION(25)\n", ("-DJDK_RESOURCE_PEERDIR=palette/amber",), "CORAL"),
            ("SET(JDK_RESOURCE_PEERDIR palette/amber)\nJDK_VERSION(25)\n", (), "CORAL"),
            ("SET(JDK_VERSION 25)\nSET(JDK_VERSION 22)\n", (), "LAVENDER"),
            ("JDK_VERSION(25)\nSET(JDK_REAL_VERSION 99)\n", (), "LAVENDER"),
        ):
            with self.subTest(body=body, flags=flags):
                self.check(body, expected, flags)

    def test_updated_scalar_does_not_implicitly_reexecute_resource_conditions(self):
        body = 'JDK_VERSION(25)\nSET(JDK_VERSION 22)\nIF (JDK_REAL_VERSION == "22")\nPEERDIR(palette/amber)\nENDIF()\n'
        graph = self.graph(fixture(body))
        for kind in ("import_test", "py3test"):
            seen = globals_of(suite(graph, kind))
            self.assertTrue({"CORAL", "AMBER", "WITH_CORAL", "WITH_AMBER"} <= seen)
            self.assertNotIn("AZURE", seen)

    def test_conf_truth_uses_nonempty_strings_while_source_if_uses_boolean_values(self):
        for value in ("no", "false", "0", "", "yes"):
            with self.subTest(value=value):
                body = f'SET(USE_SYSTEM_JDK "{value}")\nIF (USE_SYSTEM_JDK)\nPEERDIR(palette/amber)\nENDIF()\n'
                graph = self.graph(fixture(body))
                seen = globals_of(suite(graph, "py3test"))
                self.assertEqual("LAVENDER" in seen, value == "")
                self.assertEqual("AMBER" in seen, value == "yes")

    def test_restoring_an_original_without_an_inherited_value_keeps_its_namespace(self):
        files = fixture('SET(JDK_RESOURCE_PEERDIR palette/amber)\nJDK_VERSION(25)\nSET(JDK_REAL_VERSION 99)\n')
        files["build/conf/java.conf"] = files["build/conf/java.conf"].replace(
            'JDK_RESOURCE_PEERDIR=palette/lavender', 'UNRELATED_PEER=palette/lavender')
        graph = self.graph(files)
        self.assertIn("AMBER", globals_of(suite(graph, "py3test")))

    def test_java_macro_refreshes_the_same_source_selected_scalar(self):
        files = lib.java_tree()
        files.update(fixture())
        files["build/platform/java/jdk/jdk25/ya.make"] = lib.resource_lib("JDK25", 125)
        files["orchid/check/ya.make"] = (
            'JAVA_LIBRARY_NON_SPLIT()\nJDK_VERSION(25)\n'
            'PEERDIR(${JDK_RESOURCE_PEERDIR})\nJAVA_SRCS(Flower.java)\nEND()\n')
        files["orchid/check/Flower.java"] = "class Flower {}\n"
        graph = self.graph(files)
        run = lib.node_by_output(graph, "$(B)/orchid/check/test-results/java_style/meta.json")
        self.assertIn("CORAL", globals_of(run))
        self.assertNotIn("LAVENDER", globals_of(run))

    def test_binding_is_module_local_in_either_root_order(self):
        for roots in (("palette/teal", "granite/caller"), ("granite/caller", "palette/teal")):
            self.check("JDK_VERSION(25)\n", "CORAL", roots=roots)

    def test_cpp_without_explicit_resource_peer_does_not_gain_test_resources(self):
        files = fixture()
        files["granite/caller/ya.make"] = files["granite/caller/ya.make"].replace("END()", "SET(VALIDATE_DATA yes)\nFROM_SANDBOX(FILE 77 OUT_NOAUTO payload.bin)\nEND()")
        graph = self.graph(files, roots=("granite/caller",))
        run = lib.node_by_output(graph, "$(B)/granite/caller/test-results/validate_resource/meta.json")
        self.assertTrue(set(COLORS).isdisjoint(globals_of(run)))

    def test_system_jdk_and_source_mutators_suppress_or_restore_the_peer(self):
        for body, expected, flags in (
            ("", None, ("-DUSE_SYSTEM_JDK=/opt/system",)),
            ("SET(USE_SYSTEM_JDK /opt/system)\n", None, ()),
            ("ENABLE(USE_SYSTEM_JDK)\n", None, ()),
            ("ENABLE(USE_SYSTEM_JDK)\nDISABLE(USE_SYSTEM_JDK)\n", None, ()),
            ("SET_APPEND(USE_SYSTEM_JDK /opt/system)\n", None, ()),
            ("SET_APPEND(UNRELATED first second)\n", "LAVENDER", ()),
        ):
            with self.subTest(body=body):
                self.check(body, expected, flags)

    def test_private_resource_peer_still_obeys_pass_peers(self):
        self.check(expected=None, policy="SET(PASS_PEERS no)\n")

    def test_optional_config_is_not_required_and_default_binding_can_change(self):
        files = fixture("DEFAULT(JDK_RESOURCE_PEERDIR palette/teal)\n")
        del files["build/conf/java.conf"]
        graph = self.graph(files)
        self.assertIn("TEAL", globals_of(suite(graph, "py3test")))
        files["orchid/check/ya.make"] = files["orchid/check/ya.make"].replace(
            "DEFAULT(JDK_RESOURCE_PEERDIR palette/teal)", "DEFAULT(USE_SYSTEM_JDK yes)\nDEFAULT(JDK_VERSION 25)\n")
        self.graph(files)

    def test_invalid_macro_arguments_report_configuration_errors(self):
        for macro in ("JDK_VERSION()", "JDK_VERSION(17 25)", "JDK_VERSION(99)", "DEFAULT_JDK_VERSION(99)",
                      "SET(JDK_RESOURCE_PEERDIR palette/amber)\nJDK_VERSION(25)\nSET(JDK_REAL_VERSION 99)", 'SET(CHOSEN "")\nDEFAULT_JDK_VERSION($CHOSEN)'):
            with self.subTest(macro=macro):
                process = lib.make_process(fixture(macro + "\n"), "orchid/check", *TESTS)
                self.assertNotEqual(process.returncode, 0)
                self.assertTrue("expects one argument" in process.stderr or "Invalid jdk version" in process.stderr or "JDK_RESOURCE_PEERDIR" in process.stderr)

    def test_tool_and_runtime_resources_do_not_enter_the_owner_namespace(self):
        files = fixture()
        for path, resource in (("brush/tool", "TOOL_PAINT"), ("stage/runtime", "RUNTIME_PAINT")):
            lib.tool_program(files, path, path.rsplit("/", 1)[-1])
            files[path + "/ya.make"] = files[path + "/ya.make"].replace("END()", f"DECLARE_EXTERNAL_RESOURCE({resource} sbr:91)\nEND()")
        files["orchid/check/ya.make"] = files["orchid/check/ya.make"].replace(
            "END()", "RUN_PROGRAM(brush/tool OUT petal.py)\nPY_SRCS(petal.py)\nDEPENDS(stage/runtime)\nEND()")
        graph = self.graph(files)
        for kind in ("import_test", "py3test"):
            self.assertTrue({"TOOL_PAINT", "RUNTIME_PAINT"}.isdisjoint(globals_of(suite(graph, kind))))
        produced = lib.node_by_output(graph, "$(B)/orchid/check/petal.py")
        tool = lib.node_by_output(graph, "$(B)/brush/tool/tool")
        self.assertIn(tool["uid"], produced["deps"])
        self.assertIn("$(B)/brush/tool/tool", produced["inputs"])
        runtime = lib.node_by_output(graph, "$(B)/stage/runtime/runtime")
        self.assertIn(runtime["uid"], suite(graph, "py3test")["deps"])
        for resource in ("TOOL_PAINT", "RUNTIME_PAINT"):
            self.assertIsNotNone(lib.node_by_output(graph, "$(B)/resources/" + resource))


if __name__ == "__main__":
    unittest.main()
