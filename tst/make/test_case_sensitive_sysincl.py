from tst.make.lib import MakeTest, PROJECT


class CaseSensitiveSysinclTest(MakeTest):
    def fixture(self, options):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nSYSINCL={PROJECT}/sysincl.yml\n")
        self.write("sysincl.yml", '- source_filter: ""\n  case_sensitive: true\n  includes:\n    - e2e_unused.h: []\n')
        self.write("ya.make", f"LIBRARY()\n{options}\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "answer.h"\nint answer() { return VALUE; }\n')
        self.write("answer.h", "#define VALUE 42\n")

    def test_local_headers_resolve_without_case_insensitive_system_mappings(self):
        self.fixture("NO_UTIL()\nNO_RUNTIME()\nSET(NEED_PLATFORM_PEERDIRS no)")
        compiled = self.node(self.graph(), "answer.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/answer.h", compiled["inputs"])

    def test_unmapped_runtime_headers_are_rejected(self):
        self.fixture("")
        self.assertNotEqual(self.make().returncode, 0)
