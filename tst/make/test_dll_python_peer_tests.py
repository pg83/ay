from tst.make.lib import MakeTest, PROJECT


class DllPythonPeerTestsTest(MakeTest):
    def test_peer_test_generation_rejects_native_dlls_as_python_program_peers(self):
        self.write("ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("native/ya.make", "DLL(answer)\nUSE_PYTHON3()\nPY_SRCS(helper.py)\nSRCS(value.cpp)\nEND()\n")
        self.write("native/helper.py", "VALUE = 42\n")
        self.write("native/value.cpp", 'extern "C" int value() { return 42; }\n')
        result = self.make("-t", "-DADD_PEERDIRS_GEN_TESTS=yes", "--add-modules-to-results")
        self.assertNotEqual(result.returncode, 0)
