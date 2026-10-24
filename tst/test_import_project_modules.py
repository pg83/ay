import unittest

import lib
from resource_dependencies import global_resource_dependencies
from test_python2 import python2_base
from test_python_srcs import python_base


APP = "canopy/sprout"


def program(files, module=APP, macro="PY3_PROGRAM", body=""):
    files[module + "/ya.make"] = (macro + "(traveler)\n" + lib.BARE_MODULE
        + "NO_PYTHON_INCLUDES()\nPY_SRCS(entry.py=public.entry)\n" + body + "END()\n")
    files[module + "/entry.py"] = "def main(): pass\n"


def fixture(body="", macro="PY3_PROGRAM", legacy=False):
    files = python_base()
    if legacy:
        files.update(python2_base())
    program(files, macro=macro, body=body)
    return files


class ImportProjectModulesTest(unittest.TestCase):
    def make(self, files, *roots, flags=()):
        roots = roots or (APP,)
        return lib.make(files, roots[-1], *roots[:-1], "-t", "--sandboxing", *flags)

    def own_dependencies(self, graph, module=APP, archives=(".global.a",), platform="default-linux-aarch64"):
        outputs = ["$(B)/" + module + "/traveler", *["$(B)/" + module + "/libpy3traveler" + s for s in archives],
                   "$(B)/common_test.context"]
        return {next(n["uid"] for n in graph["graph"] if output in n["outputs"] and n["platform"] == platform)
                for output in outputs}

    def assert_import(self, graph, module=APP, archives=(".global.a",), masks=(), platform="default-linux-aarch64", linter="FLAKE8_PY3"):
        prefix = "$(B)/" + module + "/"
        work = prefix + "test-results/import_test"
        suite = lib.node_by_output(graph, work + "/meta.json")
        resources = global_resource_dependencies(self, graph, suite, [(linter, "sbr:123")])
        self.assertEqual(set(suite["deps"]), self.own_dependencies(graph, module, archives, platform) | set(resources))
        self.assertEqual(len(suite["deps"]), len(set(suite["deps"])))
        self.assertEqual(suite["inputs"], [prefix + "traveler", "$(S)/" + module])
        self.assertEqual(suite["outputs"], [work + p for p in ("/meta.json", "/ytest.report.trace", "/run_test.log", "/testing_out_stuff.tar.zstd")])
        args = suite["cmds"][0]["cmd_args"]
        checker = "run_pyimports --markup" + "".join(" --skip " + p for p in masks) + " --trace-dir=" + work + "/testing_out_stuff"
        self.assertEqual(args[args.index("run_check") - 1:], [
            "$(TEST_TOOL_HOST)/test_tool", "run_check", "--source-root", "$(S)", "--checker", checker,
            "--check-name", "import_test", "--trace-path", work + "/ytest.report.trace", "--out-path", work + "/testing_out_stuff",
            "--log-path", work + "/testing_out_stuff/run_check.log", prefix + "traveler", "--no-snippet-from-stdout"])
        self.assertEqual(suite["cmds"][0]["cwd"], "$(B)")
        self.assertEqual(suite["env"]["LSAN_OPTIONS"], "exitcode=100:detect_leaks=0")
        self.assertEqual(suite["kv"]["path"], module + "/import_test")
        self.assertEqual(suite["requirements"], {"cpu": 1, "network": "restricted", "ram": 8, "ram_disk": 0})
        return suite

    def test_executable_and_live_own_global_archive_are_direct_dependencies(self):
        graph = self.make(fixture())
        suite = self.assert_import(graph)
        binary = lib.node_by_output(graph, "$(B)/" + APP + "/traveler")
        archive = lib.node_by_output(graph, "$(B)/" + APP + "/libpy3traveler.global.a")
        self.assertIn(archive["uid"], binary["deps"])
        self.assertNotIn(archive["uid"], graph["result"])
        self.assertIn(suite["uid"], graph["result"])

    def test_all_own_module_outputs_are_selected_without_selecting_codegen_nodes(self):
        files = fixture("ADDINCL(${BINDIR})\nSRCS(local.cpp GLOBAL exposed.cpp)\nCOPY_FILE(pattern.h.in support.h)\n")
        files[APP + "/local.cpp"] = '#include "support.h"\nint local;\n'
        files[APP + "/exposed.cpp"] = "int exposed;\n"
        files[APP + "/pattern.h.in"] = "#define VALUE 7\n"
        graph = self.make(files)
        suite = self.assert_import(graph, archives=(".a", ".global.a"))
        copy = lib.node_by_output(graph, "$(B)/" + APP + "/support.h")
        self.assertEqual(copy["inputs"], ["$(S)/" + APP + "/pattern.h.in"])
        compiled = next(n for n in graph["graph"] if n["kv"].get("p") == "CC" and "$(S)/" + APP + "/local.cpp" in n["inputs"])
        self.assertIn(copy["uid"], compiled["deps"])
        self.assertIn(copy["outputs"][0], compiled["inputs"])
        self.assertNotIn(copy["uid"], suite["deps"])
        self.assertNotIn(compiled["uid"], suite["deps"])

    def test_peer_archives_remain_transitive_and_other_roots_do_not_leak(self):
        files = fixture("PEERDIR(canopy/words native/caps)\n")
        files.update({"canopy/words/ya.make": "PY3_LIBRARY(words)\n" + lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\nPY_SRCS(words.py)\nEND()\n",
                      "canopy/words/words.py": "word = 1\n",
                      "native/caps/ya.make": "LIBRARY(caps)\n" + lib.BARE_MODULE + "SRCS(local.cpp GLOBAL exposed.cpp)\nEND()\n",
                      "native/caps/local.cpp": "int local;\n", "native/caps/exposed.cpp": "int exposed;\n"})
        for roots in ((APP, "native/caps", "canopy/words"), ("canopy/words", "native/caps", APP)):
            with self.subTest(roots=roots):
                graph = self.make(files, *roots)
                suite = self.assert_import(graph)
                binary = lib.node_by_output(graph, "$(B)/" + APP + "/traveler")
                for output in ("$(B)/canopy/words/libpy3words.global.a", "$(B)/native/caps/libcaps.a", "$(B)/native/caps/libcaps.global.a"):
                    peer = lib.node_by_output(graph, output)
                    self.assertIn(peer["uid"], binary["deps"])
                    self.assertNotIn(peer["uid"], suite["deps"])
                self.assertFalse(any(n["kv"].get("path") == "canopy/words/import_test" for n in graph["graph"]))

    def test_python2_and_python3_bin_do_not_invent_a_library_context(self):
        for macro in ("PY2_PROGRAM", "PY3_PROGRAM_BIN"):
            with self.subTest(macro=macro):
                graph = self.make(fixture(macro=macro, legacy=macro == "PY2_PROGRAM"))
                self.assert_import(graph, archives=(), linter="FLAKE8_PY2" if macro == "PY2_PROGRAM" else "FLAKE8_PY3")
                self.assertFalse(any(n["kv"].get("p") == "AR" and any(o.startswith("$(B)/" + APP + "/") for o in n["outputs"])
                                     for n in graph["graph"]))

    def test_shared_include_keeps_project_queries_and_masks_separate_in_both_orders(self):
        files = python_base()
        files["shared/entries.inc"] = "NO_CHECK_IMPORTS(${MASKS})\nPY_SRCS(extra.py=public.extra)\n"
        modules = ("canopy/cedar", "canopy/birch")
        for module in modules:
            program(files, module, body="SET(MASKS " + module.replace('/', '.') + ".* optional.config)\nINCLUDE(${ARCADIA_ROOT}/shared/entries.inc)\n")
            files[module + "/extra.py"] = "value = 1\n"
        plans = []
        for roots in (modules, modules[::-1]):
            graph = self.make(files, *roots)
            plan = []
            for module in modules:
                suite = self.assert_import(graph, module, masks=(module.replace('/', '.') + ".*", "optional.config"))
                # -G uses per-dump numeric node references for both UID fields.
                plan.append({k: v for k, v in suite.items() if k not in ("deps", "uid", "self_uid")})
            plans.append(plan)
        self.assertEqual(*plans)

    def test_host_tool_context_is_not_an_extra_project_dependency(self):
        files = fixture()
        files.update({"native/driver/ya.make": "LIBRARY(driver)\n" + lib.BARE_MODULE
                      + "ADDINCL(${BINDIR})\nRUN_PROGRAM(" + APP + " OUT_NOAUTO report.h)\nSRCS(driver.cpp)\nEND()\n",
                      "native/driver/driver.cpp": '#include "report.h"\nint driver;\n'})
        for roots in ((APP, "native/driver"), ("native/driver", APP)):
            with self.subTest(roots=roots):
                graph = self.make(files, *roots)
                suite = self.assert_import(graph)
                for suffix in ("traveler", "libpy3traveler.global.a"):
                    output = "$(B)/" + APP + "/" + suffix
                    host = next(n for n in graph["graph"] if output in n["outputs"] and n["platform"] == "default-linux-x86_64")
                    self.assertNotIn(host["uid"], suite["deps"])
                generator = lib.node_by_output(graph, "$(B)/native/driver/report.h")
                self.assertIn(next(n["uid"] for n in graph["graph"] if "$(B)/" + APP + "/traveler" in n["outputs"]
                                   and n["platform"] == "default-linux-x86_64"), generator["deps"])

    def test_unselected_import_suite_does_not_change_build_results(self):
        files = fixture()
        graph = self.make(files, flags=("--test-size", "large"))
        self.assertFalse(any(n["kv"].get("path") == APP + "/import_test" for n in graph["graph"]))
        lib.node_by_output(graph, "$(B)/" + APP + "/traveler")
        lib.node_by_output(graph, "$(B)/" + APP + "/libpy3traveler.global.a")

    def test_target_query_is_preserved_when_host_and_target_architectures_match(self):
        graph = self.make(fixture(), flags=("--target-platform", "default-linux-x86_64"))
        self.assert_import(graph, platform="default-linux-x86_64")

    def test_disabled_import_checks_and_library_contexts_remain_disabled(self):
        for macro, body in (("PY3_PROGRAM", "NO_CHECK_IMPORTS()\n"), ("PY3_LIBRARY", "")):
            with self.subTest(macro=macro):
                graph = self.make(fixture(body, macro=macro))
                self.assertFalse(any(n["kv"].get("path") == APP + "/import_test" for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
