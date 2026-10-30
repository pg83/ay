from tst.make.lib import MakeTest, PROJECT


class LlvmSourcePathsTest(MakeTest):
    def test_generated_bitcode_inputs_resolve_redundant_parent_segments(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nCOPY_FILE(template.cpp generated.cpp)\nLLVM_BC(nested/../generated.cpp NAME answer)\nEND()\n")
        self.write("template.cpp", 'extern "C" int answer() { return 42; }\n')
        self.write("nested/.keep", "")
        graph = self.graph()
        generated = self.node(graph, "generated.cpp")
        compiled = [node for node in graph["graph"]
                    if generated["uid"] in node["deps"]
                    and any(path.endswith(".bc") for path in node["outputs"])]
        self.assertTrue(compiled)
        self.assertTrue(any(f"$(B)/{PROJECT}/generated.cpp" in node["inputs"] for node in compiled))
