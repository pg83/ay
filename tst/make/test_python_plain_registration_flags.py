from tst.make.lib import MakeTest


class PythonPlainRegistrationFlagsTest(MakeTest):
    def test_plain_registration_keeps_native_initialization_names(self):
        self.write("ya.make", f"PY2_PROGRAM(consumer)\nPEERDIR({self.project_name}/lib)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("lib/ya.make", "PY23_LIBRARY()\nUSE_PYTHON2()\nSRC(native.cpp)\nPY_REGISTER(answer)\nEND()\n")
        self.write("lib/native.cpp", 'extern "C" void initanswer() {}\n')
        graph = self.graph()
        compiled = [node for node in graph["graph"] if f"$(S)/{self.project_name}/lib/native.cpp" in node["inputs"] and any(output.endswith(".o") for output in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        args = [arg for command in compiled[0]["cmds"] for arg in command["cmd_args"]]
        self.assertFalse([arg for arg in args if arg.startswith(("-Dinitanswer=", "-DPyInit_answer="))])
