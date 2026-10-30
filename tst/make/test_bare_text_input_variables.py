from tst.make.lib import MakeTest


class BareTextInputVariablesTest(MakeTest):
    def test_appending_an_unbraced_variable_keeps_declared_files_opaque(self):
        self.write("ya.make", "LIBRARY()\nDECLARE_IN_DIRS(DATA *.h DIRS data)\nSET(INPUTS)\nSET_APPEND(INPUTS $DATA_FILES)\nRUN_PYTHON3(generate.py IN $INPUTS STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("data/value.h", '#include "../unparsed.h"\n')
        self.write("unparsed.h", "#define UNPARSED 1\n")
        node = self.node(self.graph(), "generated.cpp")
        self.assertIn(f"$(S)/{self.project_name}/data/value.h", node["inputs"])
        self.assertNotIn(f"$(S)/{self.project_name}/unparsed.h", node["inputs"])
