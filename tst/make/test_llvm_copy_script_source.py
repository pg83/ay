from tst.make.lib import MakeTest


class LlvmCopyScriptSourceTest(MakeTest):
    def test_bitcode_merge_keeps_the_copy_script_when_it_is_the_source(self):
        self.write_tree("build/scripts/fs_tools.py", 'extern "C" int answer() { return 42; }\n')
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nSET(LLVM_OPTS -x c++)\nLLVM_BC(${ARCADIA_ROOT}/build/scripts/fs_tools.py NAME answer)\nEND()\n")
        graph = self.graph()
        output = "$(B)/build/scripts/fs_tools.py.bc"
        compiled = next(node for node in graph["graph"] if output in node["outputs"])
        merged = self.node(graph, "answer_merged.bc")
        self.assertIn("$(S)/build/scripts/fs_tools.py", compiled["inputs"])
        self.assertIn(output, merged["inputs"])
        self.assertIn(compiled["uid"], merged["deps"])
