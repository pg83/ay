import re

from tst.make.lib import MakeTest


class PythonExtensionToolConfigurationTest(MakeTest):
    def test_cross_python_extensions_require_the_configured_interpreter_peer(self):
        configuration = "build/conf/python.conf"
        original = (self.root / configuration).read_text()
        stripped, count = re.subn(r"(?m)^[ \t]*PYTHON3_TOOL_PEERDIR=.*\n", "", original)
        self.assertEqual(count, 2)
        self.write_tree(configuration, stripped)
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for previous in ("no", "yes"):
            with self.subTest(previous=previous):
                self.write("ya.make", "PY3MODULE(answer)\nSET(IS_CROSS_TOOLS yes)\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make("-DUSE_PYTHON3_PREV=" + previous).returncode, 0)
