"""The sandbox task's binary, PY2, and PY3 are separate module contexts."""
import unittest

import lib
from test_python2 import python2_base
from test_python_srcs import python_base, kvs as py_kvs, keys


TASK = "harbor/jobs/CollectMint"
BUILD = "$(B)/" + TASK
SOURCE = "$(S)/" + TASK
VARIANTS = ("SB_TASK_BIN", "PY2", "PY3")
OUTPUTS = dict(zip(VARIANTS, ("mint", "libpymint.global.a", "libpy3mint.global.a")))


def fixture(selection="", name="mint"):
    files = python_base()
    files.update(python2_base())
    # _PY2_PROGRAM, unlike public PY2_PROGRAM, has no deprecation peer.
    files.pop("build/rules/py2_deprecation/ya.make")
    files.pop("build/rules/py2_deprecation/s.cpp")
    lib.tool_program(files, "tools/engrave", "engraver")
    files["shared/task.inc"] = """
IF(PYTHON2)
    SET(EDITION legacy)
ELSE()
    IF(PYTHON3)
        SET(EDITION current)
    ELSE()
        SET(EDITION uninitialized)
    ENDIF()
ENDIF()
SET(STAMP ${MODULE_TAG}_${EDITION})
IF(NO_CHECK_IMPORTS_FOR_VALUE == "$NO_CHECK_IMPORTS_FOR_VALUE api.* kernel.* library.* sky*")
    PY_SRCS(defaults_seen.py)
ENDIF()
PY_SRCS(${MODULE_TAG}.py common.py)
RESOURCE(${MODULE_TAG}.dat ${STAMP})
RUN_PROGRAM(tools/engrave ${MODULE_KIND} ${MODULE_TAG} ${STAMP} ${_PYTHON_VER} ${PYTHON_TYPE_FOR_CYTHON}
    IN seed.txt OUT_NOAUTO ${MODULE_TAG}.dat)
SRCS(native.cpp)
"""
    files[f"{TASK}/ya.make"] = (
        f"SANDBOX_PY23_TASK({name})\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
        f"{selection}\nINCLUDE(${{ARCADIA_ROOT}}/shared/task.inc)\nEND()\n"
    )
    files[f"{TASK}/seed.txt"] = "seed\n"
    files[f"{TASK}/native.cpp"] = "int native;\n"
    files[f"{TASK}/defaults_seen.py"] = ""
    files[f"{TASK}/common.py"] = ""
    for variant in VARIANTS:
        files[f"{TASK}/{variant}.py"] = ""
    for directory in ("sandbox/sdk2", "sandbox/bin", "sandbox/taskbox/worker"):
        files[f"{directory}/ya.make"] = f"PY23_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nPY_SRCS(side.py)\nEND()\n"
        files[f"{directory}/side.py"] = ""
    return files


def task_output(graph, variant):
    return lib.node_by_output(graph, BUILD + "/" + OUTPUTS[variant])


def resources(graph, output):
    by_uid = {node["uid"]: node for node in graph["graph"]}
    return [by_uid[uid] for uid in output["deps"] if "/objcopy_" in by_uid[uid]["outputs"][0]]


def resource_keys(node):
    args = node["cmds"][0]["cmd_args"]
    return keys(node) if "--keys" in args else []


def kvs(node):
    return py_kvs(node) if "--kvs" in node["cmds"][0]["cmd_args"] else []


