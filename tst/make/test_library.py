from tst.make.lib import MakeTest, PROJECT


class LibraryTest(MakeTest):
    def test_headers_update_between_invocations(self):
        self.write("ya.make", "LIBRARY()\nSRCS(a.cpp)\nEND()\n")
        self.write("a.cpp", '#include "a.h"\nint answer() { return VALUE; }\n')
        self.write("a.h", "#define VALUE 42\n")
        first = self.node(self.graph(), "a.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/a.h", first["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/b.h", first["inputs"])

        self.write("a.h", '#include "b.h"\n')
        self.write("b.h", "#define VALUE 137\n")
        second = self.node(self.graph(), "a.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/a.h", second["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/b.h", second["inputs"])
