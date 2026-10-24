"""Compiler flag macros evaluate source expressions before their bound bodies."""
import unittest

import lib


def library(body):
    return f"LIBRARY()\n{lib.BARE_MODULE}{body}END()\n"


def fixture(body):
    return {
        "cedar/ya.make": library(body + "SRCS(unit.cpp unit.c)\n"),
        "cedar/unit.cpp": "int cpp;\n",
        "cedar/unit.c": "int c;\n",
    }


def flags(graph, directory="cedar", source="unit.cpp"):
    node = lib.node_by_output(graph, f"$(B)/{directory}/{source}.o")
    return [arg for arg in node["cmds"][0]["cmd_args"] if arg.startswith("-DPROBE_")]


class CompilerFlagSubstitutionTest(unittest.TestCase):
    def test_missing_braced_values_disappear_at_the_call_and_keep_flag_order(self):
        graph = lib.make(fixture(
            "SET(MAJOR 7)\nSET(MINOR 3)\n"
            "CFLAGS(-DPROBE_FIRST -DPROBE_FULL=${ABSENT} -DPROBE_MAJOR=${MAJOR} "
            "-DPROBE_MINOR=${MINOR} -DPROBE_LAST)\n"
            "SET(ABSENT too_late)\nSET(MAJOR changed)\n"
        ), "cedar")
        expected = ["-DPROBE_FIRST", "-DPROBE_FULL=", "-DPROBE_MAJOR=7",
                    "-DPROBE_MINOR=3", "-DPROBE_LAST"]
        for source in ("unit.cpp", "unit.c"):
            self.assertEqual(flags(graph, source=source), expected)

    def test_language_specific_macros_use_the_same_pinned_append_body(self):
        graph = lib.make(fixture(
            "SET(VALUE captured)\n"
            "CXXFLAGS(-DPROBE_CXX=${VALUE} -DPROBE_CXX_EMPTY=${MISSING})\n"
            "CONLYFLAGS(-DPROBE_C=${VALUE} -DPROBE_C_EMPTY=${MISSING})\n"
            "SET(VALUE later)\nSET(MISSING later)\n"
        ), "cedar")
        self.assertEqual(flags(graph), ["-DPROBE_CXX=captured", "-DPROBE_CXX_EMPTY="])
        self.assertEqual(flags(graph, source="unit.c"), ["-DPROBE_C=captured", "-DPROBE_C_EMPTY="])

    def test_config_aliases_expand_deeply_after_single_source_dereference(self):
        files = fixture(
            "CFLAGS(-DPROBE_BARE=$STEP0 -DPROBE_BRACED=${STEP0} "
            "-DPROBE_CAPTURE=${CAPTURE})\nSET(CHOICE changed)\n"
        )
        config = "".join(f"STEP{i}=$STEP{i+1}\n" for i in range(12))
        config += "STEP12=finished\nCAPTURE=${CHOICE}\nCHOICE=before\n"
        files["build/ymake_conf.py"] = lib.ymake_conf(extra=config)
        graph = lib.make(files, "cedar")
        self.assertEqual(flags(graph), ["-DPROBE_BARE=finished", "-DPROBE_BRACED=finished",
                                       "-DPROBE_CAPTURE=before"])

    def test_shared_lexical_include_captures_each_module_environment(self):
        files = {"policy/shared.inc": "CFLAGS(-DPROBE_VALUE=${VALUE} -DPROBE_MISSING=${UNSET})\n"}
        for directory, value in (("cedar", "first"), ("maple", "second")):
            files[f"{directory}/ya.make"] = library(
                f"SET(VALUE {value})\nINCLUDE(${{ARCADIA_ROOT}}/policy/shared.inc)\n"
                "SET(VALUE changed)\nSRCS(unit.cpp)\n"
            )
            files[f"{directory}/unit.cpp"] = "int unit;\n"
        for targets in (("cedar", "maple"), ("maple", "cedar")):
            graph = lib.make(files, *targets)
            for directory, value in (("cedar", "first"), ("maple", "second")):
                self.assertEqual(flags(graph, directory),
                                 [f"-DPROBE_VALUE={value}", "-DPROBE_MISSING="])

    def test_global_prefix_is_applied_before_bound_alias_expansion(self):
        files = fixture(
            'GLOBAL_CFLAGS($WORDS)\nGLOBAL_CFLAGS("-DPROBE_THREE -DPROBE_FOUR")\n'
            "GLOBAL_CFLAGS(${WORDS})\n"
            "CFLAGS(-DPROBE_PRIVATE GLOBAL -DPROBE_EXPORTED=${MISSING})\n"
        )
        files["build/ymake_conf.py"] = lib.ymake_conf(extra="WORDS=-DPROBE_ONE -DPROBE_TWO\n")
        files["maple/ya.make"] = library("PEERDIR(cedar)\nSRCS(unit.cpp)\n")
        files["maple/unit.cpp"] = "int consumer;\n"
        graph = lib.make(files, "cedar", "maple")
        exported = ["-DPROBE_ONE", "-DPROBE_THREE", "-DPROBE_ONE", "-DPROBE_TWO",
                    "-DPROBE_EXPORTED="]
        self.assertEqual(flags(graph), ["-DPROBE_TWO", "-DPROBE_FOUR", "-DPROBE_PRIVATE", *exported])
        self.assertEqual(flags(graph, "maple"), exported)

    def test_escaped_references_remain_literal_without_rebinding(self):
        graph = lib.make(fixture(
            "SET(VALUE before)\n" +
            r'CFLAGS(-DPROBE_BRACED=\\\${VALUE} -DPROBE_BARE=\\\$VALUE -DPROBE_BOUND=${VALUE})' +
            "\nSET(VALUE after)\n"
        ), "cedar")
        self.assertEqual(flags(graph), ["-DPROBE_BRACED=${VALUE}", "-DPROBE_BARE=$VALUE",
                                       "-DPROBE_BOUND=before"])

    def test_reserved_roots_keep_real_generated_inputs_and_tool_outputs(self):
        files = fixture(
            "CFLAGS(-DPROBE_SOURCE=${ARCADIA_ROOT}/asset -DPROBE_BUILD=${ARCADIA_BUILD_ROOT}/asset "
            "-DPROBE_LOCAL=${CURDIR}/asset -DPROBE_OUTPUT=${BINDIR}/asset "
            "-DPROBE_TAIL)\n"
            "RUN_PROGRAM(tools/produce OUT generated.cpp)\n"
        )
        lib.tool_program(files, "tools/produce", "renamed-generator")
        graph = lib.make(files, "cedar")
        expected = ["-DPROBE_SOURCE=$(S)/asset", "-DPROBE_BUILD=$(B)/asset",
                    "-DPROBE_LOCAL=$(S)/cedar/asset", "-DPROBE_OUTPUT=$(B)/cedar/asset",
                    "-DPROBE_TAIL"]
        self.assertEqual(flags(graph), expected)
        self.assertEqual(flags(graph, source="generated.cpp"), expected)
        generated = lib.node_by_output(graph, "$(B)/cedar/generated.cpp")
        tool = lib.node_by_output(graph, "$(B)/tools/produce/renamed-generator")
        compiled = lib.node_by_output(graph, "$(B)/cedar/generated.cpp.o")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(tool["outputs"][0], generated["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(generated["outputs"][0], compiled["inputs"])

    def test_resource_global_is_deferred_in_the_source_binding(self):
        graph = lib.make({
            "resource/ya.make": (
                "LIBRARY()\n" + lib.BARE_MODULE +
                "CFLAGS(-DPROBE_RESOURCE=${MINT_RESOURCE_GLOBAL}/include -DPROBE_TAIL)\n"
                'IF(USER_CFLAGS MATCHES "MINT_RESOURCE_GLOBAL")\n'
                "DECLARE_EXTERNAL_RESOURCE(MINT sbr:735)\nENDIF()\nSRCS(main.cpp)\nEND()\n"
            ),
            "resource/main.cpp": "int value;\n",
        }, "resource")
        resource = lib.node_by_output(graph, "$(B)/resources/MINT")
        self.assertIn("sbr:735", resource["cmds"][0]["cmd_args"])
        compiled = lib.node_by_output(graph, "$(B)/resource/main.cpp.o")
        self.assertIn(resource["uid"], compiled["deps"])
        self.assertIn("-DPROBE_RESOURCE=$(B)/resources/MINT/include", compiled["cmds"][0]["cmd_args"])

    def test_append_bindings_are_visible_to_later_source_conditions(self):
        graph = lib.make(fixture(
            "CFLAGS(-DPROBE_FIRST=${ABSENT})\n"
            "IF(USER_CFLAGS)\nCFLAGS(-DPROBE_SEEN)\nENDIF()\n"
            "CXXFLAGS(GLOBAL -DPROBE_CXX=${ABSENT})\n"
            "IF(USER_CXXFLAGS_GLOBAL)\nCFLAGS(-DPROBE_CXX_SEEN)\nENDIF()\n"
            "CONLYFLAGS(-DPROBE_C=${ABSENT})\n"
            "IF(USER_CONLYFLAGS)\nCFLAGS(-DPROBE_C_SEEN)\nENDIF()\n"
        ), "cedar")
        self.assertEqual(flags(graph), ["-DPROBE_FIRST=", "-DPROBE_SEEN", "-DPROBE_CXX_SEEN",
                                       "-DPROBE_C_SEEN", "-DPROBE_CXX="])
        self.assertEqual(flags(graph, source="unit.c"),
                         ["-DPROBE_FIRST=", "-DPROBE_SEEN", "-DPROBE_CXX_SEEN",
                          "-DPROBE_C_SEEN", "-DPROBE_C="])

    def test_empty_arguments_and_pending_global_markers_have_no_output(self):
        graph = lib.make(fixture(
            'CFLAGS()\nCXXFLAGS(${ABSENT})\nCONLYFLAGS("" "  ")\n'
            'GLOBAL_CFLAGS()\nCFLAGS(GLOBAL GLOBAL ${ABSENT} "" -DPROBE_ONLY GLOBAL)\n'
        ), "cedar")
        self.assertEqual(flags(graph), ["-DPROBE_ONLY"])
        self.assertEqual(flags(graph, source="unit.c"), ["-DPROBE_ONLY"])

    def test_whole_command_separators_are_protected_in_the_source_binding(self):
        for call in ("&& ;", "${OPERATORS}"):
            with self.subTest(call=call):
                files = fixture(
                    f"CFLAGS({call})\n"
                    'IF(USER_CFLAGS MATCHES "quo:")\nCFLAGS(-DPROBE_PROTECTED)\nENDIF()\n'
                )
                files["build/ymake_conf.py"] = lib.ymake_conf(extra="OPERATORS=&& ;\n")
                self.assertEqual(flags(lib.make(files, "cedar")), ["-DPROBE_PROTECTED"])

    def test_bound_varargs_shadow_the_outer_config_variable(self):
        files = {
            "cedar/ya.make": library(
                'CFLAGS($Flags)\nIF(USER_CFLAGS == "$Flags")\n'
                'RUN_PROGRAM(tools/phase bound OUT phase.cpp)\nENDIF()\n'
            ),
            "build/ymake_conf.py": lib.ymake_conf(extra="Flags=outer\n"),
        }
        lib.tool_program(files, "tools/phase", "renamed-phase-tool")
        graph = lib.make(files, "cedar")
        node = lib.node_by_output(graph, "$(B)/cedar/phase.cpp")
        tool = lib.node_by_output(graph, "$(B)/tools/phase/renamed-phase-tool")
        self.assertEqual(node["cmds"][0]["cmd_args"], [tool["outputs"][0], "bound"])
        self.assertIn(tool["uid"], node["deps"])
        self.assertIn(tool["outputs"][0], node["inputs"])

    def test_macro_body_budget_includes_the_bound_flags_substitution(self):
        for count in (124, 125):
            with self.subTest(count=count):
                files = fixture("CFLAGS(-DPROBE_LIMIT=$STEP0)\n")
                config = "".join(f"STEP{i}=$STEP{i+1}\n" for i in range(count))
                files["build/ymake_conf.py"] = lib.ymake_conf(extra=config + f"STEP{count}=finished\n")
                if count == 124:
                    self.assertEqual(flags(lib.make(files, "cedar")), ["-DPROBE_LIMIT=finished"])
                else:
                    failed = lib.make_process(files, "cedar")
                    self.assertNotEqual(failed.returncode, 0)
                    self.assertIn("Macro subst exceeded the limit 128", failed.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
