from tst.make.lib import MakeTest, PROJECT


class LateIncludeInputsTest(MakeTest):
    def test_peer_generated_header_sources_are_not_link_inputs(self):
        self.write('ya.make', f'PROGRAM(answer)\nPEERDIR({PROJECT}/lib)\nSRCS(main.cpp)\nEND()\n')
        self.write('main.cpp', 'int main() { return 0; }\n')
        self.write('lib/ya.make', 'LIBRARY()\nCOPY_FILE_WITH_CONTEXT(template.h generated.h)\nSRCS(public.h ordinary.cpp)\nEND()\n')
        self.write('lib/public.h', f'#include <{PROJECT}/lib/generated.h>\n')
        self.write('lib/template.h', '#include "leaf.h"\n')
        self.write('lib/leaf.h', 'int leaf;\n')
        self.write('lib/ordinary.cpp', 'int ordinary;\n')
        graph = self.graph()
        linked = self.node(graph, 'answer')
        for name in ('public.h', 'template.h', 'leaf.h'):
            self.assertNotIn(f'$(S)/{PROJECT}/lib/{name}', linked['inputs'])

    def test_shared_bison_header_after_consumers(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nSRCS(first.cpp second.cpp parser.y)\nEND()\n")
        self.write("parser.y", "%token NUMBER\n%%\nexpression: NUMBER;\n%%\n")
        self.write("shared.h", '#include "parser.h"\n')
        for name in ("first", "second"):
            self.write(name + ".cpp", '#include "shared.h"\n')
        graph = self.graph()
        producer = self.node(graph, "parser.h")
        for name in ("first", "second"):
            consumer = self.node(graph, name + ".cpp.o")
            self.assertIn(f"$(B)/{PROJECT}/parser.h", consumer["inputs"])
            self.assertIn(producer["uid"], consumer["deps"])

    def test_peer_header_imports_without_cpp_consumer(self):
        self.write("ya.make", f"PROGRAM(answer)\nPEERDIR({PROJECT}/lib)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/deps)\nSRCS(seed.proto public.h ordinary.cpp)\nEND()\n")
        self.write("lib/public.h", f'#include <{PROJECT}/lib/seed.pb.h>\n')
        self.write("lib/seed.proto", f'syntax = "proto2";\nimport "{PROJECT}/deps/leaf.proto";\nmessage Seed {{}}\n')
        self.write("lib/ordinary.cpp", "int ordinary;\n")
        self.write("deps/ya.make", "LIBRARY()\nSRCS(leaf.proto)\nEND()\n")
        self.write("deps/leaf.proto", 'syntax = "proto2";\nmessage Leaf {}\n')
        graph = self.graph()
        for output in ("main.cpp.o", "lib/ordinary.cpp.o"):
            self.assertFalse(any(p.endswith(".pb.h") for p in self.node(graph, output)["inputs"]))

    def test_copy_with_context_source_cycle(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCOPY_FILE_WITH_CONTEXT(first.h first.generated.h)\nSRCS(main.cpp)\nEND()\n")
        self.write("first.h", '#include "second.h"\n')
        self.write("second.h", '#include "first.h"\n')
        self.write("main.cpp", '#include "first.generated.h"\n')
        graph = self.graph()
        for output in ("first.generated.h", "main.cpp.o"):
            inputs = self.node(graph, output)["inputs"]
            for source in ("first.h", "second.h"):
                self.assertIn(f"$(S)/{PROJECT}/{source}", inputs)
