from tst.make.lib import MakeTest, PROJECT


class GeneratedHeaderCyclesTest(MakeTest):
    def fixture(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCOPY_FILE_WITH_CONTEXT(first.in.h first.h)\nCOPY_FILE_WITH_CONTEXT(second.in.h second.h)\nSRCS(answer.cpp)\nEND()\n")
        self.write("first.in.h", '#pragma once\n#include "second.h"\n')
        self.write("second.in.h", '#pragma once\n#include "first.h"\n')
        self.write("answer.cpp", '#include "first.h"\nint answer() { return 42; }\n')

    def test_contextual_generated_header_cycles_are_rejected(self):
        self.fixture()
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertRegex(result.stderr, "cycle|loop")

    def test_header_cycles_are_rejected_with_pending_node_debugging(self):
        self.fixture()
        result = self.make(env={"AY_DEBUG_PENDING": "1"})
        self.assertNotEqual(result.returncode, 0)
        self.assertRegex(result.stderr, "cycle|loop")
