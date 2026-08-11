import unittest

import lib


class EmitRodataTest(unittest.TestCase):
    def test_rodata_node_shape_and_yasm_dependency(self):
        files = {
            "contrib/libs/icu/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(icudt78_dat.rodata)\nEND()\n"
            ),
            "contrib/libs/icu/icudt78_dat.rodata": "binary data\n",
            "build/scripts/rodata2asm.py": "print('rodata')\n",
        }
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(
            files,
            "contrib/libs/icu",
            "--target-platform", "default-linux-x86_64",
        )
        rodata = lib.only_node_by_kind(graph, "RD")
        self.assertEqual(rodata["outputs"], [
            "$(B)/contrib/libs/icu/icudt78_dat.rodata.asm",
            "$(B)/contrib/libs/icu/icudt78_dat.rodata.o",
        ])
        self.assertEqual(len(rodata["cmds"]), 2)
        self.assertEqual(rodata["kv"]["pc"], "light-green")
        for expected in (
            "$(S)/contrib/libs/icu/icudt78_dat.rodata",
            "$(S)/build/scripts/rodata2asm.py",
            "$(B)/contrib/tools/yasm/yasm",
        ):
            self.assertIn(expected, rodata["inputs"])
        yasm = lib.node_by_output(graph, "$(B)/contrib/tools/yasm/yasm")
        self.assertIn(yasm["uid"], rodata["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
