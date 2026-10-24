"""Python source callbacks collect linter files in their END owner namespace."""
import json
import unittest

import lib
from test_python_srcs import objcopy_nodes
from test_python_suites import fixture, args, values


MODULE = "contrib/cedar/pkg"


def sources(body="PY_SRCS(a.py)\n", macro="PY3_LIBRARY", module=MODULE):
    files = fixture()
    files = {(module + key[3:] if key.startswith("lib/") else key): value
             for key, value in files.items()}
    files[module + "/ya.make"] = f"{macro}()\n{lib.BARE_MODULE}{body}END()\n"
    files.update({module + "/" + name: "value = 1\n"
                  for name in ("a.py", "b.py", "extra.py", "__main__.py")})
    files["lint/flake8.conf"] = ""
    files["lint/black.toml"] = ""
    files["lint/ruff.toml"] = ""
    return files


def lints(graph):
    return [n for n in graph["graph"] if n["kv"].get("run_test_node")
            and "/import_test/" not in n["outputs"][0]]


def lint_files(run):
    command = args(run)
    # The runner places the collected, unique TestFiles after extra parameters.
    return command[command.index("DISABLE_FLAKE8_MIGRATIONS=yes") + 1:]


def has_resource(graph, resource):
    return any("$(B)/resources/" + resource in n["outputs"] for n in graph["graph"])


