from tst.make.lib import MakeTest


class SwigModuleNamesTest(MakeTest):
    def test_an_explicit_unqualified_python_module_name_is_preserved(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(SWIG_CPP answer.swg=answer)\nEND()\n")
        self.write("answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')
        node = self.node(self.graph(), "answer.swg.cpp")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("-module") + 1], "answer")

    def test_an_unsupported_native_swig_language_is_rejected(self):
        self.write("ya.make", "LIBRARY()\nSET(SWIG_LANG ruby)\nSRCS(answer.swg)\nEND()\n")
        self.write("answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')
        self.assertNotEqual(self.make().returncode, 0)
