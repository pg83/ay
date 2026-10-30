from tst.make.lib import MakeTest, PROJECT


class GeneratedHeaderCyclesTest(MakeTest):
    def test_contextual_generated_header_cycles_are_rejected(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCOPY_FILE_WITH_CONTEXT(first.in.h first.h)\nCOPY_FILE_WITH_CONTEXT(second.in.h second.h)\nSRCS(answer.cpp)\nEND()\n")
        self.write("first.in.h", '#pragma once\n#include "second.h"\n')
        self.write("second.in.h", '#pragma once\n#include "first.h"\n')
        self.write("answer.cpp", '#include "first.h"\nint answer() { return 42; }\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertRegex(result.stderr, "cycle|loop")
