"""Python3 binary and PY2/PY3 library contexts of SANDBOX_PY23_3_TASK."""
import unittest

from tst import lib
from tst.make.test_sandbox_py23_task import fixture as original_fixture, TASK, BUILD, SOURCE, resources, kvs, resource_keys


VARIANTS = ("SB_TASK_BIN", "PY2", "PY3")
OUTPUTS = {"SB_TASK_BIN": "mint", "PY2": "libpymint.global.a", "PY3": "libpy3mint.global.a"}


def fixture(selection="", name="mint"):
    files = original_fixture(selection, name)
    files[TASK + "/ya.make"] = files[TASK + "/ya.make"].replace("SANDBOX_PY23_TASK", "SANDBOX_PY23_3_TASK")
    return files


class SandboxPy233TaskTest(unittest.TestCase):
    def check_context(self, graph, variant):
        version = "py2" if variant == "PY2" else "py3"
        edition = "legacy" if variant == "PY2" else "current"
        output = lib.node_by_output(graph, BUILD + "/" + OUTPUTS[variant])
        action = lib.node_by_output(graph, BUILD + "/" + variant + ".dat")
        self.assertEqual(action["cmds"][0]["cmd_args"], [
            "$(B)/tools/engrave/engraver", "SANDBOX_PY23_3_TASK", variant,
            variant + "_" + edition, version, version.upper(),
        ])
        self.assertIn(SOURCE + "/seed.txt", action["inputs"])
        tool = lib.node_by_output(graph, "$(B)/tools/engrave/engraver")
        self.assertIn(tool["uid"], action["deps"])
        self.assertIn(tool["outputs"][0], action["inputs"])
        payloads = resources(graph, output)
        generated = next(n for n in payloads if variant + "_" + edition in resource_keys(n))
        self.assertIn(action["uid"], generated["deps"])
        self.assertIn(action["outputs"][0], generated["inputs"])
        bytecode = lib.node_by_output(graph, BUILD + "/common.py.yapyc" + ("" if variant == "PY2" else "3"))
        source = next(n for n in payloads if bytecode["outputs"][0] in n["inputs"])
        self.assertIn(bytecode["uid"], source["deps"])
        self.assertIn(SOURCE + "/common.py", bytecode["inputs"])
        registry = "SANDBOX_TASK_REGISTRY.harbor.jobs.CollectMint=harbor.jobs.CollectMint"
        self.assertTrue(any(registry in kvs(n) for n in payloads))
        mains = [v for n in payloads for v in kvs(n) if v.startswith("PY_MAIN=")]
        self.assertEqual(mains, ["PY_MAIN=sandbox.taskbox.binary:main"] if variant == "SB_TASK_BIN" else [])
        ids = {n["uid"] for n in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))
        return output

    def test_all_three_contexts_keep_real_producers_and_distinct_objects(self):
        graph = lib.make(fixture(), TASK, "--strip-packages-from-results")
        for variant in VARIANTS:
            output = self.check_context(graph, variant)
            self.assertIn(output["uid"], graph["result"])
            suffix = {"SB_TASK_BIN": ".o", "PY2": ".py2.o", "PY3": ".py3.o"}[variant]
            compiled = lib.node_by_output(graph, BUILD + "/native.cpp" + suffix)
            owner = output if variant == "SB_TASK_BIN" else lib.node_by_output(
                graph, BUILD + "/" + OUTPUTS[variant].replace(".global.a", ".a"))
            self.assertIn(compiled["uid"], owner["deps"])
        self.assertEqual(sum(BUILD + "/common.py.yapyc3" in n["outputs"] for n in graph["graph"]), 1)
        binary = lib.node_by_output(graph, BUILD + "/mint")
        for peer in ("sandbox/bin", "sandbox/sdk2", "sandbox/taskbox/worker"):
            archive = lib.node_by_output(graph, "$(B)/" + peer + "/libpy3" + peer.replace("/", "-") + ".global.a")
            self.assertIn(archive["uid"], binary["deps"])

    def test_selectors_use_configuration_tags_and_binary_is_python3(self):
        for selection, expected in (("ONLY_TAGS(SB_TASK_BIN)", {"SB_TASK_BIN"}),
                                     ("ONLY_TAGS(PY2)", {"PY2"}), ("ONLY_TAGS(PY3)", {"PY3"}),
                                     ("EXCLUDE_TAGS(SB_TASK_BIN)", {"PY2", "PY3"}),
                                     ("ONLY_TAGS(PY2)\nINCLUDE_TAGS(PY3)", {"PY2", "PY3"})):
            with self.subTest(selection=selection):
                graph = lib.make(fixture(selection), TASK, "--strip-packages-from-results", "-DFAIL_PY2=yes")
                outputs = {o for n in graph["graph"] for o in n["outputs"]}
                for variant in VARIANTS:
                    self.assertEqual(BUILD + "/" + OUTPUTS[variant] in outputs, variant in expected)
                    if variant in expected:
                        self.check_context(graph, variant)
        graph = lib.make(fixture("ONLY_TAGS(SB_TASK_BIN)"), TASK, "-DOPENSOURCE=no")
        self.check_context(graph, "SB_TASK_BIN")
        self.assertFalse(any(o.startswith("$(B)/build/rules/py2_deprecation/") for n in graph["graph"] for o in n["outputs"]))

    def test_python_peers_select_library_without_binary_body(self):
        for module, variant in (("PY2_PROGRAM", "PY2"), ("PY3_PROGRAM_BIN", "PY3")):
            with self.subTest(module=module):
                files = fixture()
                files["runner/ya.make"] = module + "(runner)\n" + lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\nNO_CHECK_IMPORTS()\nPEERDIR(" + TASK + ")\nEND()\n"
                graph = lib.make(files, "runner")
                library = self.check_context(graph, variant)
                runner = lib.node_by_output(graph, "$(B)/runner/runner")
                self.assertIn(library["uid"], runner["deps"])
                self.assertFalse(any(BUILD + "/mint" in n["outputs"] for n in graph["graph"]))

    def test_cpp_peer_default_and_explicit_membership_select_first_eligible_library(self):
        for tags, selected, selection in ((None, "PY3", ""), ("PY2", "PY2", ""),
                                           ("PY3", "PY3", ""), ("PY3 PY2", "PY2", ""),
                                           ("PY2 PY3", "PY3", "ONLY_TAGS(PY3)")):
            with self.subTest(tags=tags, selection=selection):
                files = fixture(selection)
                files["cpp/ya.make"] = "PROGRAM(cpp)\n" + lib.BARE_MODULE + "ALLOCATOR(FAKE)\nSRCS(main.cpp)\n" + ("SET(PEERDIR_TAGS " + tags + ")\n" if tags is not None else "") + "PEERDIR(" + TASK + ")\nEND()\n"
                files["cpp/main.cpp"] = "int main(){return 0;}\n"
                graph = lib.make(files, "cpp")
                library = self.check_context(graph, selected)
                self.assertIn(library["uid"], lib.node_by_output(graph, "$(B)/cpp/cpp")["deps"])
        files = fixture()
        files["cpp/ya.make"] = "PROGRAM(cpp)\n" + lib.BARE_MODULE + "ALLOCATOR(FAKE)\nSET(PEERDIR_TAGS)\nPEERDIR(" + TASK + ")\nEND()\n"
        result = lib.make_process(files, "cpp")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("incompatible tags", result.stderr)

    def test_depends_uses_python3_binary_and_obeys_exclusion(self):
        for selection, binary in (("", True), ("ONLY_TAGS(PY3)", False), ("ONLY_TAGS(SB_TASK_BIN)", True)):
            with self.subTest(selection=selection):
                files = fixture(selection)
                files["checks/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(" + TASK + ")\nEND()\n"
                graph = lib.make(files, "checks", "-t", "--strip-packages-from-results")
                suite = lib.node_by_output(graph, "$(B)/checks/test-results/exectest/meta.json")
                selected = [n for n in graph["graph"] if BUILD + "/mint" in n["outputs"]]
                self.assertEqual(len(selected), int(binary))
                if binary:
                    self.assertIn(selected[0]["uid"], suite["deps"])
                    self.assertIn(selected[0]["uid"], graph["result"])

    def test_runtime_conditionals_use_native_first_py2_configuration(self):
        for condition, tag, binary in (("SB_TASK_BIN", "PY3", True), ("PY2", "SB_TASK_BIN", True)):
            with self.subTest(condition=condition):
                selection = 'IF(MODULE_TAG == "' + condition + '")\nONLY_TAGS(' + tag + ')\nENDIF()'
                files = fixture(selection)
                files["checks/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(" + TASK + ")\nEND()\n"
                graph = lib.make(files, "checks", "-t", "--strip-packages-from-results")
                suite = lib.node_by_output(graph, "$(B)/checks/test-results/exectest/meta.json")
                output = self.check_context(graph, "SB_TASK_BIN")
                self.assertIn(output["uid"], suite["deps"])
                if condition == "SB_TASK_BIN":
                    direct = lib.make(files, TASK, "--strip-packages-from-results")
                    for variant in VARIANTS:
                        self.check_context(direct, variant)
                if condition == "PY2":
                    self.assertFalse(any(BUILD + "/libpymint.a" in n["outputs"] or
                                         BUILD + "/libpy3mint.a" in n["outputs"] for n in graph["graph"]))

    def test_union_and_ci_defaults_allow_python3_library(self):
        for module in ("UNION", "CI_GROUP"):
            with self.subTest(module=module):
                files = fixture()
                files["collection/ya.make"] = module + "(collection)\nPEERDIR(" + TASK + ")\nEND()\n"
                files["checks/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(collection)\nEND()\n"
                graph = lib.make(files, "checks", "-t", "--strip-packages-from-results")
                if module == "CI_GROUP":
                    self.check_context(graph, "PY3")
                else:
                    archive = lib.node_by_output(graph, BUILD + "/libpy3mint.a")
                    compiled = lib.node_by_output(graph, BUILD + "/native.cpp.py3.o")
                    self.assertIn(compiled["uid"], archive["deps"])
                self.assertFalse(any(BUILD + "/mint" in n["outputs"] or BUILD + "/libpymint.a" in n["outputs"]
                                     for n in graph["graph"]))

    def test_explicit_py3_prefix_replaces_inherited_value_and_allows_source_override(self):
        for flags, prefix in (((), "libpy3"), (("-DCYGWIN=yes",), "py3")):
            for override in (False, True):
                with self.subTest(flags=flags, override=override):
                    files = fixture("ONLY_TAGS(PY3)")
                    body = files[TASK + "/ya.make"]
                    files["shared/task.inc"] = files["shared/task.inc"].replace(
                        "${PYTHON_TYPE_FOR_CYTHON}", "${PYTHON_TYPE_FOR_CYTHON} ${MODULE_PREFIX}")
                    if override:
                        body = body.replace("ONLY_TAGS(PY3)", "ONLY_TAGS(PY3)\nSET(MODULE_PREFIX custom)")
                    files[TASK + "/ya.make"] = "SET(MODULE_PREFIX from_before)\n" + body
                    graph = lib.make(files, TASK, *flags, "--strip-packages-from-results")
                    expected = "custom" if override else prefix
                    action = lib.node_by_output(graph, BUILD + "/PY3.dat")
                    self.assertEqual(action["cmds"][0]["cmd_args"][-1], expected)
                    lib.node_by_output(graph, BUILD + "/" + prefix + "mint.a")
                    lib.node_by_output(graph, BUILD + "/" + prefix + "mint.global.a")

    def test_shared_lexical_header_keeps_each_name_and_variant_context(self):
        files = fixture()
        body = files[TASK + "/ya.make"].replace("SANDBOX_PY23_3_TASK(mint)", "SANDBOX_PY23_3_TASK(${TASK_NAME})")
        files["shared/module.inc"] = body
        other = "harbor/jobs/CollectRose"
        for path, name in ((TASK, "mint"), (other, "rose")):
            files[path + "/ya.make"] = "SET(TASK_NAME " + name + ")\nINCLUDE(${ARCADIA_ROOT}/shared/module.inc)\n"
        for path, content in list(files.items()):
            if path.startswith(TASK + "/") and not path.endswith("/ya.make"):
                files[other + path[len(TASK):]] = content
        for targets in ((TASK, other), (other, TASK)):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, "--strip-packages-from-results")
                for path, name in ((TASK, "mint"), (other, "rose")):
                    for out in (name, "libpy" + name + ".a", "libpy3" + name + ".a"):
                        lib.node_by_output(graph, "$(B)/" + path + "/" + out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
