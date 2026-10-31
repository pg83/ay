from tst.make.lib import MakeTest, PROJECT


class DllForeignSourceContextsTest(MakeTest):
    def test_dll_preserves_sources_with_exported_cython_include_directories(self):
        self.write("ya.make", f"DLL(answer)\nUSE_PYTHON3()\nADDINCL(GLOBAL FOR cython {PROJECT}/include)\nPY_SRCS(helper.py)\nSRCS(value.cpp prebuilt.o)\nEND()\n")
        self.write("include/value.pxd", "cdef int value\n")
        self.write("helper.py", "VALUE = 42\n")
        self.write("value.cpp", 'extern "C" int value() { return 42; }\n')
        self.write("prebuilt.o", "")
        graph = self.graph()
        native = self.node(graph, "libanswer.so")
        self.assertIn(f"$(S)/{PROJECT}/prebuilt.o", native["inputs"])
        self.assertIn(self.node(graph, "value.cpp.pic.o")["uid"], native["deps"])
        self.assertTrue(any(f"$(S)/{PROJECT}/helper.py" in node["inputs"] for node in graph["graph"]))
