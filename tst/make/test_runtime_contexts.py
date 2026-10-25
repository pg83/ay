import json

from tst.make.lib import MakeTest, PROJECT


class RuntimeContextsTest(MakeTest):
    def test_perl_configuration(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macros in (
            "SET(PERL_SDK ubuntu-14)",
            "SET(PERL_SDK ubuntu-16)\nSET(PERL_ROOT /usr)",
            "SET(USE_PERL_SYSTEM no)\nSET(USE_LOCAL_TOOLS yes)\nSET(LOCAL_PERL_LIBS /usr/lib)",
            "SET(USE_PERL_SYSTEM yes)\nSET(PERL_SDK ubuntu-20)",
        ):
            with self.subTest(macros=macros):
                self.write("ya.make", f"LIBRARY()\n{macros}\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_python_in_native_module(self):
        self.write("answer.py", "answer = 42\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for declaration, macros in (("PROGRAM(answer)", "USE_PYTHON3()"), ("DLL(answer)", "USE_PYTHON2()\nENABLE(PYTHON_SQLITE3)"), ("DLL(answer)", "USE_PYTHON3()\nENABLE(PYTHON_SQLITE3)")):
            with self.subTest(declaration=declaration, macros=macros):
                self.write("ya.make", f"{declaration}\n{macros}\nPY_SRCS(answer.py)\nSRCS(answer.cpp)\nEND()\n")
                self.graph()

    def test_yql_python_runtimes(self):
        self.write("answer.py", "answer = 42\n")
        for kind in ("YQL_PYTHON_UDF", "YQL_PYTHON3_UDF"):
            with self.subTest(kind=kind):
                self.write("ya.make", f"{kind}(answer)\nENABLE(PYTHON_SQLITE3)\nPY_SRCS(answer.py)\nEND()\n")
                self.graph()

    def test_yql_abi_conflict(self):
        self.write("ya.make", "LIBRARY()\nYQL_STABLE_ABI_VERSION()\nYQL_CURRENT_ABI_VERSION()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_java_runtime_dependency(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nJAVA_RUNTIME_PEERDIR({PROJECT}/runtime)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("runtime/ya.make", "JAVA_LIBRARY(runtime)\nJDK_VERSION(17)\nJAVA_SRCS(Runtime.java)\nEND()\n")
        self.write("runtime/Runtime.java", "public class Runtime {}\n")
        self.node(self.graph(), "runtime/runtime.jar")

    def test_go_resource_directories(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nALL_RESOURCE_FILES(txt PREFIX /data/ data)\nEND()\n")
        self.write("answer.go", "package answer\n")
        self.write("data/value.txt", "42\n")
        self.assertTrue(any(f"$(S)/{PROJECT}/data/value.txt" in node["inputs"] for node in self.graph()["graph"]))

    def test_static_analyzer_plugin(self):
        self.write("ya.make", f"LIBRARY()\nUSE_SA_PLUGINS(FROM {PROJECT}/plugin NAME E2E_PLUGIN)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("plugin/ya.make", "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(E2E_PLUGIN plugin.json)\nEND()\n")
        self.write("plugin/plugin.json", json.dumps({"by_platform": {"linux-x86_64": {"uri": "sbr:123456789"}}}))
        self.node(self.graph(), "answer.cpp.o")

    def test_resource_yasm_join(self):
        self.write("conftest.py", "answer = 42\n")
        entries = " ".join(f"conftest.py /value/{i:04d}/" + "x" * 300 for i in range(160))
        self.write("ya.make", f"LIBRARY()\nRESOURCE({entries})\nEND()\n")
        self.assertTrue(any(f"$(S)/{PROJECT}/conftest.py" in node["inputs"] for node in self.graph()["graph"]))

    def test_flatbuffers_java_native(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", f"FBS_LIBRARY()\nADDINCL({PROJECT}/schema)\nSRCS(answer.fbs native.cpp)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e; table Answer { value:int; }\n")
        self.write("schema/native.cpp", '#include "answer.fbs.h"\nint answer() { return 42; }\n')
        self.node(self.graph(), "answer.jar")
