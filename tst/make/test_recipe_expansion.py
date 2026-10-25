"""Recipe metadata follows makefile evaluation before base64 serialization."""
import base64
import unittest

from tst import lib
from tst.make.test_test_macro_values import option_values


def recipe_text(graph, project="suite"):
    node = lib.node_by_output(graph, f"$(B)/{project}/test-results/exectest/meta.json")
    encoded, = option_values(node, "--recipes")
    return base64.b64decode(encoded).decode()


def suite(body):
    return "EXECTEST()\nRUN(echo)\n" + body + "END()\n"


class RecipeExpansionTest(unittest.TestCase):
    def test_absent_included_option_keeps_recipe_order_and_real_dependencies(self):
        files = {
            "suite/ya.make": suite("INCLUDE(${ARCADIA_ROOT}/policy/recipes.inc)\n"),
            "policy/recipes.inc": (
                "DEPENDS(recipes/store recipes/service)\n"
                "DATA(arcadia/assets/config.ini)\n"
                "USE_RECIPE(recipes/store/store -n 20)\n"
                'USE_RECIPE(recipes/service/service "${SERVICE_OPTIONS}")\n'
            ),
            "assets/config.ini": "renamed=yes\n",
        }
        for name in ("store", "service"):
            lib.tool_program(files, f"recipes/{name}", name)
        graph = lib.make(files, "suite", "-t")
        self.assertEqual(recipe_text(graph),
                         "recipes/store/store -n 20 \n recipes/service/service \n")
        node = lib.node_by_output(graph, "$(B)/suite/test-results/exectest/meta.json")
        for name in ("store", "service"):
            producer = lib.node_by_output(graph, f"$(B)/recipes/{name}/{name}")
            self.assertIn(producer["uid"], node["deps"])
            compiled = lib.node_by_output(graph, f"$(B)/recipes/{name}/main.cpp.o")
            self.assertIn(compiled["uid"], producer["deps"])
            self.assertIn(f"$(S)/recipes/{name}/main.cpp", compiled["inputs"])
        self.assertEqual(node["inputs"], ["$(S)/assets/config.ini", "$(S)/suite"])
        self.assertIn(node["uid"], graph["result"])
        args = node["cmds"][0]["cmd_args"]
        commands, = [arg for arg in args if arg.startswith("commands=")]
        self.assertEqual(base64.b64decode(commands.partition("=")[2]), b"\necho")
        self.assertEqual(node["outputs"][0], "$(B)/suite/test-results/exectest/meta.json")

    def test_collection_and_final_substitution_have_distinct_timing(self):
        files = {"suite/ya.make": suite(
            "SET(CHOICE before)\n"
            "USE_RECIPE(recipe/run ${CHOICE} $CHOICE ${LATE} $LATE)\n"
            "SET(CHOICE after)\nSET(LATE tail words)\n"
        )}
        self.assertEqual(recipe_text(lib.make(files, "suite", "-t")),
                         "recipe/run before before tail words \n")

    def test_shared_lexical_include_uses_each_calling_context(self):
        files = {"policy/shared.inc": "USE_RECIPE(recipe/run ${OPTIONS} $LATE)\n"}
        for project, options in (("cedar", "--one"), ("maple", "--two extra")):
            files[f"{project}/ya.make"] = suite(
                f"SET(OPTIONS {options})\n"
                "INCLUDE(${ARCADIA_ROOT}/policy/shared.inc)\n"
                f"SET(OPTIONS ignored)\nSET(LATE {project}/final value)\n"
            )
        for targets in (("cedar", "maple"), ("maple", "cedar")):
            graph = lib.make(files, *targets, "-t")
            for project, options in (("cedar", "--one"), ("maple", "--two extra")):
                self.assertEqual(recipe_text(graph, project),
                                 f"recipe/run {options} {project}/final value \n")

    def test_literal_runtime_and_escaped_references_follow_makefile_syntax(self):
        files = {"suite/ya.make": suite(
            "SET(VALUE first second)\n"
            r'USE_RECIPE(recipe/run ${MISSING} $MISSING ${VALUE} $VALUE "${VALUE}" "$VALUE" '
            r'\${VALUE} \\${VALUE} ${"$"}HOME $SHELL ${SHELL} "$(RUNTIME)" '
            r'${TOOL_RESOURCE_GLOBAL} $TOOL_RESOURCE_GLOBAL "literal\path")' + "\n"
            "SET(VALUE changed)\n"
        )}
        self.assertEqual(recipe_text(lib.make(files, "suite", "-t")),
                         'recipe/run $MISSING ' + 'first second ' * 6 +
                         '${"$"}HOME $SHELL $(RUNTIME) ${TOOL_RESOURCE_GLOBAL} '
                         '$TOOL_RESOURCE_GLOBAL literalpath \n')

    def test_empty_arguments_and_expanded_spacing_follow_macro_append(self):
        files = {"suite/ya.make": suite(
            'USE_RECIPE()\nUSE_RECIPE(recipe/run "" "literal   words" ${WORDS} $LATE)\n'
            'SET(LATE "  final   words  ")\nUSE_RECIPE(${ABSENT})\n'
        )}
        self.assertEqual(recipe_text(lib.make(files, "suite", "-t", '-DWORDS= one   "two words"  ')),
                         '\n recipe/run literal words one "two words"  final   words   \n  \n')

    def test_escape_depth_controls_final_binding_and_raw_strings_stay_literal(self):
        files = {"suite/ya.make": suite(
            "SET(VALUE before)\nSET(DASH-NAME yes)\n" +
            r'USE_RECIPE(recipe/run \\\${VALUE} \\\$VALUE ${"$VALUE"} $DASH-NAME '
            r'${DASH-NAME} $0 $NAME_suffix $UNBOUND.suffix $ENV{RECIPE_OPTION})' + "\n"
            "SET(VALUE after)\n"
        )}
        self.assertEqual(recipe_text(lib.make(files, "suite", "-t", env={"RECIPE_OPTION": "from environment"})),
                         'recipe/run after after ${"$VALUE"} yes yes $0 $NAME_suffix '
                         '$UNBOUND.suffix from environment \n')

    def test_recursive_variables_expand_deeply_and_nonterminating_values_fail(self):
        files = {"suite/ya.make": suite("USE_RECIPE(recipe/run $STEP0)\n")}
        flags = [f"-DSTEP{i}=$STEP{i + 1}" for i in range(12)] + ["-DSTEP12=finished"]
        self.assertEqual(recipe_text(lib.make(files, "suite", "-t", *flags)), "recipe/run finished \n")
        failed = lib.make_process(files, "suite", "-t", "-DSTEP0=$STEP0/again")
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("Macro subst exceeded the limit 128", failed.stderr)

    def test_defined_empty_variable_can_make_the_whole_recipe_empty(self):
        files = {"suite/ya.make": suite("SET(EMPTY)\nUSE_RECIPE($EMPTY)\n")}
        self.assertEqual(recipe_text(lib.make(files, "suite", "-t")), "\n")

    def test_whole_separators_are_escaped_before_macro_append(self):
        for separators, flags in (("&& ;", []), ("${OPERATORS}", ["-DOPERATORS=&& ;"])):
            files = {"suite/ya.make": suite(
                'USE_RECIPE(recipe/run ' + separators + ' "x && y" "x ; y")\n'
            )}
            self.assertEqual(recipe_text(lib.make(files, "suite", "-t", *flags)),
                             "recipe/run   x && y x ; y \n")


if __name__ == "__main__":
    unittest.main(verbosity=2)
