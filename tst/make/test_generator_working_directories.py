from tst.make.lib import MakeTest, PROJECT


class GeneratorWorkingDirectoriesTest(MakeTest):
    def test_python_generators_preserve_relative_working_directories(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py STDOUT generated.cpp CWD work)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("work/.keep", "")
        generated = self.node(self.graph(), "generated.cpp")
        self.assertTrue(any(command.get("cwd", "").endswith("work") for command in generated["cmds"]))

    def test_tool_generators_preserve_relative_working_directories(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/tool OUT generated.cpp CWD work)\nEND()\n")
        self.write("tool/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("tool/main.py", 'from pathlib import Path\nPath("generated.cpp").write_text("int answer() { return 42; }")\n')
        self.write("work/.keep", "")
        generated = self.node(self.graph(), "generated.cpp")
        self.assertTrue(any(command.get("cwd", "").endswith("work") for command in generated["cmds"]))
