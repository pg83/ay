import unittest

import lib
from test_go_tests import args_after, go_module, go_test_world, test_node


BASE_RESOURCES = {"CLANG", "GO_TOOLS", "LLD_ROOT", "YMAKE_PYTHON3", "YOLINT"}


def bindings(names):
    return sorted(f"{name}_RESOURCE_GLOBAL::$(B)/resources/{name}" for name in names)


def fixture(project, label, module):
    files = go_test_world()
    resources = f"supplies/{label}"
    leaf, bridge = f"packages/{label}/leaf", f"packages/{label}/bridge"
    tool = f"tools/{label}_emitter"
    names = {part: f"{part}_{label.upper()}" for part in ("ALPHA", "ZETA", "TARGET", "HOST", "OUTSIDE")}
    files[f"{resources}/shared/ya.make"] = (
        "RESOURCES_LIBRARY()\n"
        f"DECLARE_EXTERNAL_RESOURCE({names['ZETA']} sbr:61)\n"
        "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(\n"
        f"{names['ALPHA']} sbr:71 FOR LINUX-X86_64 sbr:72 FOR LINUX-AARCH64)\n"
        "IF(ARCH_AARCH64)\n"
        f"DECLARE_EXTERNAL_RESOURCE({names['TARGET']} sbr:81)\n"
        "ELSE()\n"
        f"DECLARE_EXTERNAL_RESOURCE({names['HOST']} sbr:82)\n"
        "ENDIF()\nEND()\n"
    )
    files[f"{resources}/outside/ya.make"] = lib.resource_lib(names["OUTSIDE"], 91)
    go_module(files, leaf, macros=f"PEERDIR({resources}/shared)\n")
    go_module(files, bridge, body=f'import _ "a.yandex-team.ru/{leaf}"\n')
    lib.tool_program(files, tool, "emitter")
    files[f"{tool}/ya.make"] = files[f"{tool}/ya.make"].replace(
        "END()", f"PEERDIR({resources}/shared)\nEND()"
    )
    files[f"{project}/ya.make"] = (
        f"{module}()\nSET(VALIDATE_DATA yes)\nENV(SUITE_LABEL={label})\n"
        f"PEERDIR({bridge})\n"
        f"RUN_PROGRAM({tool} OUT generated.go)\n"
        "DATA(sbr://341)\nFROM_SANDBOX(FILE 342 OUT_NOAUTO unused.bin)\n"
        + ("GO_TEST_SRCS(payload_test.go)\nSIZE(MEDIUM)\n" if module == "GO_TEST" else "SRCS(payload.go)\n")
        + "END()\n"
    )
    files[f"{project}/payload_test.go" if module == "GO_TEST" else f"{project}/payload.go"] = "package main\n"
    return files, resources, leaf, bridge, tool, names


