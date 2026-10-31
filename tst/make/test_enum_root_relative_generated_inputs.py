from tst.make.lib import MakeTest


class EnumRootRelativeGeneratedInputsTest(MakeTest):
    def test_root_relative_generated_headers_keep_their_producer(self):
        project = self.project_name
        self.write("ya.make", "RECURSE(consumer)\n")
        self.write("consumer/ya.make", f"LIBRARY()\nRUN_PYTHON3(generate.py OUT_NOAUTO ${{ARCADIA_BUILD_ROOT}}/{project}/shared.h)\nGENERATE_ENUM_SERIALIZATION({project}/shared.h)\nEND()\n")
        self.write("consumer/generate.py", "print('enum class Answer { Value = 42 };')\n")
        graph = self.graph()
        generated = self.node(graph, "shared.h")
        serialized = self.node(graph, f"consumer/{project}/shared.h_serialized.cpp")
        self.assertIn(generated["uid"], serialized["deps"])
        self.assertIn(f"$(B)/{project}/shared.h", serialized["inputs"])
