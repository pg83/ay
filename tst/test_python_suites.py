import json
import unittest

import lib
from test_python_srcs import python_base


BARE = lib.BARE_MODULE


def fixture(body="", macro="PY3_LIBRARY"):
    files = python_base()
    files.update({
        "lib/ya.make": f"{macro}()\n{BARE}{body}PY_SRCS(a.py)\nEND()\n",
        "lib/a.py": "",
        "build/config/tests/py_style/default_configs.json": json.dumps({
            "flake8": "lint/flake8.conf", "black": "lint/black.toml", "ruff": "lint/ruff.toml",
        }),
    })
    for name, resource in (("flake8_py3", "FLAKE8_PY3"), ("black", "BLACK"), ("ruff", "RUFF")):
        files[f"build/external_resources/{name}/ya.make"] = (
            f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE({resource} sbr:123)\nEND()\n"
        )
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


def values(node, flag):
    command = args(node)
    return [command[i + 1] for i, arg in enumerate(command) if arg == flag]


def lint(graph, name):
    return lib.node_by_output(graph, f"$(B)/lib/test-results/{name}/meta.json")


class PythonSuitesTest(unittest.TestCase):
    def test_python3_library_lints_only_resolved_python_sources(self):
        files = fixture("SRCDIR(other)\nCOPY_FILE(a.py copied.py)\nPY_SRCS(copied.py b.py stub.pyi)\n")
        files["other/b.py"] = ""
        files["lib/stub.pyi"] = ""
        run = lint(lib.make(files, "lib", "-t"), "flake8")
        self.assertEqual(values(run, "--global-resource"), [
            "FLAKE8_PY3_RESOURCE_GLOBAL::$(B)/resources/FLAKE8_PY3",
        ] * 2)
        self.assertEqual(values(run, "--test-related-path"), [
            "$(S)/lib/a.py", "$(S)/other/b.py", "$(S)/lint/flake8.conf",
            "$(S)/tools/flake8_linter/__main__.py",
        ])
        command = args(run)
        self.assertEqual(command[-2:], ["$(S)/lib/a.py", "$(S)/other/b.py"])
        self.assertNotIn("$(B)/lib/copied.py", command)
        self.assertEqual(values(run, "--extra-param"), ["DISABLE_FLAKE8_MIGRATIONS=yes"])

    def test_black_and_ruff_use_shared_suites_resources_and_macro_options(self):
        graph = lib.make(fixture("STYLE_PYTHON()\nSTYLE_RUFF(CHECK_FORMAT)\n"), "lib", "-t", "-DDISABLE_FLAKE8=yes")
        for name, wrapper, resource in (("black", "tools/black_linter/wrapper.py", "BLACK"), ("ruff", "tools/ruff_linter/wrapper.py", "RUFF")):
            with self.subTest(name=name):
                run = lint(graph, name)
                self.assertEqual(values(run, "--lint-name"), [name])
                self.assertEqual(values(run, "--wrapper-script"), [wrapper])
                self.assertEqual(values(run, "--config"), [f"$(S)/lint/{name}.toml"])
                self.assertEqual(values(run, "--global-resource"), [f"{resource}_RESOURCE_GLOBAL::$(B)/resources/{resource}"] * 2)
                self.assertEqual(run["kv"]["p"], "TS")
        self.assertEqual(values(lint(graph, "ruff"), "--extra-param"), ["check_format=yes", "run_in_source_root=no"])
        self.assertFalse(any("test-results/flake8/" in out for n in graph["graph"] for out in n["outputs"]))

    def test_python_lints_obey_analysis_and_no_lint_after_macros(self):
        for body, flags in (("NO_LINT()\n", []), ("", ["-DCPP_ANALYSIS_MODE=yes"])):
            with self.subTest(body=body, flags=flags):
                graph = lib.make(fixture("STYLE_PYTHON()\nSTYLE_RUFF()\n" + body), "lib", "-t", *flags)
                self.assertFalse(any(n["kv"].get("run_test_node") for n in graph["graph"]))

    def test_explicit_config_type_resolves_from_common_configs(self):
        files = fixture("SET(MODULE_COMMON_CONFIGS_DIR lint/shared)\nSTYLE_RUFF(CONFIG_TYPE ruff.toml)\n")
        files["lint/shared/ruff.toml"] = ""
        self.assertEqual(values(lint(lib.make(files, "lib", "-t"), "ruff"), "--config"), ["$(S)/lint/shared/ruff.toml"])
        for body in ("STYLE_RUFF(CONFIG_TYPE ruff.toml)\n", "STYLE_RUFF(CONFIG_TYPE unknown.toml)\n"):
            with self.subTest(body=body):
                result = lib.make_process(fixture(body), "lib", "-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("config", result.stderr.lower())

    def test_autoincluded_linter_selects_its_common_configuration(self):
        files = fixture()
        files["build/conf/autoincludes.json"] = '["lib"]'
        files["lib/linters.make.inc"] = "STYLE_RUFF(CONFIG_TYPE ruff.toml)\n"
        files["lib/ruff.toml"] = ""
        self.assertEqual(values(lint(lib.make(files, "lib", "-t"), "ruff"), "--config"), ["$(S)/lib/ruff.toml"])

    def test_flake8_processing_time_chunks_unique_source_files(self):
        files = fixture("SET(FLAKE8_FILE_PROCESSING_TIME 31)\nPY_SRCS(a.py b.py c.py)\n")
        files.update({"lib/b.py": "", "lib/c.py": ""})
        graph = lib.make(files, "lib", "-t")
        runs = [n for n in graph["graph"] if n["kv"].get("run_test_node")]
        self.assertEqual(len(runs), 2)
        self.assertEqual(sorted(args(n)[args(n).index("DISABLE_FLAKE8_MIGRATIONS=yes") + 1:] for n in runs), [
            ["$(S)/lib/a.py", "$(S)/lib/c.py"], ["$(S)/lib/b.py"],
        ])

    def test_ruff_source_root_option_obeys_upstream_allowed_projects(self):
        result = lib.make_process(fixture("STYLE_RUFF(RUN_IN_SOURCE_ROOT)\n"), "lib", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not allowed", result.stderr)
        files = fixture("STYLE_RUFF(RUN_IN_SOURCE_ROOT)\n")
        files = {(key.replace("lib/", "taxi/app/", 1) if key.startswith("lib/") else key): value for key, value in files.items()}
        run = lib.node_by_output(lib.make(files, "taxi/app", "-t"), "$(B)/taxi/app/test-results/ruff/meta.json")
        self.assertEqual(values(run, "--extra-param"), ["check_format=no", "run_in_source_root=yes"])

    def test_py23_library_inherits_flake8_but_native_library_does_not(self):
        lint(lib.make(fixture(macro="PY23_LIBRARY"), "lib", "-t"), "flake8")
        files = fixture()
        files["lib/ya.make"] = f"PY23_NATIVE_LIBRARY()\n{BARE}SRCS(a.cpp)\nEND()\n"
        files["lib/a.cpp"] = "int a;\n"
        graph = lib.make(files, "lib", "-t")
        self.assertFalse(any(n["kv"].get("run_test_node") for n in graph["graph"]))

    def test_empty_python_lint_keeps_resource_peer_without_validating_config(self):
        files = fixture()
        files["lib/ya.make"] = f"LIBRARY()\n{BARE}STYLE_RUFF(CONFIG_TYPE unknown CHECK_FORMAT RUN_IN_SOURCE_ROOT)\nEND()\n"
        graph = lib.make(files, "lib", "-t")
        lib.node_by_output(graph, "$(B)/resources/RUFF")
        self.assertFalse(any(n["kv"].get("run_test_node") for n in graph["graph"]))

    def test_python_program_import_suite_uses_binary_masks_and_own_environment(self):
        for body, enabled in (("", True), ("NO_CHECK_IMPORTS(a.b c.*)\n", True), ("NO_CHECK_IMPORTS()\n", False)):
            with self.subTest(body=body):
                graph = lib.make(fixture(body, macro="PY3_PROGRAM_BIN"), "lib", "-t", "--tests-retries", "3")
                imports = [n for n in graph["graph"] if n["kv"].get("run_test_node") and "/import_test/" in n["outputs"][0]]
                self.assertEqual(len(imports), int(enabled))
                if not enabled:
                    continue
                run = imports[0]
                self.assertEqual(run["inputs"], ["$(B)/lib/lib", "$(S)/lib"])
                self.assertIn(lib.node_by_output(graph, "$(B)/lib/lib")["uid"], run["deps"])
                self.assertNotIn("--setup-pythonpath-env", args(run))
                self.assertEqual(run["env"]["LSAN_OPTIONS"], "exitcode=100:detect_leaks=0")
                self.assertEqual(run["env"]["ASAN_OPTIONS"], "exitcode=100")
                self.assertNotIn("YA_PYTHON_BIN", run["env"])
                self.assertNotIn("--retry", args(run))
                checker = "run_pyimports --markup" + (" --skip a.b --skip c.*" if body else "")
                self.assertEqual(values(run, "--checker"), [checker + " --trace-dir=$(B)/lib/test-results/import_test/testing_out_stuff"])

    def test_python_import_checks_are_disabled_for_analysis(self):
        graph = lib.make(fixture(macro="PY3_PROGRAM_BIN"), "lib", "-t", "-DCPP_ANALYSIS_MODE=yes")
        self.assertFalse(any("/import_test/" in out for n in graph["graph"] for out in n["outputs"]))

    def test_python3_test_program_has_test_link_metadata(self):
        files = fixture(macro="PY3TEST")
        files["library/python/pytest/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        graph = lib.make(files, "lib", "-t")
        self.assertEqual(lib.node_by_output(graph, "$(B)/lib/lib")["kv"].get("ld_test_node"), "1")

    def test_python3_test_uses_its_multimodule_child_tag(self):
        files = fixture(
            'IF (MODULE_TAG == "PY3TEST_PROGRAM")\nPY_SRCS(selected.py)\n'
            'ELSE()\nPY_SRCS(wrong.py)\nENDIF()\n', macro="PY3TEST",
        )
        files["library/python/pytest/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        files.update({"lib/selected.py": "", "lib/wrong.py": ""})
        graph = lib.make(files, "lib", "-t")
        lib.node_by_output(graph, "$(B)/lib/selected.py.yapyc3")
        self.assertFalse(any("$(B)/lib/wrong.py.yapyc3" in n["outputs"] for n in graph["graph"]))

    def test_runner_uses_declared_canonical_data_subpath(self):
        files = fixture("SET(CANONIZE_SUB_PATH selected)\n", macro="PY3TEST")
        files["library/python/pytest/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        run = lint(lib.make(files, "lib", "-t"), "py3test")
        self.assertEqual(values(run, "--sub-path"), ["selected"])

    def test_python_test_resolves_usrv_generated_source_from_build_srcdir(self):
        files = fixture(
            "RUN_PROGRAM(tools/gen OUT_NOAUTO manifest.dat generated.tar)\n"
            "USRV_BUILD(FROM ${BINDIR}/generated.tar lib/nested/test_generated.py)\n"
            "SRCDIR(${CMAKE_CURRENT_BINARY_DIR}/lib/nested)\n"
            "TEST_SRCS(test_generated.py)\n", macro="PY3TEST",
        )
        files["library/python/pytest/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        files["taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/ya.make"] = (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(USRV_UNPACK_TAR sbr:123)\nEND()\n"
        )
        lib.tool_program(files, "tools/gen", "gen")
        for flags in ([], ["-DPYBUILD_NO_PYC=yes"]):
            with self.subTest(flags=flags):
                graph = lib.make(files, "lib", "-t", *flags)
                generated = lib.node_by_output(graph, "$(B)/lib/lib/nested/test_generated.py")
                self.assertEqual(generated["inputs"], ["$(B)/lib/generated.tar", "$(B)/lib/manifest.dat"])
                self.assertEqual(generated["kv"], {"p": "UT", "pc": "light-cyan", "show_out": "yes"})
                self.assertEqual(args(generated)[-3:], ["--target", "$(B)/lib", "$(B)/lib/lib/nested/test_generated.py"])
                if not flags:
                    bytecode = lib.node_by_output(graph, "$(B)/lib/test_generated.py.yapyc3")
                    self.assertEqual(args(bytecode)[-3:], ["test_generated.py-", "$(B)/lib/lib/nested/test_generated.py", "$(B)/lib/test_generated.py.yapyc3"])
                    self.assertIn(generated["uid"], bytecode["deps"])
                self.assertNotIn("$(S)/lib/test_generated.py", args(lint(graph, "flake8")))
                namespaces = [a for node in graph["graph"] for command in node["cmds"] for a in command["cmd_args"] if a.startswith("py/namespace/")]
                self.assertFalse(any("__tests__." in a for a in namespaces))


if __name__ == "__main__":
    unittest.main(verbosity=2)
