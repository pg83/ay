"""RUN_PROGRAM source Deref and command-expression boundaries, with fake data."""
import unittest

import lib


MODULE = "grove/parcel"
OUT = "$(B)/" + MODULE + "/result.bin"
COMMA_CONF = '__COMMA__=${comma:""}\n'


def world(arguments, before="", after="", suffix="", comma_conf=COMMA_CONF):
    files = {
        "build/ymake.core.conf": lib.CONF_FILES["build/ymake.core.conf"] + comma_conf,
        MODULE + "/ya.make": "UNION()\n" + before + "RUN_PROGRAM(tools/forge " + arguments +
            " IN seed.txt IN_NOPARSE opaque.cpp TOOL tools/seal OUT_NOAUTO result.bin " + suffix + ")\n" + after + "END()\n",
        MODULE + "/seed.txt": "seed\n",
        MODULE + "/opaque.cpp": '#include "must-not-open.h"\n',
    }
    lib.tool_program(files, "tools/forge", "forge")
    lib.tool_program(files, "tools/seal", "seal")
    return files


class RunProgramExpressionBindingTest(unittest.TestCase):
    def command(self, files, args, *targets):
        graph = lib.make(files, *(targets or (MODULE,)))
        node = lib.node_by_output(graph, OUT)
        self.assertEqual(node["cmds"][0]["cmd_args"], ["$(B)/tools/forge/forge", *args])
        self.assertEqual(node["outputs"], [OUT])
        self.assertCountEqual(node["inputs"], [
            "$(B)/tools/forge/forge", "$(B)/tools/seal/seal",
            "$(S)/" + MODULE + "/seed.txt", "$(S)/" + MODULE + "/opaque.cpp",
        ])
        for tool in ("forge", "seal"):
            producer = lib.node_by_output(graph, f"$(B)/tools/{tool}/{tool}")
            self.assertIn(producer["uid"], node["deps"])
        self.assertNotIn("$(S)/" + MODULE + "/must-not-open.h", node["inputs"])
        union = lib.node_by_output(graph, "$(B)/" + MODULE + "/grove-parcel.pkg.fake")
        self.assertIn(node["uid"], union["deps"])
        return graph, node

    def test_escaped_fake_json_binds_comma_modifier_then_unescapes_command_terms(self):
        self.command(world(r'--config {\\\"label\\\":\\\"copper\\\"${__COMMA__}\\\"count\\\":7${__COMMA__}\\\"items\\\":[1${__COMMA__}2]}'), [
            "--config", '{"label":"copper","count":7,"items":[1,2]}',
        ])

    def test_braced_values_are_source_captured_and_bare_values_are_final(self):
        self.command(world(
            r'--a $ITEM --b ${ITEM} --c prefix=$ITEM --d prefix=${ITEM} --e "$ITEM" --f "${ITEM}"',
            before="SET(ITEM before)\n", after="SET(ITEM after)\n",
        ), ["--a", "after", "--b", "before", "--c", "prefix=after", "--d", "prefix=before", "--e", "after", "--f", "before"])

    def test_source_and_command_escape_layers_keep_literal_dollars_and_backslashes(self):
        self.command(world(
            r'--a \$ITEM --b \\$ITEM --c \${ITEM} --d \\${ITEM} --e path\\leaf --f path\\\\leaf',
            before="SET(ITEM before)\n", after="SET(ITEM after)\n",
        ), ["--a", "after", "--b", "$ITEM", "--c", "after", "--d", "before", "--e", r"path\leaf", "--f", r"path\\leaf"])

    def test_literal_comma_is_a_formal_argument_separator(self):
        self.command(world('--join=a,b prefix="c,d"'), ["--join=a", "b", "prefix=c d"])

    def test_configuration_initialization_preserves_source_definitions_and_overrides(self):
        for conf, before, expected in (
            (COMMA_CONF, "", "A,B"),
            ("__COMMA__=configured\n", "", "AconfiguredB"),
            (COMMA_CONF, "SET(__COMMA__ module)\n", "AmoduleB"),
            ("", "", "AB"),
        ):
            with self.subTest(conf=conf, before=before):
                self.command(world('A${__COMMA__}B', before=before, comma_conf=conf), [expected])


    def test_no_dollar_and_single_escape_inputs_follow_different_source_passes(self):
        self.command(world(r'--config {\\\"label\\\":\\\"copper\\\",\\\"count\\\":7}'), [
            "--config", r'{\"label\":\"copper\"', r'\"count\":7}',
        ])
        self.command(world(r'--config {\"label\":\"copper\"${__COMMA__}\"count\":7}'), [
            "--config", '{label:copper,count:7}',
        ])

    def test_final_quote_groups_empty_arguments_and_protected_separators(self):
        self.command(world('"two words" \'single words\' prefix="two words" "" --amp=x"&&"y && ;'), [
            "two", "words", "single", "words", "prefix=two words", "", "--amp=x&&y", "&&", ";",
        ])
        self.command(world('empty="" "$MISSING" x""y'), ["empty=", "xy"])
        self.command(world('"&& ;"'), [";"])
        self.command(world('&&&&lead&&&&'), ["lead"])
        self.command(world('"&& &&"'), [])

    def test_final_env_binding_and_runtime_roots_retain_their_command_scope(self):
        _, node = self.command(world(
            '--word ${ITEM} --source ${ARCADIA_ROOT}/fake --build ${ARCADIA_BUILD_ROOT}/fake',
            before="SET(ITEM before)\n", after="SET(ITEM after)\n",
            suffix='CWD ${ARCADIA_ROOT} ENV CAPTURED=${ITEM} LATE=$ITEM',
        ), ["--word", "before", "--source", "$(S)/fake", "--build", "$(B)/fake"])
        self.assertEqual(node["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(node["cmds"][0]["env"]["CAPTURED"], "before")
        self.assertEqual(node["cmds"][0]["env"]["LATE"], "after")

    def test_lists_comma_aliases_and_missing_variables_keep_native_word_boundaries(self):
        self.command(world(
            r'${WORDS} pre${WORDS}post "$WORDS" "${WORDS}" --alias ${ALIAS} --missing ${UNSET} --suffix=x${UNSET}',
            before="SET(WORDS red blue)\nSET(ALIAS ${__COMMA__})\n",
        ), ["red", "blue", "prered", "bluepost", "red", "blue", "red", "blue", "--alias", ",", "--missing", "--suffix=x"])

    def test_shared_lexical_call_binds_each_owner_in_both_root_orders(self):
        files = world("")
        files["policy/call.inc"] = (
            r'RUN_PROGRAM(tools/forge --config {\\\"color\\\":\\\"${COLOR}\\\"${__COMMA__}\\\"value\\\":$VALUE} '
            'OUT_NOAUTO record.bin)\n'
        )
        for module, color, value in (("grove/amber", "yellow", "3"), ("grove/slate", "grey", "5")):
            files[module + "/ya.make"] = (
                f"UNION()\nSET(COLOR {color})\nINCLUDE(${{ARCADIA_ROOT}}/policy/call.inc)\n"
                f"SET(COLOR changed)\nSET(VALUE {value})\nEND()\n"
            )
        for targets in (("grove/amber", "grove/slate"), ("grove/slate", "grove/amber")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for module, color, value in (("grove/amber", "yellow", "3"), ("grove/slate", "grey", "5")):
                    node = lib.node_by_output(graph, "$(B)/" + module + "/record.bin")
                    self.assertEqual(node["cmds"][0]["cmd_args"], [
                        "$(B)/tools/forge/forge", "--config", '{"color":"' + color + '","value":' + value + '}',
                    ])
                    self.assertEqual(node["inputs"], ["$(B)/tools/forge/forge"])
                    self.assertIn(lib.node_by_output(graph, "$(B)/tools/forge/forge")["uid"], node["deps"])

    def test_malformed_command_terms_are_rejected_instead_of_serialized_as_literals(self):
        for arguments in ('--bad x&&y', '--bad &'):
            with self.subTest(arguments=arguments):
                result = lib.make_process(world(arguments), MODULE)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("command separator", result.stderr)
                self.assertNotIn("panic:", result.stderr)


    def test_late_macro_preparation_splits_words_and_commas_before_formal_binding(self):
        self.command(world('--value $ITEM', before="SET(ITEM before)\n", after="SET(ITEM one,two)\n"), [
            "--value", "one", "two",
        ])
        self.command(world('--prefix=$WORDS --bare $WORDS', before="SET(WORDS red blue)\n"), [
            "--prefix=red", "blue", "--bare", "red", "blue",
        ])

    def test_configuration_command_dependencies_preserve_raw_binding_times(self):
        for conf, after, expected in (
            ('PUNCT=${comma:""}\n__COMMA__=${PUNCT}\n', "", ["A,B"]),
            ('PUNCT=${comma:""}\n__COMMA__=${PUNCT}${PUNCT}\n', "", ["A,,B"]),
            ('__COMMA__=${UNSET_PUNCT}\n', "", ["AB"]),
            ('PUNCT=${comma:""}\n__COMMA__=${PUNCT}\n', "SET(PUNCT changed)\n", ["AchangedB"]),
            ('__COMMA__=,\n', "", ["A", "B"]),
        ):
            with self.subTest(conf=conf, after=after):
                self.command(world('A${__COMMA__}B', after=after, comma_conf=conf), expected)

    def test_late_tool_and_captured_output_keep_actual_producer_identities(self):
        files = world('--chosen')
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace(
            "RUN_PROGRAM(tools/forge", "RUN_PROGRAM($SELECTED",
        ).replace("END()", "SET(SELECTED tools/seal)\nEND()")
        graph = lib.make(files, MODULE)
        node = lib.node_by_output(graph, OUT)
        tool = lib.node_by_output(graph, "$(B)/tools/seal/seal")
        self.assertEqual(node["cmds"][0]["cmd_args"], [tool["outputs"][0], "--chosen"])
        self.assertEqual(node["inputs"], [tool["outputs"][0], "$(S)/" + MODULE + "/seed.txt", "$(S)/" + MODULE + "/opaque.cpp"])
        self.assertTrue(node["foreign_deps"]["tool"])
        self.assertTrue(all(uid == tool["uid"] for uid in node["foreign_deps"]["tool"]))
        self.assertIn(tool["uid"], node["deps"])
        files = world('--output ${FINAL}', before="SET(FINAL result.bin)\n")
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace("OUT_NOAUTO result.bin", "OUT_NOAUTO ${FINAL}")
        self.command(files, ["--output", OUT])

    def test_empty_prepared_call_keeps_the_missing_tool_error(self):
        files = world("")
        files[MODULE + "/ya.make"] = "UNION()\nRUN_PROGRAM($TOOL)\nSET(TOOL)\nEND()\n"
        result = lib.make_process(files, MODULE)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RUN_PROGRAM expects at least 1 argument (tool path)", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_py3_wrapper_uses_the_same_source_and_command_binding(self):
        files = world(r'--config {\\\"label\\\":\\\"copper\\\"${__COMMA__}\\\"count\\\":7}')
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace("RUN_PROGRAM(", "RUN_PY3_PROGRAM(")
        self.command(files, ["--config", '{"label":"copper","count":7}'])


    def test_escaped_whitespace_stays_in_one_lexical_atom_before_source_deref(self):
        self.command(world(
            r'alpha\ words$ITEM --plain omega\ words 12\ words$ITEM --other /omega\ words$ITEM',
            before="SET(ITEM before)\n", after="SET(ITEM after)\n",
        ), ["alpha", "wordsafter", "--plain", "omega words", "12", "wordsafter", "--other", "/omega", "wordsafter"])

    def test_late_keyword_words_remain_ordinary_arguments(self):
        self.command(world('--head $KEYWORD', after="SET(KEYWORD IN untouched.txt)\n"), ["--head", "IN", "untouched.txt"])


    def test_json_value_escapes_survive_without_blanket_backslash_removal(self):
        self.command(world(r'--config {\\\"path\\\":\\\"C:\\\\\\\\tmp\\\"${__COMMA__}\\\"quoted\\\":\\\"a\\\\\\\"b\\\"}'), [
            "--config", r'{"path":"C:\\tmp","quoted":"a\"b"}',
        ])

    def test_stdout_secondary_output_keeps_registry_main_and_source_obligations(self):
        files = world(r'--config {\\\"count\\\":7${__COMMA__}\\\"ok\\\":true}')
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace(
            "UNION()", "LIBRARY(parcel)\n" + lib.BARE_MODULE,
        ).replace("OUT_NOAUTO result.bin", "OUT_NOAUTO result.bin secondary.h STDOUT_NOAUTO trace.txt OUTPUT_INCLUDES grove/parcel/runtime.h").replace(
            "END()", "SRCS(use.cpp)\nEND()",
        )
        files[MODULE + "/use.cpp"] = '#include "grove/parcel/secondary.h"\n'
        files[MODULE + "/runtime.h"] = ""
        graph = lib.make(files, MODULE)
        producer = lib.node_by_output(graph, OUT)
        self.assertEqual(producer["outputs"], ["$(B)/" + MODULE + "/trace.txt", OUT, "$(B)/" + MODULE + "/secondary.h"])
        self.assertEqual(producer["cmds"][0]["stdout"], producer["outputs"][0])
        self.assertEqual(producer["cmds"][0]["cmd_args"], ["$(B)/tools/forge/forge", "--config", '{"count":7,"ok":true}'])
        self.assertCountEqual(producer["inputs"], ["$(B)/tools/forge/forge", "$(B)/tools/seal/seal", "$(S)/" + MODULE + "/seed.txt", "$(S)/" + MODULE + "/opaque.cpp"])
        compiled = lib.node_by_output(graph, "$(B)/" + MODULE + "/use.cpp.o")
        self.assertIn(producer["uid"], compiled["deps"])
        for path in (producer["outputs"][0], producer["outputs"][2], "$(S)/" + MODULE + "/runtime.h"):
            self.assertIn(path, compiled["inputs"])
        archive = lib.node_by_output(graph, "$(B)/" + MODULE + "/libparcel.a")
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertIn(compiled["outputs"][0], archive["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