class GoValidationResourcesTest(unittest.TestCase):
    def test_validation_retains_the_owning_go_executable_resource_closure(self):
        for project, label in (("apps/meteor/checks", "rivet"), ("services/aurora/probes", "prism")):
            for module in ("GO_TEST", "GO_PROGRAM", "GO_DLL"):
                with self.subTest(project=project, module=module):
                    files, resources, leaf, bridge, tool, names = fixture(project, label, module)
                    graph = lib.make(files, project, resources + "/outside", "--target-platform-tests", "--dist")
                    expected = bindings(BASE_RESOURCES | {names[part] for part in ("ALPHA", "ZETA", "TARGET")})
                    context = lib.node_by_output(graph, "$(B)/common_test.context")
                    # The target DART consumes ALPHA; the unrelated root and
                    # the host tool only declare their private resource names.
                    fetch = lib.node_by_output(graph, "$(B)/resources/" + names["ALPHA"])
                    self.assertIn("sbr:71", fetch["cmds"][0]["cmd_args"])
                    for part in ("HOST", "OUTSIDE"):
                        self.assertFalse(any("$(B)/resources/" + names[part] in n["outputs"] for n in graph["graph"]))
                    generated = lib.node_by_output(graph, f"$(B)/{project}/generated.go")
                    host = lib.node_by_output(graph, f"$(B)/{tool}/emitter")
                    self.assertEqual(host["platform"], "default-linux-x86_64")
                    self.assertIn(host["uid"], generated["deps"])
                    consumer = next(n for n in graph["graph"] if generated["outputs"][0] in n["inputs"] and any(o.endswith(".vet.txt") for o in n["outputs"]))
                    self.assertEqual(consumer["platform"], "default-linux-aarch64")
                    self.assertIn(generated["uid"], consumer["deps"])
                    for peer in (leaf, bridge):
                        archive = lib.node_by_output(graph, f"$(B)/{peer}/{peer.rsplit('/', 1)[-1]}.a")
                        self.assertIn(archive["uid"], consumer["deps"])
                    bridge_node = lib.node_by_output(graph, f"$(B)/{bridge}/bridge.a")
                    leaf_node = lib.node_by_output(graph, f"$(B)/{leaf}/leaf.a")
                    self.assertIn(leaf_node["uid"], bridge_node["deps"])
                    for suite, resource_id in (("validate_data_sbr", "341"), ("validate_resource", "342")):
                        node = test_node(graph, project + "/" + suite)
                        args = node["cmds"][0]["cmd_args"]
                        self.assertEqual(args_after(args, "--global-resource"), expected)
                        start = args.index("--global-resource")
                        end = args.index("--node-timeout")
                        self.assertEqual(args[start:end], [arg for value in expected for arg in ("--global-resource", value)])
                        self.assertEqual(node["inputs"], ["$(S)/" + project, resource_id])
                        consumed = BASE_RESOURCES | {names[part] for part in ("ALPHA", "ZETA", "TARGET")}
                        expected_deps = [context["uid"], *(lib.node_by_output(graph, "$(B)/resources/" + name)["uid"] for name in consumed)]
                        self.assertCountEqual(node["deps"], expected_deps)
                        self.assertEqual(args_after(args, "--env"), ["SUITE_LABEL=" + label])
                        self.assertEqual(node["requirements"]["network"], "full")
                    # Existing suite metadata and resource bindings stay intact.
                    for suite in ("gofmt", "govet"):
                        args = test_node(graph, project + "/" + suite)["cmds"][0]["cmd_args"]
                        self.assertEqual(args_after(args, "--global-resource"), expected)
                    if module == "GO_TEST":
                        run = test_node(graph, project + "/" + project.rsplit("/", 1)[-1])
                        args = run["cmds"][0]["cmd_args"]
                        self.assertEqual(args_after(args, "--global-resource"), expected)
                        self.assertEqual(args_after(args, "--test-size"), ["medium"])

    def test_separate_modules_keep_separate_validation_bindings(self):
        files = go_test_world()
        for project, name in (("satellite/north", "NORTH_WIDGET"), ("satellite/south", "SOUTH_WIDGET")):
            files[f"supplies/{name}/ya.make"] = lib.resource_lib(name, 53)
            go_module(files, project, module="GO_PROGRAM", macros=f"SET(VALIDATE_DATA yes)\nPEERDIR(supplies/{name})\nDATA(sbr://403)\n")
        for targets in (("satellite/north", "satellite/south"), ("satellite/south", "satellite/north")):
            graph = lib.make(files, *targets, "--target-platform-tests")
            for project, name in (("satellite/north", "NORTH_WIDGET"), ("satellite/south", "SOUTH_WIDGET")):
                args = test_node(graph, project + "/validate_data_sbr")["cmds"][0]["cmd_args"]
                self.assertEqual(args_after(args, "--global-resource"), bindings(BASE_RESOURCES | {name}))


if __name__ == "__main__":
    unittest.main(verbosity=2)
