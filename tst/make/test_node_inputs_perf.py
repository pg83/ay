from tst.make.lib import MakeTest, PROJECT


class NodeInputsPerfTest(MakeTest):
    def test_mixed_roots_and_shared_producer(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nRUN_PYTHON3(generate.py IN seed.txt OUT first.h second.h)\nSRCS(first.cpp second.cpp)\nEND()\n")
        self.write("generate.py", "pass\n")
        self.write("seed.txt", "42\n")
        self.write("source.h", "#pragma once\n")
        self.write("first.cpp", f'#include "source.h"\n#include <{PROJECT}/first.h>\n')
        self.write("second.cpp", f'#include "source.h"\n#include <{PROJECT}/second.h>\n')
        graph = self.graph()
        producer = self.node(graph, "first.h")
        for name, header in (("first", "first.h"), ("second", "second.h")):
            node = self.node(graph, name + ".cpp.o")
            self.assertIn(f"$(S)/{PROJECT}/source.h", node["inputs"])
            self.assertIn(f"$(B)/{PROJECT}/{header}", node["inputs"])
            self.assertIn(producer["uid"], node["deps"])
