import json
import unittest

import lib


def project(body, extra="", path="copper/app"):
    return {
        f"{path}/ya.make": (
            f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(copper/sdk)\n"
            f"{body}\nSRCS(probe.cpp)\nEND()\n"
        ),
        f"{path}/probe.cpp": "int main() { return 0; }\n",
        "copper/sdk/ya.make": (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:841)\n"
            "DECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:842)\nEND()\n"
        ),
        "build/ymake_conf.py": lib.ymake_conf(extra=extra),
    }


class WindowsManifestTest(unittest.TestCase):
    def graph(self, files, *targets, platform="default-linux-x86_64"):
        result = lib.make_process(files, *targets, "--target-platform", platform)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("unmodelled", result.stderr)
        return json.loads(result.stdout)

    def assert_unused(self, graph):
        for node in graph["graph"]:
            self.assertFalse(any("manifest" in path for path in node["inputs"] + node["outputs"]))
            for cmd in node["cmds"]:
                self.assertFalse(any(arg.startswith(("/MANIFEST", "/MANIFESTINPUT"))
                                     for arg in cmd["cmd_args"]))

    def test_scalar_macro_matches_explicit_set_and_preserves_custom_flags(self):
        for argument, value in (("chosen.manifest", "chosen.manifest"),
                                ('"quoted choice.manifest"', "quoted choice.manifest"),
                                ("CAPS", "CAPS"), ("off", "off"), ("no", "no"),
                                ("0", "0"), ('""', "")):
            with self.subTest(argument=argument):
                tail = (f'\nIF (WINDOWS_MANIFEST == "{value}")\nCFLAGS(-DVALUE_OK)\nENDIF()\n'
                        "LDFLAGS(-Wl,--custom-choice)")
                actual = self.graph(project(f"WINDOWS_MANIFEST({argument})" + tail), "copper/app")
                expected = self.graph(project(f"SET(WINDOWS_MANIFEST {value})" + tail), "copper/app")
                self.assertEqual(actual, expected)
                obj = lib.node_by_output(actual, "$(B)/copper/app/probe.cpp.o")
                self.assertIn("-DVALUE_OK", obj["cmds"][0]["cmd_args"])
                link = lib.node_by_output(actual, "$(B)/copper/app/app")
                self.assertTrue(any("-Wl,--custom-choice" in cmd["cmd_args"] for cmd in link["cmds"]))
                self.assert_unused(actual)

    def test_default_macro_is_assignment_and_can_be_replaced_or_cleared(self):
        for after, value in (("", "build/long-path.manifest"),
                             ("WINDOWS_MANIFEST(replaced.manifest)", "replaced.manifest"),
                             ("SET(WINDOWS_MANIFEST)", ""),
                             ('WINDOWS_MANIFEST("")', "")):
            with self.subTest(after=after):
                tail = f'\n{after}\nIF (WINDOWS_MANIFEST == "{value}")\nCFLAGS(-DFINAL_OK)\nENDIF()'
                actual = self.graph(project("WINDOWS_LONG_PATH_MANIFEST()" + tail), "copper/app")
                expected = self.graph(project("SET(WINDOWS_MANIFEST build/long-path.manifest)" + tail),
                                      "copper/app")
                self.assertEqual(actual, expected)
                self.assertIn("-DFINAL_OK", lib.node_by_output(actual, "$(B)/copper/app/probe.cpp.o")
                              ["cmds"][0]["cmd_args"])
                self.assert_unused(actual)

    def test_argument_expansion_is_captured_before_later_assignment(self):
        files = project(
            "SET(CHOICE first.manifest)\nWINDOWS_MANIFEST($CHOICE)\nSET(CHOICE later.manifest)\n"
            'IF (WINDOWS_MANIFEST == "first.manifest")\nCFLAGS(-DCAPTURED)\nENDIF()\n'
            "WINDOWS_MANIFEST(${CONFIG_ALIAS})\n"
            'IF (WINDOWS_MANIFEST == "deep choice.manifest")\nCFLAGS(-DCONFIG_EXPANDED)\nENDIF()',
            extra="CONFIG_ALIAS=$CONFIG_LEAF\nCONFIG_LEAF=deep choice.manifest\n")
        graph = self.graph(files, "copper/app")
        args = lib.node_by_output(graph, "$(B)/copper/app/probe.cpp.o")["cmds"][0]["cmd_args"]
        self.assertIn("-DCAPTURED", args)
        self.assertIn("-DCONFIG_EXPANDED", args)
        self.assert_unused(graph)

    def test_shared_lexical_include_uses_each_module_environment_and_real_tool(self):
        files = project("", path="copper/amber")
        del files["copper/amber/ya.make"]
        files["shared/manifest.inc"] = (
            "WINDOWS_MANIFEST($CHOICE)\n"
            "RUN_PROGRAM(tools/scribe --choice=${WINDOWS_MANIFEST} OUT stamp.cpp)\n"
        )
        for name, value in (("amber", "amber.manifest"), ("violet", "violet.manifest")):
            files[f"copper/{name}/ya.make"] = (
                f"LIBRARY()\n{lib.BARE_MODULE}SET(CHOICE {value})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/manifest.inc)\nSET(CHOICE later)\nEND()\n"
            )
        lib.tool_program(files, "tools/scribe", "scribe-tool")
        for targets in (("copper/amber", "copper/violet"), ("copper/violet", "copper/amber")):
            with self.subTest(targets=targets):
                graph = self.graph(files, *targets)
                tool = lib.node_by_output(graph, "$(B)/tools/scribe/scribe-tool")
                for name in ("amber", "violet"):
                    generated = lib.node_by_output(graph, f"$(B)/copper/{name}/stamp.cpp")
                    self.assertEqual(generated["cmds"][0]["cmd_args"],
                                     ["$(B)/tools/scribe/scribe-tool", f"--choice={name}.manifest"])
                    self.assertIn(tool["uid"], generated["deps"])
                    self.assertIn(tool["outputs"][0], generated["inputs"])
                    compiled = lib.node_by_output(graph, f"$(B)/copper/{name}/stamp.cpp.o")
                    self.assertIn(generated["uid"], compiled["deps"])

    def test_non_msvc_never_demands_missing_or_generated_manifest(self):
        for platform, extra in (("default-linux-x86_64", "MSVC=no\n"),
                                ("default-windows-x86_64", "MSVC=no\nCOMPILER_ID=CLANG\n"),
                                ("default-linux-x86_64", "MSVC=off\nCOMPILER_ID=CUSTOM\n")):
            for declaration in ("WINDOWS_MANIFEST(absent.manifest)",
                                "WINDOWS_MANIFEST(${BINDIR}/unused.manifest)",
                                "WINDOWS_LONG_PATH_MANIFEST()"):
                with self.subTest(platform=platform, declaration=declaration):
                    dormant = "RUN_PROGRAM(tools/dormant OUT_NOAUTO unused.manifest)\n"
                    files = project(dormant, extra)
                    lib.tool_program(files, "tools/dormant", "dormant-tool")
                    baseline = self.graph(files, "copper/app", platform=platform)
                    files["copper/app/ya.make"] = files["copper/app/ya.make"].replace(
                        dormant, dormant + declaration + "\n")
                    graph = self.graph(files, "copper/app", platform=platform)
                    self.assertEqual(graph, baseline)
                    self.assert_unused(graph)

    def test_macro_and_plain_set_overwrite_configuration_defaults(self):
        body = ("WINDOWS_MANIFEST(first.manifest)\nSET(WINDOWS_MANIFEST second.manifest)\n"
                'IF (WINDOWS_MANIFEST == "second.manifest")\nCFLAGS(-DSET_WINS)\nENDIF()\n'
                "WINDOWS_MANIFEST(final.manifest)\n"
                'IF (WINDOWS_MANIFEST == "final.manifest")\nCFLAGS(-DMACRO_WINS)\nENDIF()')
        graph = self.graph(project(body, extra="WINDOWS_MANIFEST=config.manifest\n"), "copper/app")
        args = lib.node_by_output(graph, "$(B)/copper/app/probe.cpp.o")["cmds"][0]["cmd_args"]
        self.assertIn("-DSET_WINS", args)
        self.assertIn("-DMACRO_WINS", args)
        self.assert_unused(graph)

    def test_java_collector_uses_the_same_scalar_facades(self):
        files = lib.java_tree(**{
            "copper/jar/ya.make": (
                "JAVA_CONTRIB()\nWINDOWS_LONG_PATH_MANIFEST()\n"
                'IF (WINDOWS_MANIFEST == "build/long-path.manifest")\n'
                "WINDOWS_MANIFEST($CHOICE)\nENDIF()\n"
                'IF (WINDOWS_MANIFEST == "selected.manifest")\nLOCAL_JAR(payload.jar)\nENDIF()\nEND()\n'
            ),
            "copper/jar/payload.jar": "fixture jar",
            "build/ymake_conf.py": lib.ymake_conf(extra="CHOICE=selected.manifest\n"),
        })
        graph = self.graph(files, "copper/jar")
        jars = [node for node in graph["graph"] if "$(S)/copper/jar/payload.jar" in node["inputs"]]
        self.assertEqual(len(jars), 1)
        self.assert_unused(graph)

    def test_later_recurse_conditions_see_the_assignments(self):
        files = project("")
        files["copper/scope/ya.make"] = (
            "UNION()\nEND()\nWINDOWS_LONG_PATH_MANIFEST()\n"
            'IF (WINDOWS_MANIFEST == "build/long-path.manifest")\n'
            "WINDOWS_MANIFEST(recursion.manifest)\nENDIF()\n"
            'IF (WINDOWS_MANIFEST == "recursion.manifest")\nRECURSE(../app)\nENDIF()\n'
        )
        graph = self.graph(files, "copper/scope")
        lib.node_by_output(graph, "$(B)/copper/app/app")
        self.assert_unused(graph)

    def test_arity_is_checked_after_macro_argument_expansion(self):
        for name, args, prefix, diagnostic in (
                ("WINDOWS_MANIFEST", "", "", "expects one argument"),
                ("WINDOWS_MANIFEST", "first second", "", "expects one argument"),
                ("WINDOWS_MANIFEST", "${CHOICES}", "SET(CHOICES first second)\n", "expects one argument"),
                ("WINDOWS_MANIFEST", "${EMPTY}", "SET(EMPTY)\n", "expects one argument"),
                ("WINDOWS_LONG_PATH_MANIFEST", "unexpected", "", "expects no arguments"),
                ("WINDOWS_LONG_PATH_MANIFEST", '""', "", "expects no arguments")):
            with self.subTest(name=name, args=args):
                result = lib.make_process(project(prefix + f"{name}({args})"), "copper/app")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(name + " " + diagnostic, result.stderr)
        graph = self.graph(project("WINDOWS_LONG_PATH_MANIFEST(${UNBOUND})\n"
                                   'IF (WINDOWS_MANIFEST == "build/long-path.manifest")\n'
                                   "CFLAGS(-DEMPTY_ARGUMENT_REMOVED)\nENDIF()"), "copper/app")
        self.assertIn("-DEMPTY_ARGUMENT_REMOVED", lib.node_by_output(graph, "$(B)/copper/app/probe.cpp.o")
                      ["cmds"][0]["cmd_args"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
