import unittest

import lib
from test_yql import BARE, udf_fixture


COMPILERS = ("CLANG16", "CLANG18", "CLANG20", "CLANG22", "CLANG")


def world():
    files = udf_fixture()
    files.update({
        "build/ya.conf.json": lib.ya_conf_json("20"),
        "plugins/nova/ya.make": (
            "YQL_UDF_CONTRIB(astral)\nSET(ASTRAL_RESOURCE_DEFAULT 821)\n"
            "STYLE_CPP()\nSET(VALIDATE_DATA yes)\n"
            "FROM_SANDBOX(FILE 822 OUT_NOAUTO ${MODULE_TAG}.payload)\n"
            "SRCS(aggregate.cpp)\nPEERDIR(variants/${MODULE_TAG})\nEND()\n"
        ),
        "plugins/nova/aggregate.cpp": "int aggregate;\n",
        "build/cow/on/ya.make": f"LIBRARY()\n{BARE}SRCS(cow.c)\nEND()\n",
        "build/cow/on/cow.c": "int cow;\n",
        "contrib/libs/libeatmydata/ya.make": f"LIBRARY()\n{BARE}SRCS(disable_sync.c)\nEND()\n",
        "contrib/libs/libeatmydata/disable_sync.c": "int disable_sync;\n",
        "contrib/libs/linux-headers/ya.make": (
            "LIBRARY()\nDISABLE(NEED_PLATFORM_PEERDIRS)\nNO_PLATFORM()\n"
            "ADDINCL(GLOBAL contrib/libs/linux-headers)\nEND()\n"
        ),
        "build/platform/clang/ya.make": (
            "RESOURCES_LIBRARY()\n"
            + "".join(f"DECLARE_EXTERNAL_RESOURCE({name} sbr:{900 + i})\n"
                      for i, name in enumerate(COMPILERS))
            + "END()\n"
        ),
        "build/platform/lld/ya.make": lib.resource_lib("LLD_ROOT", 910),
        "build/platform/linux_sdk/ya.make": lib.resource_lib("OS_SDK_ROOT", 911),
        "build/platform/clang/clang-format/ya.make": lib.resource_lib("CLANG_FORMAT", 912),
        "build/platform/python/ymake_python3/ya.make": lib.resource_lib("YMAKE_PYTHON3", 913),
        "build/scripts/fs_tools.py": "import process_command_files\n",
        "build/scripts/process_command_files.py": "",
        "control/ya.make": (
            f"PROGRAM()\n{BARE}SET(VALIDATE_DATA yes)\n"
            "FROM_SANDBOX(FILE 823 OUT_NOAUTO unused.payload)\nSRCS(main.cpp)\nEND()\n"
        ),
        "control/main.cpp": "int main() { return 0; }\n",
    })
    for tag in ("YQL_UDF_SHARED", "YQL_UDF_STATIC", "YQL_UDF_STATIC_CURRENT"):
        files[f"variants/{tag}/ya.make"] = lib.resource_lib("PRIVATE_" + tag, 914)
    for flavor, abi in (("stable", ""), ("current", "YQL_CURRENT_ABI_VERSION()\n")):
        files[f"drivers/{flavor}/ya.make"] = f"PROGRAM()\n{BARE}{abi}PEERDIR(plugins/nova)\nEND()\n"
    return files


def resource_bindings(node):
    args = node["cmds"][0]["cmd_args"]
    return [args[i + 1] for i, arg in enumerate(args) if arg == "--global-resource"]


def bindings(*names):
    return sorted(f"{name}_RESOURCE_GLOBAL::$(B)/resources/{name}" for name in names)


