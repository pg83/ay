import unittest

import lib
from test_go_tests import go_world, go_module, test_node, test_paths, args_after, go_suite_resources


MACRO = """
macro TEAM_SOURCE_CHECK() {
    .ALLOWED_IN_LINTERS_MAKE=yes
    SET(_SALT __MODDIR__ ${MODDIR})
    SET(_FILES_VAR files_${hash:_SALT})
    _GLOB(${_FILES_VAR} ${ARCADIA_ROOT}/${MODDIR}/*_test.go)
    SET(_CHILD_VAR child_${hash:_SALT})
    _GLOB(${_CHILD_VAR} ${ARCADIA_ROOT}/${MODDIR}/checks/ya.make)
    SET(_EMPTY_VAR empty_${hash:_SALT})
    _GLOB(${_EMPTY_VAR} ${ARCADIA_ROOT}/${MODDIR}/missing/ya.make)
    _ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME source_policy WRAPPER_SCRIPT tools/policy/check.py FILES ${${_FILES_VAR}} EXTRA_PARAMS child=${${_CHILD_VAR}} absent=${${_EMPTY_VAR}})
}
"""


def world():
    files = go_world()
    files.update({
        "build/internal/conf/internal.conf": '@import "${CONF_ROOT}/internal/conf/team.conf"\n',
        "build/internal/conf/team.conf": MACRO,
        "build/conf/autoincludes.json": '["project"]\n',
        "project/linters.make.inc": 'IF (MODULE_LANG == GO AND MODULE_TAG != "GO_TEST")\nTEAM_SOURCE_CHECK()\nENDIF()\n',
        "tools/policy/check.py": "",
    })
    go_module(files, "project/first")
    files["project/first/z_test.go"] = "package first\n"
    files["project/first/a_test.go"] = "package first\n"
    files["project/first/checks/ya.make"] = "RECURSE()\n"
    return files


