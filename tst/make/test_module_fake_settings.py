import json

from tst.make.lib import MakeTest, PROJECT


class ModuleFakeSettingsTest(MakeTest):
    def fixture(self, setting):
        self.write("ya.make", f"LIBRARY(answer)\n{setting}\nHEADERS(.)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "value.h"\nint answer() { return VALUE; }\n')
        self.write("value.h", "#pragma once\n#define VALUE 42\n")
    def test_fake_modules_have_no_build_roots(self):
        self.fixture("SET(_FAKE_MODULE yes)")
        result = self.make()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["result"], [])

    def test_disabling_fake_sources_keeps_declared_source_headers(self):
        self.fixture("SET(NEED_ADD_FAKE_SRC no)")
        node = self.node(self.graph(), "answer.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/value.h", node["inputs"])
