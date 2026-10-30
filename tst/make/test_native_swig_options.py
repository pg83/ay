from tst.make.lib import MakeTest


class NativeSwigOptionsTest(MakeTest):
    def source(self):
        self.write("answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')

    def test_named_libraries_use_the_declared_swig_module_name(self):
        self.source()
        self.write("ya.make", "LIBRARY(named)\nSRCS(answer.swg)\nEND()\n")
        generated = self.node(self.graph(), "answer_wrap.swg.cpp")
        self.assertTrue(any(path.endswith("/named.py") for path in generated["outputs"]))
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("-module") + 1], "named")

    def test_dlls_use_the_declared_swig_interface_name(self):
        self.source()
        self.write("ya.make", "DLL(named)\nSRCS(answer.swg)\nEND()\n")
        generated = self.node(self.graph(), "answer_wrap.swg.cpp")
        self.assertTrue(any(path.endswith("/named.py") for path in generated["outputs"]))
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("-interface") + 1], "libnamed")

    def test_programs_use_the_program_swig_interface_name(self):
        self.source()
        self.write("ya.make", "PROGRAM(named)\nSRCS(answer.swg main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        generated = self.node(self.graph(), "answer_wrap.swg.cpp")
        self.assertTrue(any(path.endswith("/named.py") for path in generated["outputs"]))
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("-interface") + 1], "named")

    def test_perl_sources_generate_perl_modules(self):
        self.source()
        self.write("ya.make", "LIBRARY(named)\nSET(SWIG_LANG perl)\nSRCS(answer.swg)\nEND()\n")
        generated = self.node(self.graph(), "answer_wrap.swg.cpp")
        self.assertTrue(any(path.endswith("/named.pm") for path in generated["outputs"]))
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-perl", args)

    def test_an_explicitly_empty_swig_language_is_rejected(self):
        self.source()
        self.write("ya.make", "LIBRARY()\nSET(SWIG_LANG)\nSRCS(answer.swg)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