class CustomLintTest(unittest.TestCase):
    def test_declarative_globs_feed_generic_custom_lint(self):
        graph = lib.make(world(), "project/first", "-t")
        node = test_node(graph, "project/first/source_policy")
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("run_custom_lint", args)
        self.assertEqual(args_after(args, "--wrapper-script"), ["tools/policy/check.py"])
        self.assertEqual(args_after(args, "--depends"), ["project/first"])
        self.assertEqual(args_after(args, "--extra-param"), [
            "child=${ARCADIA_ROOT}/project/first/checks/ya.make", "absent=",
        ])
        self.assertNotIn("--config", args)
        sources = ["$(S)/project/first/a_test.go", "$(S)/project/first/z_test.go"]
        self.assertEqual(args[-3:-1], sources)
        self.assertEqual(args_after(args, "--test-related-path"), [*sources, "$(S)/tools/policy/check.py"])
        self.assertEqual(node["deps"], [lib.node_by_output(graph, "$(B)/common_test.context")["uid"],
                                      *go_suite_resources(self, graph, node, world())])

    def test_same_macro_runs_in_each_module_without_leaking_globs(self):
        files = world()
        go_module(files, "project/second", macros="PEERDIR(project/first)\n")
        files["project/second/only_test.go"] = "package second\n"
        graph = lib.make(files, "project/second", "project/first", "-t")
        node = test_node(graph, "project/second/source_policy")
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[-2], "$(S)/project/second/only_test.go")
        self.assertEqual(args_after(args, "--extra-param"), ["child=", "absent="])
        self.assertIn("project/first/source_policy", test_paths(graph))

    def test_explicit_check_options_use_existing_suite_pipeline(self):
        files = world()
        files["project/linters.make.inc"] = ""
        lib.tool_program(files, "helper", "helper")
        files["configs/check.json"] = '{"source_policy": "configs/rules.txt"}'
        files["configs/rules.txt"] = "policy\n"
        files["project/first/ya.make"] = files["project/first/ya.make"].replace("END()", """
_ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME source_policy WRAPPER_SCRIPT tools/policy/check.py
    FILES a_test.go $S/project/first/z_test.go "$(B)/generated.go" $B/generated.go
    DEFAULT_CONFIGS configs/check.json DEPENDS helper FILE_PROCESSING_TIME 40 EXTRA_PARAMS mode=full)
END()
""")
        graph = lib.make(files, "project/first", "-t")
        node = test_node(graph, "project/first/source_policy")
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args_after(args, "--config"), ["$(S)/configs/rules.txt"])
        self.assertEqual(args_after(args, "--extra-param"), ["mode=full"])
        self.assertEqual(args_after(args, "--depends"), ["helper/helper", "project/first"])
        self.assertIn(lib.node_by_output(graph, "$(B)/helper/helper")["uid"], node["deps"])
        self.assertFalse(any("generated.go" in i for i in node["inputs"]))

    def test_empty_files_no_lint_and_tidy_do_not_create_suites(self):
        for macro in ("", "NO_LINT()", "NO_LINT(none_internal)", "SET(_NO_LINT_VALUE none)", "SET(_NO_LINT_VALUE none_internal)", "SET(TIDY yes)"):
            with self.subTest(macro=macro):
                files = world()
                if macro:
                    go_module(files, "project/first", macros=macro + "\n")
                else:
                    del files["project/first/a_test.go"]
                    del files["project/first/z_test.go"]
                graph = lib.make(files, "project/first", "-t")
                self.assertNotIn("project/first/source_policy", test_paths(graph))

    def test_macro_parameters_and_conditions_execute_per_call(self):
        files = world()
        files["build/internal/conf/team.conf"] = """
macro UNRELATED() {
    UNKNOWN_BODY()
}
macro CONDITIONAL_CHECK(Mode, Missing) {
    SET(UNUSED ${Missing})
    IF (Mode == yes)
        _ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME source_policy WRAPPER_SCRIPT tools/policy/check.py FILES a_test.go)
    ELSE()
        _ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME fallback WRAPPER_SCRIPT tools/policy/check.py FILES z_test.go)
    ENDIF()
}
"""
        files["project/linters.make.inc"] = "CONDITIONAL_CHECK(yes)\nCONDITIONAL_CHECK(no)\n"
        graph = lib.make(files, "project/first", "-t")
        self.assertIn("project/first/source_policy", test_paths(graph))
        self.assertIn("project/first/fallback", test_paths(graph))

    def test_invalid_explicit_metadata_reports_configuration_error(self):
        for args, message in (
            ("FILES a_test.go", "NAME and WRAPPER_SCRIPT"),
            ("NAME source_policy WRAPPER_SCRIPT tools/policy/check.py FILES checks/ya.make", "ya.make in collected files"),
            ("NAME source_policy WRAPPER_SCRIPT tools/policy/check.py FILES a_test.go EXTRA_PARAMS broken", "requires key=value"),
            ("NAME source_policy WRAPPER_SCRIPT tools/policy/check.py FILES a_test.go FILE_PROCESSING_TIME broken", "invalid syntax"),
        ):
            with self.subTest(args=args):
                files = world()
                files["project/linters.make.inc"] = "_ADD_CUSTOM_EXPLICIT_LINTER_CHECK(" + args + ")\n"
                result = lib.make_process(files, "project/first", "-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)

    def test_unsupported_wrapper_statements_are_reported(self):
        for command, message in (
            ("OTHER_OPERATION()", "unsupported configuration statement OTHER_OPERATION"),
            ("SRCS(a.go)", "unsupported configuration statement *main.SrcsStmt"),
            ("SET(V ${broken)", "unclosed variable"),
        ):
            with self.subTest(command=command):
                files = world()
                files["build/internal/conf/team.conf"] = MACRO.replace("SET(_SALT", command + "\nSET(_SALT")
                result = lib.make_process(files, "project/first", "-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)

    def test_unknown_macro_without_definition_keeps_normal_diagnostic(self):
        files = world()
        files["project/linters.make.inc"] = "UNDEFINED_STYLE_CHECK()\n"
        result = lib.make_process(files, "project/first", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('macro "UNDEFINED_STYLE_CHECK" is not in the modelled set', result.stderr)


if __name__ == "__main__":
    unittest.main()
