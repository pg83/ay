from tst.make.lib import MakeTest, PROJECT


class MoreModulesTest(MakeTest):
    def test_dll(self):
        self.write("ya.make", "DLL(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        self.assertIn(self.node(graph, "libanswer.so")["uid"], graph["result"])

    def test_java_program_with_peer(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAR_MAIN_CLASS(e2e.Main)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("Main.java", "package e2e; public class Main { public static void main(String[] args) { System.out.println(Value.answer()); } }\n")
        self.write("lib/ya.make", "JAVA_LIBRARY(value)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
        self.write("lib/Value.java", "package e2e; public class Value { public static int answer() { return 42; } }\n")
        graph = self.graph()
        self.node(graph, "lib/value.jar")
        self.node(graph, "answer.jar")
        self.assertTrue(any(path.endswith(".jar") for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))

    def test_python_program_entrypoint(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nRESOURCE_FILES(PREFIX data/ value.txt)\nEND()\n")
        self.write("main.py", 'if __name__ == "__main__":\n    print(42)\n')
        self.write("value.txt", "42\n")
        graph = self.graph()
        self.assertIn(self.node(graph, "answer")["uid"], graph["result"])

    def test_docs_library(self):
        self.write("ya.make", "DOCS_LIBRARY(guide)\nSRCS(**/*.md EXCLUDE private/**)\nEND()\n")
        self.write("index.md", "# Guide\n")
        self.write("chapter/intro.md", "# Introduction\n")
        self.write("private/draft.md", "# Draft\n")
        graph = self.graph()
        inputs = self.node(graph, "guide.docslib")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/chapter/intro.md", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/private/draft.md", inputs)

    def test_docs_book_with_library(self):
        self.write("ya.make", f"DOCS(guide)\nPEERDIR({PROJECT}/shared)\nDOCS_VARS(answer=42)\nEND()\n")
        self.write(".yfm", "title: Guide\n")
        self.write("toc.yaml", "title: Guide\nitems:\n  - name: Introduction\n    href: index.md\n")
        self.write("index.md", "# Guide\nThe answer is {{ answer }}.\n")
        self.write("shared/ya.make", "DOCS_LIBRARY(shared)\nEND()\n")
        self.write("shared/other.md", "# Shared text\n")
        graph = self.graph()
        self.node(graph, "shared/shared.docslib")
        self.assertIn(self.node(graph, "guide.tar.gz")["uid"], graph["result"])

    def test_llvm_bitcode_resource(self):
        self.write("ya.make", "LIBRARY()\nLLVM_BC(answer.c NAME answer SYMBOLS answer)\nEND()\n")
        self.write("answer.c", "int answer(void) { return 42; }\n")
        graph = self.graph()
        producer = self.node(graph, "answer.c.bc")
        self.assertIn(f"$(S)/{PROJECT}/answer.c", producer["inputs"])
        self.node(graph, "answer_optimized.bc")

    def test_llvm_unnamed_resource(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nLLVM_BC(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.node(graph, "answer.cpp.bc")
        self.node(graph, "_optimized.bc")

    def test_llvm_resource_in_program(self):
        self.write("ya.make", "PROGRAM(answer)\nUSE_LLVM_BC16()\nLLVM_BC(answer.cpp NAME answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        self.node(graph, "answer.cpp.bc")
        self.node(graph, "answer_optimized.bc")
        self.assertIn(self.node(graph, "answer")["uid"], graph["result"])

    def test_llvm_ir_source(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nLLVM_BC(answer.ll NAME answer)\nEND()\n")
        self.write("answer.ll", "define i32 @answer() { ret i32 42 }\n")
        graph = self.graph()
        producer = self.node(graph, "answer.ll.bc")
        self.assertTrue(producer["cmds"][0]["cmd_args"][0].endswith("/bin/llvm-as"))
        self.assertIn(f"$(S)/{PROJECT}/answer.ll", producer["inputs"])

    def test_llvm_precompiled_source(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nLLVM_BC(answer.ll NAME answer NO_COMPILE)\nCOPY_FILE(prebuilt.bc answer.ll.bc)\nEND()\n")
        self.write("answer.ll", "define i32 @answer() { ret i32 42 }\n")
        self.write("prebuilt.bc", "precompiled bitcode\n")
        graph = self.graph()
        copy = self.node(graph, "answer.ll.bc")
        self.assertIn(f"$(S)/{PROJECT}/prebuilt.bc", copy["inputs"])
        self.assertIn(copy["uid"], self.node(graph, "answer_merged.bc")["deps"])
