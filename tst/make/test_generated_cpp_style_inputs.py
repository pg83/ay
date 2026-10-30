from tst.make.lib import MakeTest, PROJECT


class GeneratedCppStyleInputsTest(MakeTest):
    def test_diamond_generator_inputs_create_one_source_style_entry(self):
        self.write("ya.make", f"LIBRARY()\nSTYLE_CPP()\nRUN_PROGRAM({PROJECT}/generator IN value.cpp OUT_NOAUTO left.cpp right.cpp)\nRUN_PROGRAM({PROJECT}/generator IN left.cpp right.cpp OUT generated.cpp)\nEND()\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('int answer() { return 42; }')\n")
        self.write("value.cpp", "int value() { return 42; }\n")
        checked = self.node(self.graph("-t"), "test-results/clang_format/meta.json")
        self.assertIn(f"$(S)/{PROJECT}/value.cpp", checked["inputs"])
        self.assertEqual(checked["inputs"].count(f"$(S)/{PROJECT}/value.cpp"), 1)
