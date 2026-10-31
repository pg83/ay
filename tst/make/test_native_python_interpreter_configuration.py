import re

from tst.make.lib import MakeTest


class NativePythonInterpreterConfigurationTest(MakeTest):
    def test_previous_python_entry_requires_interpreter_peer_configuration(self):
        configuration = "build/conf/python.conf"
        original = (self.root / configuration).read_text()
        stripped, count = re.subn(r"(?m)^[ \t]*PYTHON3_TOOL_PEERDIR=.*\n", "", original)
        self.assertEqual(count, 2)
        self.write_tree(configuration, stripped)
        self.write("ya.make", "PROGRAM(answer)\nUSE_PYTHON3()\nENABLE(PYTHON_SQLITE3)\nPY_SRCS(answer.py)\nSRCS(main.cpp)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        result = self.make("-DUSE_PYTHON3_PREV=yes")
        self.assertNotEqual(result.returncode, 0)