class SandboxPy23TaskTest(unittest.TestCase):
    def assert_context(self, graph, variant):
        output = task_output(graph, variant)
        edition = "current" if variant == "PY3" else "legacy"
        tag = variant
        version = "py3" if variant == "PY3" else "py2"
        generated = lib.node_by_output(graph, f"{BUILD}/{variant}.dat")
        self.assertEqual(generated["cmds"][0]["cmd_args"], [
            "$(B)/tools/engrave/engraver", "SANDBOX_PY23_TASK", tag, f"{variant}_{edition}", version, version.upper(),
        ])
        self.assertIn(SOURCE + "/seed.txt", generated["inputs"])
        tool = lib.node_by_output(graph, "$(B)/tools/engrave/engraver")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(tool["outputs"][0], generated["inputs"])
        nodes = resources(graph, output)
        payload = next(node for node in nodes if f"{variant}_{edition}" in resource_keys(node))
        self.assertIn(generated["uid"], payload["deps"])
        self.assertIn(generated["outputs"][0], payload["inputs"])
        for node in nodes:
            self.assertIn("$(B)/tools/rescompiler/rescompiler", node["inputs"])
            self.assertIn(lib.node_by_output(graph, "$(B)/tools/rescompiler/rescompiler")["uid"], node["deps"])
        bytecode = lib.node_by_output(graph, BUILD + "/common.py.yapyc" + ("3" if variant == "PY3" else ""))
        common = next(node for node in nodes if bytecode["outputs"][0] in node["inputs"])
        self.assertIn(bytecode["uid"], common["deps"])
        self.assertIn(SOURCE + "/common.py", bytecode["inputs"])
        compiler = lib.node_by_output(graph, "$(B)/tools/" + ("py3cc/py3cc" if variant == "PY3" else "py2cc/py2cc"))
        self.assertIn(compiler["uid"], bytecode["deps"])
        registry = "SANDBOX_TASK_REGISTRY.harbor.jobs.CollectMint=harbor.jobs.CollectMint"
        self.assertTrue(any(registry in kvs(node) for node in nodes))
        python = next(node for node in nodes if any(
            key.endswith(f"/{variant}.py") or key.endswith("." + variant)
            for key in resource_keys(node)))
        if variant != "SB_TASK_BIN":
            command = output["cmds"][0]["cmd_args"]
            self.assertLess(command.index(python["outputs"][0]), command.index(payload["outputs"][0]))
        masks = [value for node in nodes for value in kvs(node) if value.startswith("py/no_check_imports/")]
        mains = [value for node in nodes for value in kvs(node) if value.startswith("PY_MAIN=")]
        if variant == "SB_TASK_BIN":
            self.assertEqual(mains, ["PY_MAIN=sandbox.taskbox.binary:main"])
            self.assertEqual([value.split("=", 1)[1] for value in masks], ["Noneapi.*kernel.*library.*sky*api.*kernel.*library.*sky* api.* kernel.* library.* sky*"])
        else:
            self.assertEqual(mains, [])
            self.assertEqual(masks, [])
        return output

    def test_root_keeps_all_three_contexts_and_real_producer_edges(self):
        graph = lib.make(fixture(), TASK, "--strip-packages-from-results")
        for variant in VARIANTS:
            output = self.assert_context(graph, variant)
            self.assertIn(output["uid"], graph["result"])
            compiled = next(node for node in graph["graph"] if node["outputs"][0].startswith(
                BUILD + "/native.cpp" + {"SB_TASK_BIN": ".o", "PY2": ".py2.", "PY3": ".py3."}[variant]))
            native_consumer = output if variant == "SB_TASK_BIN" else lib.node_by_output(
                graph, BUILD + "/" + OUTPUTS[variant].replace(".global.a", ".a"))
            self.assertIn(compiled["uid"], native_consumer["deps"])
        binary = task_output(graph, "SB_TASK_BIN")
        defaults = lib.node_by_output(graph, BUILD + "/defaults_seen.py.yapyc")
        self.assertIn(defaults["outputs"][0], next(node for node in resources(graph, binary)
                          if any("defaults_seen" in key for key in resource_keys(node)))["inputs"])
        for directory in ("sandbox/sdk2", "sandbox/bin", "sandbox/taskbox/worker"):
            archive = lib.node_by_output(graph, "$(B)/" + directory + "/libpy" + directory.replace("/", "-") + ".global.a")
            self.assertIn(archive["uid"], binary["deps"])

    def test_python_peers_select_only_the_corresponding_library(self):
        for version, program, variant in ((2, "PY2_PROGRAM", "PY2"), (3, "PY3_PROGRAM_BIN", "PY3")):
            with self.subTest(version=version):
                files = fixture()
                files["runner/ya.make"] = f"{program}(runner)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nPEERDIR({TASK})\nEND()\n"
                # Public PY2_PROGRAM's own deprecation rule is outside the task.
                graph = lib.make(files, "runner", opensource=True)
                runner = lib.node_by_output(graph, "$(B)/runner/runner")
                library = self.assert_context(graph, variant)
                self.assertIn(library["uid"], runner["deps"])
                sdk = lib.node_by_output(graph, "$(B)/sandbox/sdk2/libpy" + ("3" if version == 3 else "") + "sandbox-sdk2.global.a")
                self.assertIn(sdk["uid"], runner["deps"])
                outputs = {out for node in graph["graph"] for out in node["outputs"]}
                for other in set(VARIANTS) - {variant}:
                    self.assertNotIn(BUILD + "/" + OUTPUTS[other], outputs)
                self.assertFalse(any(out.startswith(("$(B)/sandbox/bin/", "$(B)/sandbox/taskbox/worker/")) for out in outputs))

    def test_root_selectors_use_configuration_names(self):
        for selection, variants in (("ONLY_TAGS(SB_TASK_BIN)", {"SB_TASK_BIN"}),
                                    ("ONLY_TAGS(PY2)", {"PY2"}),
                                    ("ONLY_TAGS(PY3)", {"PY3"}),
                                    ("EXCLUDE_TAGS(SB_TASK_BIN)", {"PY2", "PY3"}),
                                    ("ONLY_TAGS(PY2)\nINCLUDE_TAGS(PY3)", {"PY2", "PY3"})):
            with self.subTest(selection=selection):
                graph = lib.make(fixture(selection), TASK, "--strip-packages-from-results")
                outputs = {out for node in graph["graph"] for out in node["outputs"]}
                for variant in VARIANTS:
                    self.assertEqual(BUILD + "/" + OUTPUTS[variant] in outputs, variant in variants)
                    if variant in variants:
                        self.assertIn(task_output(graph, variant)["uid"], graph["result"])

    def test_depends_keeps_selected_siblings_and_only_binary_runtime_artifact(self):
        for selection, variants in (("", set(VARIANTS)), ("ONLY_TAGS(SB_TASK_BIN)", {"SB_TASK_BIN"}),
                                    ("ONLY_TAGS(PY2)", {"PY2"}), ("ONLY_TAGS(PY3)", {"PY3"})):
            with self.subTest(selection=selection):
                files = fixture(selection)
                files["checks/ya.make"] = f"EXECTEST()\nRUN(true)\nDEPENDS({TASK})\nEND()\n"
                graph = lib.make(files, "checks", "-t", "--strip-packages-from-results")
                suite = lib.node_by_output(graph, "$(B)/checks/test-results/exectest/meta.json")
                outputs = {out for node in graph["graph"] for out in node["outputs"]}
                for variant in VARIANTS:
                    self.assertEqual(BUILD + "/" + OUTPUTS[variant] in outputs, variant in variants)
                    if variant in variants:
                        output = task_output(graph, variant)
                        self.assertIn(output["uid"], graph["result"])
                        self.assertEqual(output["uid"] in suite["deps"], variant == "SB_TASK_BIN")

    def test_bundle_selects_unique_final_binary(self):
        files = fixture()
        files["package/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}BUNDLE({TASK} NAME worker.bin)\nRESOURCE(worker.bin worker)\nEND()\n"
        graph = lib.make(files, "package")
        binary = task_output(graph, "SB_TASK_BIN")
        bundle = lib.node_by_output(graph, "$(B)/package/worker.bin")
        self.assertEqual(bundle["cmds"][0]["cmd_args"][-2:], [BUILD + "/mint", "$(B)/package/worker.bin"])
        self.assertIn(binary["uid"], bundle["deps"])
        self.assertIn(BUILD + "/mint", bundle["inputs"])

    def test_first_library_context_ignores_later_binary_selectors_for_root_and_depends(self):
        selection = 'IF(MODULE_TAG == "SB_TASK_BIN")\nONLY_TAGS(PY3)\nENDIF()'
        for depends in (False, True):
            with self.subTest(depends=depends):
                files = fixture(selection)
                files["checks/ya.make"] = f"EXECTEST()\nRUN(true)\nDEPENDS({TASK})\nEND()\n"
                graph = lib.make(files, "checks" if depends else TASK, "-t")
                outputs = {out for node in graph["graph"] for out in node["outputs"]}
                self.assertIn(BUILD + "/" + OUTPUTS["PY3"], outputs)
                self.assertIn(BUILD + "/" + OUTPUTS["PY2"], outputs)
                self.assertIn(BUILD + "/mint", outputs)

    def test_platform_excluded_submodule_is_not_a_root_or_depends_result(self):
        for depends in (False, True):
            with self.subTest(depends=depends):
                files = fixture()
                files["checks/ya.make"] = f"EXECTEST()\nRUN(true)\nDEPENDS({TASK})\nEND()\n"
                graph = lib.make(files, "checks" if depends else TASK, "-t", "-DEXCLUDE_SUBMODULES=PY2")
                outputs = {out for node in graph["graph"] for out in node["outputs"]}
                self.assertIn(BUILD + "/mint", outputs)
                self.assertIn(BUILD + "/" + OUTPUTS["PY3"], outputs)
                self.assertNotIn(BUILD + "/" + OUTPUTS["PY2"], outputs)

    def test_exclude_submodules_reads_configuration_flags_instead_of_body_set(self):
        graph = lib.make(fixture("SET(EXCLUDE_SUBMODULES PY3)"), TASK)
        for variant in VARIANTS:
            task_output(graph, variant)

    def test_context_python_variables_override_inherited_configuration(self):
        graph = lib.make(fixture(), TASK, "-DPYTHON2=yes", "-DPYTHON3=no",
                         "-D_PYTHON_VER=foreign", "-DPYTHON_TYPE_FOR_CYTHON=foreign")
        for variant in VARIANTS:
            self.assert_context(graph, variant)

    def test_recursion_uses_basename_and_inherited_library_prefixes(self):
        files = fixture(name="")
        files["harbor/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\nRECURSE(jobs/CollectMint)\n"
        graph = lib.make(files, "harbor", "--strip-packages-from-results")
        for output in ("CollectMint", "libpyharbor-jobs-CollectMint.global.a", "libpy3harbor-jobs-CollectMint.global.a"):
            self.assertIn(lib.node_by_output(graph, BUILD + "/" + output)["uid"], graph["result"])

    def test_binary_defaults_obey_flags_without_public_py2_deprecation(self):
        for flags, prefix in ((["-DNO_CHECK_IMPORTS_FOR_VALUE=custom.*"], "custom.*"),
                              (["-DSANITIZER_TYPE=address"], "None"),
                              (["-DNO_CHECK_IMPORTS_FOR_VALUE="], ""),
                              (["-DFAIL_PY2=yes"], "None")):
            with self.subTest(flags=flags):
                graph = lib.make(fixture("ONLY_TAGS(SB_TASK_BIN)"), TASK, *flags, opensource=False)
                binary = task_output(graph, "SB_TASK_BIN")
                masks = [value.split("=", 1)[1] for node in resources(graph, binary) for value in kvs(node)
                         if value.startswith("py/no_check_imports/")]
                self.assertEqual(masks, [prefix + "api.*kernel.*library.*sky*api.*kernel.*library.*sky* api.* kernel.* library.* sky*"])

    def test_public_python2_program_keeps_its_deprecation_dependency(self):
        files = python2_base()
        files["ordinary/ya.make"] = (
            f"PY2_PROGRAM()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
            "NO_CHECK_IMPORTS()\nPY_SRCS(main.py)\nEND()\n"
        )
        files["ordinary/main.py"] = ""
        graph = lib.make(files, "ordinary", opensource=False)
        deprecation = lib.node_by_output(
            graph, "$(B)/build/rules/py2_deprecation/libbuild-rules-py2_deprecation.a")
        binary = lib.node_by_output(graph, "$(B)/ordinary/ordinary")
        self.assertIn(deprecation["outputs"][0], binary["inputs"])
        self.assertIn(deprecation["uid"], binary["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
