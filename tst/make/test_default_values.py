import base64
import unittest

from tst import lib


VALUE_CASES = (
    ("variadic", "", "DEFAULT(STEPS cedar birch maple)\n", "cedar birch maple"),
    ("quoted", "", 'DEFAULT(STEPS "cedar grove" birch "maple hill")\n', "cedar grove birch maple hill"),
    ("existing", "", "SET(STEPS chosen pair)\nDEFAULT(STEPS ignored fallback)\n", "chosen pair"),
    ("empty", "", "SET(STEPS)\nDEFAULT(STEPS ignored fallback)\n", ""),
    ("quoted_empty", "", 'SET(STEPS "")\nDEFAULT(STEPS ignored fallback)\n', ""),
    ("inherited", "SET(STEPS outer pair)\n", "DEFAULT(STEPS ignored fallback)\n", "outer pair"),
    ("inherited_empty", "SET(STEPS)\n", "DEFAULT(STEPS ignored fallback)\n", ""),
    ("no_value", "", "DEFAULT(STEPS)\nDEFAULT(STEPS ignored fallback)\n", ""),
    ("repeated", "", "DEFAULT(STEPS first second)\nDEFAULT(STEPS ignored fallback)\n", "first second"),
    ("expanded", "", "SET(BASE initial pair)\nDEFAULT(STEPS ${BASE} suffix)\nSET(BASE changed later)\n", "initial pair suffix"),
    ("recursive", "", "SET(BASE initial pair)\nSET(ALIAS ${BASE})\nDEFAULT(STEPS ${ALIAS} suffix)\nSET(BASE changed later)\n", "initial pair suffix"),
)


def recipe_files(cases=VALUE_CASES):
    return {
        f"grove/{name}/ya.make": before + "EXECTEST()\nRUN(echo)\n" + body
        + "USE_RECIPE(recipe/run --steps ${STEPS} tail)\nEND()\n"
        for name, before, body, _ in cases
    }


def recipe_text(graph, name):
    run = lib.node_by_output(graph, f"$(B)/grove/{name}/test-results/exectest/meta.json")
    args = run["cmds"][-1]["cmd_args"]
    return base64.b64decode(args[args.index("--recipes") + 1]).decode()


def generated_files():
    files = {
        "grove/parcel/ya.make": "UNION()\nDEFAULT(STEPS cedar birch maple)\n"
        "RUN_PROGRAM(tools/marker ${STEPS} OUT generated.h)\n"
        "COPY_FILE(generated.h installed.h)\nEND()\n",
    }
    lib.tool_program(files, "tools/marker", "marker")
    return files


class DefaultValuesTest(unittest.TestCase):
    def test_all_default_arguments_reach_recipe_in_source_order(self):
        cases = VALUE_CASES[:2] + VALUE_CASES[8:]
        graph = lib.make(recipe_files(cases), "grove/variadic", *[f"grove/{c[0]}" for c in cases[1:]], "-t")
        for name, _, _, value in cases:
            with self.subTest(case=name):
                self.assertEqual(recipe_text(graph, name), "recipe/run --steps " + (value + " " if value else "") + "tail \n")

    def test_existing_and_explicitly_empty_local_and_inherited_values_win(self):
        cases = VALUE_CASES[2:8]
        graph = lib.make(recipe_files(cases), "grove/existing", *[f"grove/{c[0]}" for c in cases[1:]], "-t")
        for name, _, _, value in cases:
            with self.subTest(case=name):
                self.assertEqual(recipe_text(graph, name), "recipe/run --steps " + (value + " " if value else "") + "tail \n")

    def test_shared_include_defaults_are_evaluated_in_each_source_context(self):
        files = {"shared/recipe.inc": "DEFAULT(STEPS ${LABEL} second third)\n"
                 "USE_RECIPE(recipe/run ${STEPS})\nSET_APPEND(STEPS final)\n"
                 "USE_RECIPE(recipe/check ${STEPS})\n"}
        for name in ("pine", "birch"):
            files[f"grove/{name}/ya.make"] = (
                f"EXECTEST()\nRUN(echo)\nSET(LABEL {name})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/recipe.inc)\nEND()\n"
            )
        for order in (("pine", "birch"), ("birch", "pine")):
            with self.subTest(order=order):
                graph = lib.make(files, f"grove/{order[0]}", f"grove/{order[1]}", "-t")
                for name in order:
                    self.assertEqual(recipe_text(graph, name),
                                     f"recipe/run {name} second third \n recipe/check {name} second third final \n")

    def test_default_arguments_reach_real_generated_producer_and_copy_consumer(self):
        graph = lib.make(generated_files(), "grove/parcel")
        generated = lib.node_by_output(graph, "$(B)/grove/parcel/generated.h")
        installed = lib.node_by_output(graph, "$(B)/grove/parcel/installed.h")
        tool = lib.node_by_output(graph, "$(B)/tools/marker/marker")
        self.assertEqual(generated["cmds"][0]["cmd_args"], ["$(B)/tools/marker/marker", "cedar", "birch", "maple"])
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn("$(B)/tools/marker/marker", generated["inputs"])
        self.assertIn(generated["uid"], installed["deps"])
        self.assertIn("$(B)/grove/parcel/generated.h", installed["inputs"])
        package = lib.node_by_output(graph, "$(B)/grove/parcel/grove-parcel.pkg.fake")
        self.assertIn(package["uid"], graph["result"])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
