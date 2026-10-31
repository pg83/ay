from tst.make.lib import MakeTest


class ModuleArchiveIncludePathsTest(MakeTest):
    def test_inactive_archive_includes_preserve_peer_link_dependencies(self):
        project = self.project_name
        self.write("ya.make", f"PROGRAM(consumer)\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{project})\nPEERDIR({project}/peer)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", f'#if 0\n#include "{project}/peer/libanswer.a"\n#include "peer/libanswer.a"\n#include "peer/sub/../libanswer.a"\n#endif\nint main() {{ return 0; }}\n')
        self.write("peer/ya.make", "LIBRARY(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("peer/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        compiled = self.node(graph, "main.cpp.o")
        archive = self.node(graph, "peer/libanswer.a")
        linked = self.node(graph, "consumer")
        self.assertIn(f"$(S)/{project}/main.cpp", compiled["inputs"])
        self.assertIn(archive["uid"], linked["deps"])
