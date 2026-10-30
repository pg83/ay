from tst.make.lib import MakeTest


class PythonHeaderOptionsTest(MakeTest):
    def test_python_headers_require_a_nonempty_include_directory_setting(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for previous in ("no", "yes"):
            with self.subTest(previous=previous):
                self.write("ya.make", f"LIBRARY()\nSET(USE_PYTHON3_PREV {previous})\nSET(PY3_BASE_INCLUDE_DIR)\nPYTHON3_ADDINCL()\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_external_python_headers_use_configured_flags(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in ("PYTHON2_ADDINCL", "PYTHON3_ADDINCL"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON)\nSET(PYTHON_INCLUDE -DE2E_EXTERNAL_PYTHON_HEADER=42)\n{macro}()\nSRCS(answer.cpp)\nEND()\n")
                args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
                self.assertIn("-DE2E_EXTERNAL_PYTHON_HEADER=42", args)

    def test_system_python_headers_add_the_platform_dependency(self):
        self.write_tree("build/platform/python/ya.make", "LIBRARY()\nCFLAGS(GLOBAL -DE2E_SYSTEM_PYTHON_HEADER=42)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in ("PYTHON2_ADDINCL", "PYTHON3_ADDINCL"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON yes)\n{macro}()\nSRCS(answer.cpp)\nEND()\n")
                args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
                self.assertIn("-DE2E_SYSTEM_PYTHON_HEADER=42", args)

    def test_native_python_three_can_use_previous_headers(self):
        self.write("ya.make", "LIBRARY()\nSET(USE_PYTHON3_PREV yes)\nPYTHON3_ADDINCL()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DUSE_PYTHON3_PREV", args)

