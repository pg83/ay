import json

from tst.make.lib import MakeTest, PROJECT


class NativeToolContextsTest(MakeTest):
    def test_header_keeps_weak_tool_target(self):
        self.write("ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nSIZE(SMALL)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/native)\nADD_DLLS_TO_JAR()\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("native/ya.make", f"DLL(native)\nPEERDIR({PROJECT}/generated)\nSRCS(native.cpp api.h)\nEND()\n")
        self.write("native/native.cpp", "int answer() { return 42; }\n")
        self.write("native/api.h", f'#include <{PROJECT}/generated/value.h>\n')
        self.write("generated/ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/generator OUT value.h)\nEND()\n")
        self.write("generator/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("generator/main.cpp", "int main() { return 0; }\n")
        result = self.make("-k", "-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes")
        self.assertIn("Tool is not found in a host graph", result.stderr)
        graph = json.loads(result.stdout)
        for node in graph["graph"]:
            for field in ("inputs", "outputs"):
                node[field] = [p.replace("$(BUILD_ROOT)", "$(B)") for p in node.get(field, [])]
        generators = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generator/generator" in node["outputs"]]
        self.assertTrue(generators)
        self.assertTrue(any(f"$(B)/{PROJECT}/generator/main.cpp.o" in node["inputs"] for node in generators))

    def test_header_tool_inherits_pic_context(self):
        self.write("ya.make", f"UNITTEST()\nSRCS(test.cpp)\nDEPENDS({PROJECT}/native)\nEND()\n")
        self.write("test.cpp", "int answer() { return 0; }\n")
        self.write("native/ya.make", f"DLL(native)\nSRCS(native.cpp)\nPEERDIR({PROJECT}/headers)\nEND()\n")
        self.write("native/native.cpp", "int native() { return 42; }\n")
        self.write("headers/ya.make", f"LIBRARY()\nSRCS(api.h unrelated.cpp)\nPEERDIR({PROJECT}/generated)\nEND()\n")
        self.write("headers/unrelated.cpp", "int unrelated() { return 1; }\n")
        self.write("headers/api.h", f'#include <{PROJECT}/generated/value.h>\n')
        self.write("generated/ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/generator OUT value.h)\nEND()\n")
        self.write("generator/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("generator/main.cpp", "int main() { return 0; }\n")
        graph = self.graph("-k", "-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes")
        generator = self.node(graph, "generator/generator")
        self.assertIn(f"$(B)/{PROJECT}/generator/main.cpp.pic.o", generator["inputs"])
