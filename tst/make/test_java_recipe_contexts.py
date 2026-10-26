from tst.make.lib import MakeTest, PROJECT


class JavaRecipeContextsTest(MakeTest):
    def test_version_selected_recipe_include(self):
        self.write("ya.make", "JUNIT5(answer)\nJDK_VERSION(17)\nSIZE(MEDIUM)\nDEFAULT(RECIPE_VERSION 15.3.0)\nINCLUDE(recipe.inc)\nJAVA_SRCS(AnswerTest.java)\nEND()\n")
        self.write("AnswerTest.java", "public class AnswerTest {}\n")
        self.write("recipe.inc", "DEFAULT(RECIPE_SUFFIX 15)\nIF(DEFINED RECIPE_VERSION)\nIF(RECIPE_VERSION VERSION_GE 17.0.0)\nSET(RECIPE_SUFFIX 17)\nELSEIF(RECIPE_VERSION VERSION_GE 16.0.0)\nSET(RECIPE_SUFFIX 16)\nELSEIF(RECIPE_VERSION VERSION_GE 15.0.0)\nSET(RECIPE_SUFFIX 15)\nELSE()\nSET(RECIPE_SUFFIX 14)\nENDIF()\nENDIF()\nINCLUDE(recipe-${RECIPE_SUFFIX}.inc)\n")
        self.write("recipe-15.inc", f"BUILD_ONLY_IF(WARNING OS_LINUX OS_DARWIN ARCH_X86_64)\nREQUIREMENTS(ram_disk:4)\nDATA(sbr://123456789=_testsuite)\nDEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe --version 15)\n")
        self.write("recipe/ya.make", "PY3_PROGRAM(recipe)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("recipe/main.py", "pass\n")
        graph = self.graph("-tt")
        runners = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"] if "--recipes" in command["cmd_args"]]
        self.assertEqual(len(runners), 1)
        self.assertIn("--recipes", runners[0])
        self.assertIn("--requires-ram-disk", runners[0])
        self.assertIn("123456789=_testsuite", runners[0])