class PythonLintCollectionTest(unittest.TestCase):
    def assert_bytecode(self, graph, module, filename):
        bytecode = lib.node_by_output(graph, f"$(B)/{module}/{filename}.yapyc3")
        self.assertIn(f"$(S)/{module}/{filename}", bytecode["inputs"])
        self.assertIn("$(B)/tools/py3cc/py3cc", bytecode["inputs"])
        tool = lib.node_by_output(graph, "$(B)/tools/py3cc/py3cc")
        self.assertIn(tool["uid"], bytecode["deps"])
        packed = next(n for n in objcopy_nodes(graph, module)
                      if bytecode["outputs"][0] in n["inputs"])
        self.assertIn(bytecode["uid"], packed["deps"])
        return bytecode

    def test_raw_source_and_config_values_gate_collection_and_all_python_styles(self):
        for body, flags in (
            ("SET(_NO_LINT_VALUE none_internal)\nPY_SRCS(a.py)\n", []),
            ("PY_SRCS(a.py)\nSET(_NO_LINT_VALUE none_internal)\n", []),
            ("ALL_PY_SRCS(RECURSIVE)\n", ["-D_NO_LINT_VALUE=none_internal"]),
            ("NO_LINT()\nPY_SRCS(a.py)\n", []),
        ):
            with self.subTest(body=body, flags=flags):
                graph = lib.make(sources("STYLE_PYTHON()\nSTYLE_RUFF()\n" + body), MODULE, "-t", *flags)
                self.assertEqual(lints(graph), [])
                for resource in ("FLAKE8_PY3", "BLACK", "RUFF"):
                    self.assertFalse(has_resource(graph, resource))
                self.assert_bytecode(graph, MODULE, "a.py")

    def test_final_owner_values_clear_macros_and_repeated_inline_calls(self):
        for body in (
            "NO_LINT()\nSET(_NO_LINT_VALUE)\nPY_SRCS(a.py b.py)\n",
            "PY_SRCS(a.py)\nSET(_NO_LINT_VALUE none_internal)\nPY_SRCS(b.py)\nSET(_NO_LINT_VALUE)\n",
            "TEST_SRCS(b.py)\nNO_LINT()\nNO_LINT(ktlint)\nPY_SRCS(a.py a.py)\n",
            "SET(_NO_LINT_VALUE none_internal)\nNO_LINT(ktlint)\nPY_SRCS(b.py a.py)\n",
        ):
            with self.subTest(body=body):
                graph = lib.make(sources(body), MODULE, "-t")
                run, = lints(graph)
                self.assertEqual(lint_files(run), [f"$(S)/{MODULE}/a.py", f"$(S)/{MODULE}/b.py"])
                self.assertTrue(has_resource(graph, "FLAKE8_PY3"))
                self.assert_bytecode(graph, MODULE, "a.py")

    def test_owner_end_and_eof_precede_directory_namespace_changes(self):
        for initial, after, enabled in (("", "none_internal", True), ("none_internal", "", False)):
            with self.subTest(initial=initial):
                files = sources(f"SET(_NO_LINT_VALUE {initial})\nPY_SRCS(a.py)\n")
                files[MODULE + "/ya.make"] += f"SET(_NO_LINT_VALUE {after})\n"
                graph = lib.make(files, MODULE, "-t")
                self.assertEqual(len(lints(graph)), int(enabled))
                self.assertEqual(has_resource(graph, "FLAKE8_PY3"), enabled)
        files = sources("SET(_NO_LINT_VALUE none_internal)\nPY_SRCS(a.py)\n")
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].removesuffix("END()\n")
        self.assertEqual(lints(lib.make(files, MODULE, "-t")), [])

    def test_owner_lint_fields_are_not_rebound_by_directory_setters(self):
        for inside, outside, enabled in (
            ("", "SET(CPP_ANALYSIS_MODE yes)\nSET(_PY_EXTRA_LINT_FILES_VALUE extra.py)\nSET(PY_LINTER_FILES broken)\n", True),
            ("SET(CPP_ANALYSIS_MODE yes)\n", "SET(CPP_ANALYSIS_MODE)\n", False),
            ("SET(DISABLE_FLAKE8 yes)\n", "SET(DISABLE_FLAKE8)\n", False),
        ):
            with self.subTest(inside=inside):
                files = sources(inside + "PY_SRCS(a.py)\n")
                files[MODULE + "/ya.make"] += outside
                graph = lib.make(files, MODULE, "-t")
                self.assertEqual(len(lints(graph)), int(enabled))
                self.assertEqual(has_resource(graph, "FLAKE8_PY3"), enabled)
                if enabled:
                    self.assertEqual(lint_files(lints(graph)[0]), [f"$(S)/{MODULE}/a.py"])

    def test_none_policy_raw_validation_and_exact_analysis_gate(self):
        for body, flags, error in (
            ("NO_LINT()\nPY_SRCS(a.py)\n", [], "NO_LINT() is allowed only in"),
            ("SET(_NO_LINT_VALUE bad)\nPY_SRCS(a.py)\n", [], "Unsupported value for NO_LINT macro: bad"),
            ("SET(_NO_LINT_VALUE none_internal extra)\nPY_SRCS(a.py)\n", [], "Unsupported value"),
            ("SET(_NO_LINT_VALUE bad)\n", [], "Unsupported value"),
            ("SET(_NO_LINT_VALUE bad)\nPY_SRCS(a.py)\n", ["-DCPP_ANALYSIS_MODE=yes"], "Unsupported value"),
            ("SET(_NO_LINT_VALUE bad)\nPY_SRCS(a.py)\n", ["-DDISABLE_FLAKE8=yes"], "Unsupported value"),
            ("SET(_NO_LINT_VALUE bad)\n", ["-DCPP_ANALYSIS_MODE=true"], "Unsupported value"),
        ):
            with self.subTest(body=body, flags=flags):
                result = lib.make_process(sources(body, module="cedar/pkg"), "cedar/pkg", "-t", *flags)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
        graph = lib.make(sources("SET(_NO_LINT_VALUE bad)\n"), MODULE, "-t", "-DCPP_ANALYSIS_MODE=yes")
        self.assertEqual(lints(graph), [])
        graph = lib.make(sources("PY_SRCS(a.py)\n"), MODULE, "-t", "-DCPP_ANALYSIS_MODE=true")
        self.assertEqual(len(lints(graph)), 1)

    def test_extras_seed_fields_and_actual_source_build_resolution(self):
        body = ("SRCDIR(shared)\nCOPY_FILE(a.py generated.py)\n"
                "PY_EXTRA_LINT_FILES(extra.py missing.py generated.py ${ARCADIA_ROOT}/shared/rooted.py)\n"
                "PY_SRCS(a.py generated.py b.py stub.pyi)\n")
        files = sources(body)
        files.update({"shared/b.py": "", "shared/rooted.py": "", MODULE + "/stub.pyi": ""})
        graph = lib.make(files, MODULE, "-t")
        run, = lints(graph)
        expected = [f"$(S)/{MODULE}/a.py", f"$(S)/{MODULE}/b.py", f"$(S)/{MODULE}/extra.py", "$(S)/shared/rooted.py"]
        self.assertEqual(lint_files(run), expected)
        for source in expected:
            self.assertIn(source, run["inputs"])
        self.assertNotIn(f"$(B)/{MODULE}/generated.py", run["inputs"])
        generated = lib.node_by_output(graph, f"$(B)/{MODULE}/generated.py")
        bytecode = lib.node_by_output(graph, f"$(B)/{MODULE}/generated.py.yapyc3")
        self.assertIn(generated["uid"], bytecode["deps"])
        self.assertIn(generated["outputs"][0], bytecode["inputs"])
        resource = lib.node_by_output(graph, "$(B)/resources/FLAKE8_PY3")
        self.assertIn("FLAKE8_PY3_RESOURCE_GLOBAL::" + resource["outputs"][0], values(run, "--global-resource"))

        # Both native source-root spellings are accepted; BUILD data is opaque.
        seed = json.dumps(["$S/shared/seed.py", "${ARCADIA_ROOT}/shared/other.py", "$B/generated.py"])
        files = sources("PY_SRCS(a.py)\n")
        files["build/ymake_conf.py"] = lib.ymake_conf(extra="\nPY_LINTER_FILES=" + seed + "\n")
        seeded, = lints(lib.make(files, MODULE, "-t"))
        self.assertEqual(lint_files(seeded), [f"$(S)/{MODULE}/a.py", "$(S)/shared/other.py", "$(S)/shared/seed.py"])

    def test_file_field_validation_is_distinct_from_collection_and_linter_gates(self):
        for body, seed, flags, expected_error in (
            ("PY_SRCS(a.py)\n", "broken", [], "Invalid PY_LINTER_FILES"),
            ("PY_SRCS(a.py)\n", "null", ["-DCPP_ANALYSIS_MODE=yes"], "Invalid PY_LINTER_FILES"),
            ("PY_SRCS(a.py)\n", "broken", ["-DCPP_ANALYSIS_MODE=yes"], "Invalid PY_LINTER_FILES"),
            ("", '["$B/only.py"]', [], "No files to lint"),
        ):
            with self.subTest(body=body, seed=seed, flags=flags):
                files = sources(body)
                files["build/ymake_conf.py"] = lib.ymake_conf(extra="\nPY_LINTER_FILES=" + seed + "\n")
                result = lib.make_process(files, MODULE, *flags)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected_error, result.stderr)
        for body, flags in (("", ["-DCPP_ANALYSIS_MODE=yes"]), ("SET(_NO_LINT_VALUE none_internal)\nPY_SRCS(a.py)\n", [])):
            files = sources(body)
            files["build/ymake_conf.py"] = lib.ymake_conf(extra="\nPY_LINTER_FILES=broken\n")
            self.assertEqual(lints(lib.make(files, MODULE, "-t", *flags)), [])

    def test_disabled_flake8_keeps_file_validation_without_fetching_unused_resource(self):
        graph = lib.make(sources("PY_SRCS(a.py)\n"), MODULE, "-t", "-DDISABLE_FLAKE8=yes")
        self.assertEqual(lints(graph), [])
        self.assertFalse(has_resource(graph, "FLAKE8_PY3"))
        files = sources("")
        files["build/ymake_conf.py"] = lib.ymake_conf(extra='\nPY_LINTER_FILES=["$B/only.py"]\n')
        graph = lib.make(files, MODULE, "-t", "-DDISABLE_FLAKE8=yes")
        self.assertEqual(lints(graph), [])
        self.assertFalse(has_resource(graph, "FLAKE8_PY3"))
        graph = lib.make(sources("PY_SRCS(a.py)\n"), MODULE, "-t", "-DDISABLE_FLAKE8=true")
        self.assertEqual(len(lints(graph)), 1)
        files = sources("PY_SRCS(a.py)\n")
        files["build/config/tests/py_style/default_configs.json"] = "{}"
        result = lib.make_process(files, MODULE, "-t", "-DDISABLE_FLAKE8=yes")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("config", result.stderr)

    def test_shared_include_and_independent_root_orders_keep_files_and_owner_flags(self):
        files = sources("SET(_NO_LINT_VALUE none_internal)\nINCLUDE(${ARCADIA_ROOT}/shared/sources.inc)\n")
        other = "contrib/birch/pkg"
        files.update(sources("SET(_NO_LINT_VALUE ktlint)\nINCLUDE(${ARCADIA_ROOT}/shared/sources.inc)\n", module=other))
        files["shared/sources.inc"] = "PY_SRCS(a.py)\nPY_EXTRA_LINT_FILES(extra.py)\nTEST_SRCS(b.py)\nSTYLE_PYTHON()\n"
        for first, second in ((MODULE, other), (other, MODULE)):
            with self.subTest(first=first):
                graph = lib.make(files, first, second, "-t")
                runs = lints(graph)
                self.assertEqual(len(runs), 2)
                for run in runs:
                    self.assertTrue(run["outputs"][0].startswith(f"$(B)/{other}/"))
                    self.assertIn(f"$(S)/{other}/a.py", run["inputs"])
                    self.assertIn(f"$(S)/{other}/b.py", run["inputs"])
                    self.assertIn(f"$(S)/{other}/extra.py", run["inputs"])
                    self.assertNotIn(f"$(S)/{MODULE}/a.py", run["inputs"])
                self.assert_bytecode(graph, MODULE, "a.py")
                self.assert_bytecode(graph, other, "a.py")

    def test_cython_callbacks_collect_only_their_source_python_and_extras(self):
        from test_python_cython import python_base
        files = python_base()
        files.update(sources("BUILDWITH_CYTHON_C(direct.py)\nPY_SRCS(CYTHONIZE_PY fast.py)\nPY_EXTRA_LINT_FILES(extra.py)\n"))
        files.update({MODULE + "/direct.py": "", MODULE + "/fast.py": ""})
        graph = lib.make(files, MODULE, "-t")
        run, = lints(graph)
        self.assertEqual(lint_files(run), [f"$(S)/{MODULE}/extra.py", f"$(S)/{MODULE}/fast.py"])
        cy = lib.node_by_output(graph, f"$(B)/{MODULE}/fast.py.cpp")
        cc = lib.node_by_output(graph, f"$(B)/{MODULE}/fast.py.cpp.o")
        self.assertIn(cy["uid"], cc["deps"])
        self.assertIn(cy["outputs"][0], cc["inputs"])
        self.assertIn(f"$(S)/{MODULE}/fast.py", cy["inputs"])
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace("PY_SRCS", "SRCDIR(shared)\nPY_SRCS", 1)
        files["shared/fast.py"] = files.pop(MODULE + "/fast.py")
        run, = lints(lib.make(files, MODULE, "-t"))
        self.assertEqual(lint_files(run), [f"$(S)/{MODULE}/extra.py", "$(S)/shared/fast.py"])
        files[MODULE + "/ya.make"] = f"PY3_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(CYTHONIZE_PY leaf.pyx)\nPY_EXTRA_LINT_FILES(extra.py)\nEND()\n"
        files[MODULE + "/leaf.pyx"] = ""
        run, = lints(lib.make(files, MODULE, "-t"))
        self.assertEqual(lint_files(run), [f"$(S)/{MODULE}/extra.py"])
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace("PY_SRCS", "SET(_NO_LINT_VALUE bad)\nPY_SRCS")
        result = lib.make_process(files, MODULE, "-t", "-DCPP_ANALYSIS_MODE=yes")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Unsupported value for NO_LINT macro: bad", result.stderr)

    def test_python2_and_explicit_native_styles_use_the_same_file_collector(self):
        for macro, style, name in (("PY2_LIBRARY", "", "py2_flake8"), ("LIBRARY", "STYLE_PYTHON()\n", "black")):
            for raw, enabled in (("ktlint", True), ("none_internal", False)):
                with self.subTest(macro=macro, raw=raw):
                    files = sources(f"{style}PY_SRCS(a.py)\nSET(_NO_LINT_VALUE {raw})\n", macro=macro)
                    lib.tool_program(files, "tools/pycc", "pycc")
                    lib.tool_program(files, "tools/py2cc", "py2cc")
                    files["build/config/tests/py_style/default_configs.json"] = json.dumps({name: "lint/flake8.conf"})
                    graph = lib.make(files, MODULE, "-t")
                    runs = lints(graph)
                    self.assertEqual(len(runs), int(enabled))
                    if enabled:
                        self.assertEqual(values(runs[0], "--lint-name"), [name])
                        self.assertEqual(args(runs[0])[-1], f"$(S)/{MODULE}/a.py")
                    else:
                        self.assertFalse(has_resource(graph, "FLAKE8_PY2" if macro == "PY2_LIBRARY" else "BLACK"))

    def test_import_test_and_main_only_binary_keep_their_independent_graph(self):
        files = sources("SET(_NO_LINT_VALUE none_internal)\nPY_SRCS(MAIN a.py)\n", macro="PY3_PROGRAM")
        graph = lib.make(files, MODULE, "-t")
        self.assertEqual(lints(graph), [])
        run = next(n for n in graph["graph"] if "/import_test/meta.json" in n["outputs"][0])
        binary = lib.node_by_output(graph, f"$(B)/{MODULE}/pkg")
        self.assertEqual(run["inputs"], [f"$(B)/{MODULE}/pkg", f"$(S)/{MODULE}"])
        self.assertIn(binary["uid"], run["deps"])
        self.assert_bytecode(graph, MODULE, "a.py")
        files = sources("SET(_NO_LINT_VALUE none_internal)\nPY_SRCS(MAIN a.py)\n", macro="PY3_PROGRAM_BIN")
        graph = lib.make(files, MODULE, "-t")
        self.assertEqual(lints(graph), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
