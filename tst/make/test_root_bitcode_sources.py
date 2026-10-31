from tst.make.lib import MakeTest


class RootBitcodeSourcesTest(MakeTest):
    def test_source_tree_root_bitcode_compilation_keeps_its_source(self):
        self.write_tree("ya.make", "LIBRARY(root)\nUSE_LLVM_BC16()\nLLVM_BC(__ay_root_e2e__.cpp NAME answer)\nEND()\n")
        self.write_tree("__ay_root_e2e__.cpp", 'extern "C" int answer() { return 42; }\n')
        self.project_name = "."
        compiled = [node for node in self.graph()["graph"] if "$(S)/__ay_root_e2e__.cpp" in node["inputs"] and any(output.endswith(".bc") for output in node["outputs"])]
        self.assertEqual(len(compiled), 1)

    def test_source_tree_root_bitcode_compilation_keeps_its_ragel_producer(self):
        self.write_tree("ya.make", "LIBRARY(root)\nUSE_LLVM_BC16()\nBUILDWITH_RAGEL6(__ay_root_e2e__.rl)\nLLVM_BC(__ay_root_e2e__.rl6.cpp NAME answer)\nEND()\n")
        self.write_tree("__ay_root_e2e__.rl", "%%{ machine answer; main := any*; }%%\n%% write data;\n")
        self.project_name = "."
        graph = self.graph()
        generated = [node for node in graph["graph"] if any(output.endswith("/__ay_root_e2e__.rl6.cpp") for output in node["outputs"])]
        self.assertEqual(len(generated), 1)
        source = next(output for output in generated[0]["outputs"] if output.endswith("/__ay_root_e2e__.rl6.cpp"))
        compiled = [node for node in graph["graph"] if source in node["inputs"] and any(output.endswith(".bc") for output in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        self.assertIn(generated[0]["uid"], compiled[0]["deps"])
