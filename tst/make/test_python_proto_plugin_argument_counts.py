from tst.make.lib import MakeTest


class PythonProtoPluginArgumentCountsTest(MakeTest):
    def test_python_proto_plugin_declarations_validate_positional_arguments(self):
        for declaration in ("PY_PROTO_PLUGIN(plugin tool)", "PY_PROTO_PLUGIN2(plugin .py tool)"):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"PY3_LIBRARY()\n{declaration}\nPY_SRCS(answer.py)\nEND()\n")
                self.write("answer.py", "ANSWER = 42\n")
                result = self.make()
                self.assertNotEqual(result.returncode, 0)
