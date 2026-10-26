import base64

from tst.make.lib import MakeTest, PROJECT


class YqlContextsTest(MakeTest):
    def test_python_udf_main_module_resource_name(self):
        self.write("ya.make", "YQL_PYTHON_UDF(answer)\nREGISTER_YQL_PYTHON_UDF()\nPY_SRCS(__main__.py)\nEND()\n")
        self.write("__main__.py", "def answer(): return 42\n")
        graph = self.graph()
        resources = [node for node in graph["graph"] if f"$(S)/{PROJECT}/__main__.py" in node["inputs"] and any("objcopy_" in path for path in node["outputs"])]
        self.assertEqual(len(resources), 1)
        args = [arg for command in resources[0]["cmds"] for arg in command["cmd_args"]]
        self.assertIn(base64.b64encode(b"/py_modules/__main__").decode(), args)

    def test_conditional_shared_udf_uses_pic_for_its_sources_and_peers(self):
        self.write("ya.make", f"IF (OS_LINUX)\nYQL_UDF(answer)\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/peer)\nEND()\nELSE()\nLIBRARY()\nEND()\nENDIF()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.write("peer/ya.make", "LIBRARY()\nSRCS(peer.cpp)\nEND()\n")
        self.write("peer/peer.cpp", "int peer() { return 42; }\n")
        graph = self.graph()
        self.node(graph, "answer.cpp.pic.o")
        self.node(graph, "peer/peer.cpp.pic.o")
        self.assertIn(f"$(B)/{PROJECT}/answer.cpp.pic.o", self.node(graph, "libanswer.so")["inputs"])

    def test_bundle_selects_shared_udf(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE({PROJECT}/udf NAME bundled)\nRESOURCE(bundled /bundled)\nEND()\n")
        self.write("udf/ya.make", "YQL_UDF(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("udf/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        producer = self.node(graph, "udf/libanswer.so")
        bundle = self.node(graph, "bundled")
        self.assertIn(f"$(B)/{PROJECT}/udf/libanswer.so", bundle["inputs"])
        self.assertIn(producer["uid"], bundle["deps"])

    def test_bundle_accepts_dll_output(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE({PROJECT}/dll NAME bundled)\nRESOURCE(bundled /bundled)\nEND()\n")
        self.write("dll/ya.make", "DLL(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("dll/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        producer = self.node(graph, "dll/libanswer.so")
        bundle = self.node(graph, "bundled")
        self.assertIn(f"$(B)/{PROJECT}/dll/libanswer.so", bundle["inputs"])
        self.assertIn(producer["uid"], bundle["deps"])
