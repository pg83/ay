from tst.make.lib import MakeTest, PROJECT


class IncludeHashCpuFallbackTest(MakeTest):
    def test_disabling_avx512_keeps_large_header_closures_and_uids(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        headers = [f"header{index}.h" for index in range(33)]
        self.write("answer.cpp", "".join(f'#include "{name}"\n' for name in headers) + "int answer() { return 42; }\n")
        for name in headers:
            self.write(name, "#pragma once\n")
        ordinary = self.node(self.graph(), "answer.cpp.o")
        fallback = self.node(self.graph(env={"AY_CHAOS": "cpu-avx512=0"}), "answer.cpp.o")
        self.assertEqual(set(ordinary["inputs"]), set(fallback["inputs"]))
        self.assertEqual(ordinary["uid"], fallback["uid"])
        for name in headers:
            self.assertIn(f"$(S)/{PROJECT}/{name}", fallback["inputs"])
