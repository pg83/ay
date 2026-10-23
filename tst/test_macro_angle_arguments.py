import unittest

import lib
from test_yamake_syntax import user_cflags


CASES = {
    "embedded": ("CFLAGS(-DOAK_HEADER=<copper/root.h> -DX=a<=b -DY=c>=d -DZ=a<b>c)",
                 ["-DOAK_HEADER=<copper/root.h>", "-DX=a<=b", "-DY=c>=d", "-DZ=a<b>c"]),
    "standalone": ("CFLAGS(< > <= >= == !=)", ["<", ">", "<=", ">=", "==", "!="]),
    "quoted": ('CFLAGS("-DOAK_HEADER=<copper/root.h>" \'<\' ">" "<=" ">=" "==" "!=")',
               ["-DOAK_HEADER=<copper/root.h>", "<", ">", "<=", ">=", "==", "!="]),
    "setter": ("SET(HEADER <copper/root.h>)\nCFLAGS(-DOAK_HEADER=${HEADER})",
               ["-DOAK_HEADER=<copper/root.h>"]),
    "segments": ('CFLAGS(-DA=ONE<two> -DB=12<34> -DC="<leaf>" -DD=pre\'<mid>\'post)',
                 ["-DA=ONE<two>", "-DB=12<34>", "-DC=<leaf>", "-DD=pre<mid>post"]),
    "condition": (
        "CFLAGS(<)\nSET(LEVEL 3)\nIF(LEVEL < 4)\nCFLAGS(-DLESS)\nENDIF()\n"
        "IF(LEVEL > 4)\nCFLAGS(-DWRONG)\nELSE()\nCFLAGS(-DNOT_GREATER)\nENDIF()\n"
        "IF(LEVEL <= 3 AND LEVEL >= 3 AND LEVEL == 3 AND LEVEL != 4)\n"
        "CFLAGS(-DEQUAL_RANGE)\nENDIF()\nCFLAGS(>)",
        ["<", "-DLESS", "-DNOT_GREATER", "-DEQUAL_RANGE", ">"],
    ),
}


def workspace(body):
    return {
        "grove/ya.make": (
            "LIBRARY(oak)\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nNO_COMPILER_WARNINGS()\n"
            f"ADDINCL(grove/include)\n{body}\nSRCS(x.cpp)\nEND()\n"
        ),
        "grove/x.cpp": "#include <copper/root.h>\nint oak;\n",
        "grove/include/copper/root.h": '#include "leaf.h"\n',
        "grove/include/copper/leaf.h": "#pragma once\n",
    }


class MacroAngleArgumentsTest(unittest.TestCase):
    def check_case(self, name):
        body, flags = CASES[name]
        graph = lib.make(workspace(body), "grove")
        self.assertEqual(user_cflags(graph, "$(B)/grove/x.cpp.o"), flags)
        compiled = lib.node_by_output(graph, "$(B)/grove/x.cpp.o")
        self.assertIn("$(S)/grove/x.cpp", compiled["inputs"])
        self.assertIn("$(S)/grove/include/copper/root.h", compiled["inputs"])
        self.assertIn("$(S)/grove/include/copper/leaf.h", compiled["inputs"])
        archive = lib.node_by_output(graph, "$(B)/grove/liboak.a")
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertIn(compiled["outputs"][0], archive["inputs"])
        self.assertIn(archive["uid"], graph["result"])

    def test_embedded_angle_bytes_remain_in_their_argument(self):
        self.check_case("embedded")
        self.check_case("segments")

    def test_standalone_operators_are_macro_argument_values(self):
        self.check_case("standalone")

    def test_quoted_and_setter_values_preserve_angle_bytes(self):
        self.check_case("quoted")
        self.check_case("setter")

    def test_if_comparisons_keep_their_operator_semantics_between_macros(self):
        self.check_case("condition")


if __name__ == "__main__":
    unittest.main()
