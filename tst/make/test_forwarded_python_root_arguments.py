from tst.make.lib import MakeTest


class ForwardedPythonRootArgumentsTest(MakeTest):
    def test_forwarded_configuration_values_keep_the_source_root_argument(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py ${E2E_ARG0} STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        flags = [f"-DE2E_ARG{index}=${{E2E_ARG{index + 1}}}" for index in range(7)]
        flags.append("-DE2E_ARG7=${ARCADIA_ROOT}")
        generated = self.node(self.graph(*flags), "generated.cpp")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[-1], "$(S)")
