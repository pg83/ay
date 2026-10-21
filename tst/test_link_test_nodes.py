import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def library(body=""):
    return f"LIBRARY()\n{NO_PLATFORM}{body}END()\n"


def make_unsandboxed(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-link-test-nodes-") as directory:
        root = Path(directory)
        lib.seed_root(root)
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        result = lib.run(
            "make", "-j0", "-G",
            "--source-root", root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
            *args, target,
            env=env,
        )
        return json.loads(result.stdout)


def fixture():
    return {
        "ya.conf": 'use_command_file_in_testtool = true\n[flags]\nOPENSOURCE = "yes"\n[host_platform_flags]\nOPENSOURCE = "yes"\n',
        "lib/ya.make": library("SRCS(a.cpp)\n"),
        "lib/a.cpp": "int a(){return 1;}\n",
        "library/cpp/testing/unittest_main/ya.make": library("SRCS(m.cpp)\n"),
        "library/cpp/testing/unittest_main/m.cpp": "int main(){return 0;}\n",
        "res/ya.make": (
            "RESOURCES_LIBRARY()\n"
            "DECLARE_EXTERNAL_RESOURCE(ZTOOL sbr:22 ATOOL sbr:11)\n"
            "END()\n"
        ),
        "lib/ut/ya.make": (
            f"UNITTEST_FOR(lib)\n{NO_PLATFORM}"
            "SRCS(a_ut.cpp GLOBAL g.cpp x.c h.h sub/y.cc)\n"
            "SRC_C_AVX(v.cpp)\n"
            "PEERDIR(res)\n"
            "END()\n"
        ),
        "lib/a_ut.cpp": "int main(){return 0;}\n",
        "lib/g.cpp": "int g;\n",
        "lib/x.c": "int x;\n",
        "lib/h.h": "\n",
        "lib/v.cpp": "int v;\n",
        "lib/sub/y.cc": "int y;\n",
    }


def result_outputs(graph):
    by_uid = {node["uid"]: node for node in graph["graph"]}
    return [by_uid[ref]["outputs"][0] for ref in graph["result"]]


def suite_outputs(suite):
    base = f"$(B)/lib/ut/test-results/{suite}"
    return [
        f"{base}/meta.json",
        f"{base}/ytest.report.trace",
        f"{base}/run_test.log",
        f"{base}/testing_out_stuff.tar.zstd",
    ]


class TestNodesTest(unittest.TestCase):
    def test_recursed_boost_tests_are_not_final_targets_without_tests(self):
        for macro in ("BOOSTTEST", "BOOSTTEST_WITH_MAIN"):
            with self.subTest(macro=macro):
                files = {
                    "maps/sample/ya.make": library() + "RECURSE(ut)\n",
                    "maps/sample/ut/ya.make": f"{macro}()\n{NO_PLATFORM}SRCS(test.cpp)\nEND()\n",
                    "maps/sample/ut/test.cpp": "int test;\n",
                    "library/cpp/testing/boost_test/ya.make": library(),
                    "library/cpp/testing/boost_test_main/ya.make": library(),
                }
                output = "$(B)/maps/sample/ut/ut"
                graph = lib.make(files, "maps/sample")
                self.assertFalse(any(output in node["outputs"] for node in graph["graph"]))
                direct = lib.make(files, "maps/sample/ut")
                lib.node_by_output(direct, output)

    def test_recursed_boost_tests_keep_native_dependencies_and_runner(self):
        for macro in ("BOOSTTEST", "BOOSTTEST_WITH_MAIN"):
            with self.subTest(macro=macro):
                files = {
                    "maps/sample/ya.make": library() + "RECURSE(ut)\n",
                    "maps/sample/ut/ya.make": f"{macro}()\n{NO_PLATFORM}"
                                             "PEERDIR(dep)\nSRCS(test.cpp)\nSPLIT_FACTOR(3)\nEND()\n",
                    "maps/sample/ut/test.cpp": "int test;\n",
                    "dep/ya.make": library("SRCS(native.cpp)\n"),
                    "dep/native.cpp": "int native;\n",
                    "library/cpp/testing/boost_test/ya.make": library("SRCS(support.cpp)\n"),
                    "library/cpp/testing/boost_test/support.cpp": "int support;\n",
                    "library/cpp/testing/boost_test_main/ya.make": library("SRCS(main.cpp)\n"),
                    "library/cpp/testing/boost_test_main/main.cpp": "int main(){return 0;}\n",
                }
                graph = lib.make(files, "maps/sample", "-t")
                binary = lib.node_by_output(graph, "$(B)/maps/sample/ut/ut")
                self.assertIn("$(B)/dep/libdep.a", binary["inputs"])
                self.assertIn("$(B)/library/cpp/testing/boost_test/libcpp-testing-boost_test.a", binary["inputs"])
                self.assertEqual(macro == "BOOSTTEST_WITH_MAIN", any(
                    "/boost_test_main/" in path for path in binary["inputs"]
                ))
                lib.node_by_output(graph, "$(B)/dep/native.cpp.o")
                run = lib.node_by_output(graph, "$(B)/maps/sample/ut/test-results/ut/meta.json")
                self.assertEqual("ut", run["env"]["TEST_NAME"])
                args = run["cmds"][0]["cmd_args"]
                self.assertEqual("boost_test", args[args.index("--test-type") + 1])
                self.assertIn("run_boost_test", args)
                self.assertNotIn("run_ut", args)
                self.assertNotIn("--supports-test-parameters", args)
                self.assertNotIn("--supports-canonization", args)

    def test_unittest_for_emits_context_run_and_declared_style_nodes(self):
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace("END()", "STYLE_CPP()\nEND()")
        files["build/platform/clang/clang-format/ya.make"] = "RESOURCES_LIBRARY()\nEND()\n"
        graph = lib.make(files, "lib/ut", "-t", "-r")
        self.assertEqual(
            result_outputs(graph),
            [
                "$(B)/lib/ut/lib-ut",
                suite_outputs("clang_format")[0],
                suite_outputs("unittest")[0],
            ],
        )

        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertEqual(context["kv"], {"p": "CP", "pc": "light-blue"})
        self.assertEqual(
            context["cmds"][0]["cmd_args"][:3],
            [
                "$(YMAKE_PYTHON3)/bin/python3",
                "$(S)/build/scripts/append_file.py",
                "$(B)/common_test.context",
            ],
        )
        self.assertIn('  "build_type": "release",', context["cmds"][0]["cmd_args"])
        self.assertEqual(context["inputs"], ["$(S)/build/scripts/append_file.py"])
        self.assertEqual(
            context["requirements"], {"network": "restricted"}
        )

        binary = lib.node_by_output(graph, "$(B)/lib/ut/lib-ut")
        self.assertEqual(binary["kv"]["ld_test_node"], "1")
        run = lib.node_by_output(graph, suite_outputs("unittest")[0])
        self.assertEqual(run["outputs"], suite_outputs("unittest"))
        self.assertCountEqual(run["deps"], [binary["uid"], context["uid"]])
        self.assertEqual(
            run["kv"],
            {
                "p": "TS",
                "path": "lib/ut/unittest",
                "pc": "yellow",
                "run_test_node": True,
                "show_out": True,
                "special_runner": "",
            },
        )
        self.assertEqual(
            run["requirements"],
            {"cpu": 1, "network": "restricted", "ram": 8, "ram_disk": 0},
        )
        self.assertEqual(run["env"]["TEST_NAME"], "unittest")
        self.assertEqual(run["env"]["YA_TEST_RUNNER"], "1")
        self.assertEqual(run["cmds"][0]["cwd"], "$(B)")
        self.assertEqual(run["inputs"], ["$(S)/lib/ut"])
        text = " ".join(run["cmds"][0]["cmd_args"])
        self.assertIn(
            "--target-platform-descriptor "
            "default-linux-aarch64-release-FAKEID=sandboxing-SANDBOXING=yes",
            text,
        )
        self.assertIn(
            "--global-resource ATOOL_RESOURCE_GLOBAL::$(B)/resources/ATOOL "
            "--global-resource ZTOOL_RESOURCE_GLOBAL::$(B)/resources/ZTOOL "
            "--ram-limit-gb 8",
            text,
        )
        self.assertIn("run_ut --binary $(B)/lib/ut/lib-ut", text)

        style = lib.node_by_output(graph, suite_outputs("clang_format")[0])
        self.assertEqual(style["deps"], [context["uid"]])
        self.assertNotIn("disable_cache", style["kv"])
        self.assertEqual(style["env"]["TEST_NAME"], "clang_format")
        self.assertCountEqual(
            style["inputs"],
            [
                "$(S)/build/config/tests/cpp_style/.clang-format",
                "$(S)/build/scripts/c_templates/svn_interface.c",
                "$(S)/tools/cpp_style_checker/wrapper.py",
                "$(S)/lib/ut",
                "$(S)/lib/a_ut.cpp",
                "$(S)/lib/g.cpp",
                "$(S)/lib/h.h",
                "$(S)/lib/sub/y.cc",
                "$(S)/lib/v.cpp",
                "$(S)/lib/x.c",
            ],
        )
        command = style["cmds"][0]["cmd_args"]
        lint_files = ["$(S)/build/scripts/c_templates/svn_interface.c", "$(S)/lib/a_ut.cpp", "$(S)/lib/g.cpp", "$(S)/lib/h.h", "$(S)/lib/sub/y.cc", "$(S)/lib/v.cpp", "$(S)/lib/x.c"]
        self.assertEqual(command[-len(lint_files)-1:], lint_files + ["--ya-end-command-file"])
        related_paths = [command[i + 1] for i, arg in enumerate(command) if arg == "--test-related-path"]
        self.assertCountEqual(related_paths, lint_files + ["$(S)/build/config/tests/cpp_style/.clang-format", "$(S)/tools/cpp_style_checker/wrapper.py"])

    def test_cpp_suites_select_runners_and_metadata(self):
        for macro, name, runner, peers, test_ld in [
            ("UNITTEST", "unittest", "run_ut", ["unittest_main"], True),
            ("UNITTEST_WITH_CUSTOM_ENTRY_POINT", "unittest", "run_ut", [], False),
            ("GTEST", "gtest", "run_ut", ["gtest", "gtest_main"], True),
            ("G_BENCHMARK", "tests", "run_g_benchmark", ["gbenchmark"], False),
            ("FUZZ", "fuzz", "run_fuzz", ["nofuzz", "contrib/libs/libfuzzer"], False),
        ]:
            with self.subTest(macro=macro):
                files = {
                    "tests/ya.make": f"{macro}()\n{NO_PLATFORM}SRCS(t.cpp)\n"
                    "SIZE(MEDIUM)\nTIMEOUT(75)\nTEST_CWD(data)\n"
                    "DATA(arcadia/data)\nDEPENDS(tool)\n"
                    "REQUIREMENTS(ram:4 cpu:2 network:full)\nEND()\n",
                    "tests/t.cpp": "int main(){return 0;}\n",
                    "data/input.txt": "input\n",
                    "tool/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(t.cpp)\nEND()\n",
                    "tool/t.cpp": "int main(){return 0;}\n",
                }
                for peer in peers:
                    peer = peer if "/" in peer else "library/cpp/testing/" + peer
                    files[f"{peer}/ya.make"] = library()
                graph = lib.make(files, "tests", "-t")
                run = lib.node_by_output(graph, f"$(B)/tests/test-results/{name}/meta.json")
                args = run["cmds"][0]["cmd_args"]
                self.assertIn(runner, args)
                self.assertEqual(args[args.index("--timeout") + 1], "75")
                self.assertEqual(args[args.index("--test-size") + 1], "medium")
                self.assertEqual(args[args.index("--test-run-cwd") + 1], "$(S)/data")
                self.assertEqual(run["requirements"], {"cpu": 2, "ram": 4, "network": "full", "ram_disk": 0})
                expected_inputs = {"$(S)/data", "$(S)/tests"}
                if macro == "FUZZ":
                    expected_inputs.add("$(S)/fuzzing/tests/corpus.json")
                self.assertEqual(set(run["inputs"]), expected_inputs)
                tool = lib.node_by_output(graph, "$(B)/tool/tool")
                self.assertIn(tool["uid"], run["deps"])
                binary = next(n for n in graph["graph"] if n.get("kv", {}).get("p") == "LD" and n["outputs"][0].startswith("$(B)/tests/"))
                self.assertNotIn(tool["uid"], binary["deps"])
                kv = {"p": "LD", "pc": "light-blue", "show_out": "yes"}
                self.assertEqual(binary["kv"], {**kv, "ld_test_node": "1"} if test_ld else kv)

    def test_python_test_sources_use_tests_namespace_and_pytest_entrypoint(self):
        for macro in ("PY3TEST", "PY2TEST"):
            with self.subTest(macro=macro):
                files = {
                    "a/ut/ya.make": f"{macro}()\n{NO_PLATFORM}"
                    "NO_CHECK_IMPORTS()\nNO_IMPORT_TRACING()\nNO_PYTHON_INCLUDES()\n"
                    "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\n"
                    "TEST_SRCS(test_sample.py)\nEND()\n",
                    "a/ut/test_sample.py": "def test_example(): pass\n",
                }
                for peer in ["library/python/pytest", "library/python/runtime_py3/main", "library/python/runtime/main", "contrib/deprecated/python/subprocess32", "library/cpp/resource"]:
                    files[f"{peer}/ya.make"] = library()
                for tool in ["tools/archiver", "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler", "tools/rescompressor", "tools/py2cc"]:
                    lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
                graph = lib.make(files, "a/ut", "-t")
                binary = lib.node_by_output(graph, "$(B)/a/ut/a-ut")
                self.assertEqual(binary["kv"].get("ld_test_node"), "1" if macro == "PY3TEST" else None)
                all_args = [arg for n in graph["graph"] for cmd in n["cmds"] for arg in cmd["cmd_args"]]
                self.assertIn("__tests__/test_sample.py" if macro == "PY3TEST" else "/py_modules/__tests__.test_sample", " ".join(all_args))
                self.assertIn("PY_MAIN=library.python.pytest.main:main", " ".join(all_args))
                suite = "py3test" if macro == "PY3TEST" else "pytest"
                run = lib.node_by_output(graph, f"$(B)/a/ut/test-results/{suite}/meta.json")
                self.assertIn(binary["uid"], run["deps"])
                args = run["cmds"][0]["cmd_args"]
                self.assertIn("--doctest-modules", args)
                self.assertEqual(args[args.index("--test-type") + 1], "py3test" if macro == "PY3TEST" else "pytest")
                self.assertEqual(run["env"]["PYTHONDONTWRITEBYTECODE"], "1")
                self.assertEqual(run["env"]["ASAN_OPTIONS"], "exitcode=100:detect_leaks=0")
                self.assertEqual(run["env"]["ASAN_OPTIONS_ORIGINAL"], "exitcode=100")
                self.assertEqual(args[args.index("--dep-root") + 1], "a/ut")
                self.assertEqual(args[args.index("--tb") + 1], "short")

    def test_generated_python_test_sources_retain_producer_dependencies(self):
        for macro in ("PY3TEST",):
            with self.subTest(macro=macro):
                files = {
                    "a/ut/ya.make": f"{macro}()\n{NO_PLATFORM}"
                    "NO_CHECK_IMPORTS()\nNO_IMPORT_TRACING()\nNO_PYTHON_INCLUDES()\n"
                    "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\n"
                    "COPY_FILE(input.py test_sample.py)\nTEST_SRCS(test_sample.py)\nEND()\n",
                    "a/ut/input.py": "def test_example(): pass\n",
                }
                for peer in ["library/python/pytest", "library/python/runtime_py3/main", "library/python/runtime/main", "contrib/deprecated/python/subprocess32", "library/cpp/resource"]:
                    files[f"{peer}/ya.make"] = library()
                for tool in ["tools/archiver", "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler", "tools/rescompressor", "tools/py2cc"]:
                    lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
                graph = lib.make(files, "a/ut", "-t")
                binary = lib.node_by_output(graph, "$(B)/a/ut/a-ut")
                all_args = [arg for n in graph["graph"] for cmd in n["cmds"] for arg in cmd["cmd_args"]]
                self.assertIn("__tests__/test_sample.py" if macro == "PY3TEST" else "/py_modules/__tests__.test_sample", " ".join(all_args))
                self.assertIn("PY_MAIN=library.python.pytest.main:main", " ".join(all_args))
                suite = "py3test" if macro == "PY3TEST" else "pytest"
                run = lib.node_by_output(graph, f"$(B)/a/ut/test-results/{suite}/meta.json")
                self.assertIn(binary["uid"], run["deps"])
                args = run["cmds"][0]["cmd_args"]
                self.assertIn("--doctest-modules", args)
                self.assertEqual(args[args.index("--test-type") + 1], "py3test" if macro == "PY3TEST" else "pytest")
                self.assertEqual(run["env"]["PYTHONDONTWRITEBYTECODE"], "1")
                self.assertEqual(run["env"]["ASAN_OPTIONS"], "exitcode=100:detect_leaks=0")
                self.assertEqual(run["env"]["ASAN_OPTIONS_ORIGINAL"], "exitcode=100")
                self.assertEqual(args[args.index("--dep-root") + 1], "a/ut")
                self.assertEqual(args[args.index("--tb") + 1], "short")

                generated = lib.node_by_output(graph, "$(B)/a/ut/test_sample.py")
                bytecode = lib.node_by_output(graph, "$(B)/a/ut/test_sample.py.yapyc3")
                self.assertIn(generated["uid"], bytecode["deps"])

    def test_split_cpp_suite_accumulates_all_chunks(self):
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace("END()", "FORK_SUBTESTS(MODULO)\nSPLIT_FACTOR(3)\nEND()")
        graph = lib.make(files, "lib/ut", "-t")
        chunks = []
        for index in range(3):
            output = f"$(B)/lib/ut/test-results/unittest/chunk{index}/meta.json"
            node = lib.node_by_output(graph, output)
            args = node["cmds"][0]["cmd_args"]
            self.assertEqual(args[args.index("--modulo") + 1], "3")
            self.assertEqual(args[args.index("--modulo-index") + 1], str(index))
            self.assertIn("--split-by-tests", args)
            chunks.append(node)
        merged = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        self.assertEqual(merged["kv"]["p"], "TA")
        self.assertEqual(set(merged["deps"]), {n["uid"] for n in chunks})
        self.assertEqual(set(merged["inputs"]), {o for n in chunks for o in n["outputs"]})
        self.assertIn(merged["uid"], graph["result"])

    def test_fork_test_files_produces_separate_python_chunks(self):
        files = {
            "a/ya.make": f"PY3TEST()\n{NO_PLATFORM}"
            "NO_CHECK_IMPORTS()\nNO_IMPORT_TRACING()\nNO_PYTHON_INCLUDES()\n"
            "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\n"
            "TEST_SRCS(test_z.py test_a.py)\nFORK_TEST_FILES()\nEND()\n",
            "a/test_z.py": "", "a/test_a.py": "",
        }
        for peer in ["library/python/pytest", "library/python/runtime_py3/main", "library/cpp/resource"]:
            files[f"{peer}/ya.make"] = library()
        for tool in ["tools/archiver", "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler", "tools/rescompressor"]:
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        graph = lib.make(files, "a", "-t")
        for name in ["test_a", "test_z"]:
            node = lib.node_by_output(graph, f"$(B)/a/test-results/py3test/{name}/meta.json")
            args = node["cmds"][0]["cmd_args"]
            self.assertEqual(args[args.index("--test-file-filter") + 1], name + ".py")
            self.assertEqual(node["kv"]["split_file"], name + ".py")
        result = lib.node_by_output(graph, "$(B)/a/test-results/py3test/meta.json")
        self.assertEqual(result["kv"]["p"], "TA")

    def test_exectest_builds_runtime_dependencies_without_a_native_link(self):
        graph = lib.make({
            "exec/ya.make": "EXECTEST()\nRUN(tool --flag)\nDEPENDS(tool)\nUSE_RECIPE(tool/tool setup)\nEND()\n",
            "tool/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(t.cpp)\nEND()\n",
            "tool/t.cpp": "int main(){return 0;}\n",
        }, "exec", "-t")
        run = lib.node_by_output(graph, "$(B)/exec/test-results/exectest/meta.json")
        tool = lib.node_by_output(graph, "$(B)/tool/tool")
        self.assertIn(tool["uid"], run["deps"])
        args = run["cmds"][0]["cmd_args"]
        self.assertIn("run_exectest", args)
        self.assertIn("--noconftest", args)
        self.assertIn("--recipes", args)
        self.assertFalse(any(n["kv"]["p"] == "LD" and n["outputs"][0].startswith("$(B)/exec/") for n in graph["graph"]))

    def test_exectest_preserves_runtime_variables_and_sets_its_dependency_path(self):
        import base64

        files = {
            "exec/ya.make": "EXECTEST()\nSET(command tool)\nRUN(${command} ${ARCADIA_ROOT}/input ENV OUTPUT=${TEST_OUT_ROOT})\nDEPENDS(tool)\nEND()\n",
        }
        lib.tool_program(files, "tool", "tool")
        run = lib.node_by_output(lib.make(files, "exec", "-t"), "$(B)/exec/test-results/exectest/meta.json")
        command = run["cmds"][0]["cmd_args"]
        blob = next(arg[len("commands="):] for arg in command if arg.startswith("commands="))
        self.assertEqual(base64.b64decode(blob).decode(), "\ntool ${ARCADIA_ROOT}/input ENV OUTPUT=${TEST_OUT_ROOT}")
        self.assertEqual(run["env"]["PATH"], "$(B)/:$(B)/tool")
        self.assertEqual(run["env"]["ASAN_OPTIONS"], "exitcode=100:detect_leaks=0")
        self.assertEqual(run["env"]["LSAN_OPTIONS_ORIGINAL"], "exitcode=100")

    def test_exectest_path_uses_project_and_resolved_dependency_output_directories(self):
        for name, directory in (("renamed", "tools/driver"), ("bin/renamed", "tools/driver/bin")):
            with self.subTest(name=name):
                files = {
                    "suite/exec/ya.make": "EXECTEST()\nRUN(driver)\nDEPENDS(tools/driver)\nEND()\n",
                }
                lib.tool_program(files, "tools/driver", name)
                graph = lib.make(files, "suite/exec", "-t")
                run = lib.node_by_output(graph, "$(B)/suite/exec/test-results/exectest/meta.json")
                self.assertEqual(run["env"]["PATH"], "$(B)/suite:$(B)/" + directory)

    def test_recurse_for_tests_emits_child_suites_only_with_tests_enabled(self):
        files = fixture()
        files["lib/ya.make"] += "RECURSE_FOR_TESTS(ut)\n"
        normal = lib.make(files, "lib")
        self.assertFalse(any("/test-results/" in o for n in normal["graph"] for o in n["outputs"]))
        graph = lib.make(files, "lib", "-t")
        run = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        self.assertIn(run["uid"], graph["result"])

    def test_recurse_follows_the_branch_its_condition_selects(self):
        files = fixture()
        files["lib/ya.make"] += "IF (OS_LINUX)\n    RECURSE_FOR_TESTS(ut)\nELSE()\n    RECURSE_FOR_TESTS(other)\nENDIF()\n"
        files["lib/other/ya.make"] = files["lib/ut/ya.make"]
        graph = lib.make(files, "lib", "-t")
        lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        self.assertFalse(any(o.startswith("$(B)/lib/other/") for n in graph["graph"] for o in n["outputs"]))

    def test_unsandboxed_debug_descriptor(self):
        graph = make_unsandboxed(fixture(), "lib/ut", "-t")
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertIn('  "build_type": "debug",', context["cmds"][0]["cmd_args"])
        run = lib.node_by_output(graph, suite_outputs("unittest")[0])
        args = run["cmds"][0]["cmd_args"]
        descriptor = args[args.index("--target-platform-descriptor") + 1]
        self.assertEqual(descriptor, "default-linux-aarch64-debug")

    def test_test_nodes_require_flag_and_unittest_root(self):
        files = fixture()
        graph = lib.make(files, "lib/ut")
        self.assertEqual(result_outputs(graph), ["$(B)/lib/ut/lib-ut"])
        files["prog/ya.make"] = (
            f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(lib)\nEND()\n"
        )
        files["prog/m.cpp"] = "int main(){return 0;}\n"
        graph = lib.make(files, "prog", "-t")
        self.assertEqual(result_outputs(graph), ["$(B)/prog/prog"])
        kinds = {node["kv"]["p"] for node in graph["graph"]}
        self.assertNotIn("TS", kinds)


if __name__ == "__main__":
    unittest.main(verbosity=2)
