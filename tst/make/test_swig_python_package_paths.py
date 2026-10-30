from tst.make.lib import MakeTest


class SwigPythonPackagePathsTest(MakeTest):
    def test_nested_interfaces_use_the_leaf_of_the_python_module_name(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(NAMESPACE e2e SWIG_CPP interfaces/answer.swg=custom.package.answer)\nEND()\n")
        self.write("interfaces/answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')
        node = self.node(self.graph(), "interfaces/answer.swg.cpp")
        self.assertIn(f"$(B)/{self.project_name}/interfaces/answer.py", node["outputs"])
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("-module") + 1], "answer")
        self.assertEqual(args[args.index("-interface") + 1], "answer_swg")