class YqlResourceScopeTest(unittest.TestCase):
    def graph(self, files, *targets):
        return lib.make(files, targets[-1], *targets[:-1], "--target-platform-tests", "-DADD_PEERDIRS_GEN_TESTS=yes",
                        "-DAUTOCHECK=yes", opensource=False)

    def test_prebuilt_validation_owns_only_its_configured_resources(self):
        for targets in (("plugins/nova", "control"), ("control", "plugins/nova")):
            with self.subTest(targets=targets):
                graph = self.graph(world(), *targets)
                validation = lib.node_by_output(graph, "$(B)/plugins/nova/test-results/validate_resource/meta.json")
                self.assertEqual(validation["inputs"], ["$(S)/plugins/nova", "821", "822"])
                self.assertEqual(resource_bindings(validation), bindings("CLANG_FORMAT", "YMAKE_PYTHON3"))
                control = lib.node_by_output(graph, "$(B)/control/test-results/validate_resource/meta.json")
                self.assertEqual(resource_bindings(control), bindings(*COMPILERS, "LLD_ROOT", "OS_SDK_ROOT", "YMAKE_PYTHON3"))
                compiled = lib.node_by_output(graph, "$(B)/control/main.cpp.o")
                self.assertIn("$(B)/resources/CLANG20/bin/clang++", compiled["cmds"][0]["cmd_args"])
                library = lib.node_by_output(graph, "$(B)/plugins/nova/libastral.so")
                python = lib.node_by_output(graph, "$(B)/resources/YMAKE_PYTHON3")
                self.assertIn(python["uid"], library["deps"])
                self.assertFalse(any("aggregate.cpp.pic.o" in o for n in graph["graph"] for o in n["outputs"]))

    def test_prebuilt_program_uses_the_same_bare_link_scope(self):
        files = world()
        files["plugins/nova/ya.make"] = files["plugins/nova/ya.make"].replace(
            "YQL_UDF_CONTRIB(astral)", "YQL_PREBUILT_PROGRAM(astral)",
        ).replace("STYLE_CPP()\n", "")
        graph = self.graph(files, "plugins/nova", "control")
        validation = lib.node_by_output(graph, "$(B)/plugins/nova/test-results/validate_resource/meta.json")
        self.assertEqual(validation["inputs"], ["$(S)/plugins/nova", "821", "822"])
        self.assertEqual(resource_bindings(validation), bindings("YMAKE_PYTHON3"))
        program = lib.node_by_output(graph, "$(B)/plugins/nova/astral")
        fetch = lib.node_by_output(graph, "$(B)/plugins/nova/astral.prebuilt")
        python = lib.node_by_output(graph, "$(B)/resources/YMAKE_PYTHON3")
        self.assertIn(fetch["uid"], program["deps"])
        self.assertIn(python["uid"], program["deps"])
        self.assertEqual(program["cmds"][0]["cmd_args"][-3:],
                         ["copy", "$(B)/plugins/nova/astral.prebuilt", "$(B)/plugins/nova/astral"])

    def test_static_contexts_retain_their_compiler_and_private_resources(self):
        for flavor, tag, suffix in (("stable", "YQL_UDF_STATIC", "udfs"),
                                    ("current", "YQL_UDF_STATIC_CURRENT", "udfs.abi_current")):
            with self.subTest(flavor=flavor):
                graph = self.graph(world(), "drivers/" + flavor)
                validation = lib.node_by_output(graph, "$(B)/plugins/nova/test-results/validate_resource/meta.json")
                self.assertEqual(validation["inputs"], ["$(S)/plugins/nova", "822"])
                self.assertEqual(resource_bindings(validation),
                                 bindings(*COMPILERS, "CLANG_FORMAT", "LLD_ROOT", "OS_SDK_ROOT", "YMAKE_PYTHON3", "PRIVATE_" + tag))
                compiled = lib.node_by_output(graph, f"$(B)/plugins/nova/aggregate.cpp.{suffix}.o")
                args = compiled["cmds"][0]["cmd_args"]
                self.assertIn("$(B)/resources/CLANG20/bin/clang++", args)
                self.assertEqual("-DUSE_CURRENT_UDF_ABI_VERSION" in args, flavor == "current")

    def test_validation_dart_keeps_first_context_resources_when_merging_ids(self):
        for targets, expected in (
            (("plugins/nova", "drivers/current", "drivers/stable"), ("CLANG_FORMAT", "YMAKE_PYTHON3")),
            (("drivers/current", "plugins/nova", "drivers/stable"),
             (*COMPILERS, "CLANG_FORMAT", "LLD_ROOT", "OS_SDK_ROOT", "YMAKE_PYTHON3", "PRIVATE_YQL_UDF_STATIC_CURRENT")),
        ):
            with self.subTest(targets=targets):
                graph = self.graph(world(), *targets)
                validations = [n for n in graph["graph"]
                               if "$(B)/plugins/nova/test-results/validate_resource/meta.json" in n["outputs"]]
                self.assertEqual(len(validations), 1)
                self.assertEqual(validations[0]["inputs"], ["$(S)/plugins/nova", "821", "822"])
                self.assertEqual(resource_bindings(validations[0]), bindings(*expected))

    def test_disabled_platform_defaults_preserve_explicit_compiler_resource_peers(self):
        files = world()
        files["plugins/nova/ya.make"] = (
            "LIBRARY()\nNO_PLATFORM()\nDISABLE(NEED_PLATFORM_PEERDIRS COMMON_LINK_SETTINGS)\n"
            "PEERDIR(build/platform/clang build/platform/linux_sdk)\n"
            "FROM_SANDBOX(FILE 822 OUT_NOAUTO unused.payload)\nSRCS(aggregate.cpp)\nEND()\n"
        )
        graph = self.graph(files, "plugins/nova")
        validation = lib.node_by_output(graph, "$(B)/plugins/nova/test-results/validate_resource/meta.json")
        self.assertEqual(resource_bindings(validation), bindings(*COMPILERS, "OS_SDK_ROOT", "YMAKE_PYTHON3"))
        compiled = lib.node_by_output(graph, "$(B)/plugins/nova/aggregate.cpp.o")
        args = compiled["cmds"][0]["cmd_args"]
        self.assertIn("$(B)/resources/CLANG20/bin/clang++", args)
        self.assertNotIn("-I$(S)/contrib/libs/linux-headers", args)


if __name__ == "__main__":
    unittest.main(verbosity=2)
