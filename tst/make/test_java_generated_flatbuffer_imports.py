from tst.make.lib import MakeTest, PROJECT


class JavaGeneratedFlatbufferImportsTest(MakeTest):
    def test_generated_flatbuffer_import_keeps_its_producer(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("schema/ya.make", "FBS_LIBRARY()\nCOPY_FILE(base.in.fbs base.fbs)\nSRCS(answer.fbs)\nEND()\n")
        self.write("schema/answer.fbs", f'include "{PROJECT}/schema/base.fbs";\nnamespace e2e;\ntable Answer {{ value:Base; }}\nroot_type Answer;\n')
        self.write("schema/base.in.fbs", "namespace e2e;\ntable Base { value:int; }\n")
        graph = self.graph()
        generated = self.node(graph, "schema/base.fbs")
        java = self.node(graph, "schema/answer.fbs.jsrc")
        self.assertIn(f"$(B)/{PROJECT}/schema/base.fbs", java["inputs"])
        self.assertIn(generated["uid"], java["deps"])
