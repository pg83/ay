from tst.make.lib import MakeTest, PROJECT


class SwigGeneratedHeaderInputsTest(MakeTest):
    def test_java_swig_archive_depends_on_generated_headers(self):
        self.write("ya.make", f"DLL_JAVA(answer)\nJDK_VERSION(17)\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nADDINCL(FOR swig ${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCOPY_FILE(template.h generated.h)\nSRCS(answer.swg)\nEND()\n")
        self.write("template.h", "int answer();\n")
        self.write("answer.swg", '%module answer\n%include "generated.h"\n%{\n#include "generated.h"\n%}\n')
        graph = self.graph()
        header = self.node(graph, "generated.h")
        archive = self.node(graph, "answer.swg.jsrc")
        self.assertIn(f"$(B)/{PROJECT}/generated.h", archive["inputs"])
        self.assertIn(header["uid"], archive["deps"])
