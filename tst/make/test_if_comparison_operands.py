import os
import unittest
from unittest.mock import patch

from tst import lib
from tst.make.test_ymake_conf import Tree, cc_args, library


class IfComparisonOperandsTest(unittest.TestCase):
    def evaluate(self, expected, prelude=""):
        body = prelude
        for index, condition in enumerate(expected):
            body += (
                f"IF({condition})\nCFLAGS(-DTAKEN_{index})\n"
                f"ELSE()\nCFLAGS(-DSKIPPED_{index})\nENDIF()\n"
            )
        graph = Tree(self, {
            "palette/ya.make": library(body + "SRCS(pigment.cpp)\n"),
            "palette/pigment.cpp": "int pigment;\n",
        }).graph("palette", "--sandboxing")
        flags = cc_args(graph, "$(B)/palette/pigment.cpp.o")
        for index, (condition, taken) in enumerate(expected.items()):
            with self.subTest(condition=condition):
                self.assertEqual(f"-DTAKEN_{index}" in flags, taken)
                self.assertEqual(f"-DSKIPPED_{index}" in flags, not taken)

    def test_bare_right_operands_are_literals_even_when_defined(self):
        self.evaluate({
            "SHADE == Amber": True,
            "SHADE != Amber": False,
            "SHADE == SHADE": False,
            "SHADE == ALIAS": False,
            "WORD == ALIAS": True,
            "MISSING_SHADE == Amber": False,
            "MISSING_SHADE != Amber": True,
            'MISSING_SHADE == ""': True,
            "MISSING_SHADE == MISSING_SHADE": False,
            "SHADE STARTS_WITH Am": True,
            "SHADE MATCHES Amber": True,
            "SHADE STARTS_WITH ALIAS": False,
            "ANDROID_API == Amber": False,
            "ANDROID_API == 0": True,
            'ANDROID_API == "0"': True,
            "PADDED == 007": True,
            "PADDED == 7": False,
        }, "SET(SHADE Amber)\nSET(ALIAS Amber)\nSET(WORD ALIAS)\nSET(PADDED 007)\n")

    def test_explicit_references_expand_defined_and_undefined_variables(self):
        self.evaluate({
            "SHADE == ${ALIAS}": True,
            'SHADE == "${ALIAS}"': True,
            "${SHADE} == Amber": True,
            'SHADE == "${MISSING_SHADE}Amber"': True,
            "MISSING_SHADE == ${MISSING_ALIAS}": True,
            "EMPTY_SHADE == ${MISSING_ALIAS}": True,
            "MISSING_SHADE == ${EMPTY_SHADE}": True,
            "SHADE != ${MISSING_ALIAS}": True,
            "LOCATION == ${CURDIR}": True,
            "LOCATION == CURDIR": False,
            "CURDIR == ${LOCATION}": True,
            "REVISION VERSION_GE ${MINIMUM}": True,
            "REVISION VERSION_LT ${MINIMUM}": False,
            "SHADE STARTS_WITH ${PREFIX}": True,
            "SHADE MATCHES ${ALIAS}": True,
        }, (
            "SET(SHADE Amber)\nSET(ALIAS Amber)\nSET(EMPTY_SHADE)\n"
            'SET(LOCATION "$(S)/palette")\nSET(REVISION 3.2)\nSET(MINIMUM 3.1)\nSET(PREFIX Am)\n'
        ))

    def test_numeric_comparisons_keep_variable_and_literal_positions(self):
        self.evaluate({
            "COUNT < LIMIT": False,
            "COUNT <= LIMIT": False,
            "COUNT > LIMIT": True,
            "COUNT >= LIMIT": True,
            "COUNT < ${LIMIT}": True,
            "COUNT <= ${LIMIT}": True,
            "COUNT > ${LIMIT}": False,
            "COUNT >= ${LIMIT}": False,
            "COUNT < 7": False,
            "COUNT <= 7": True,
            "COUNT > 7": False,
            "COUNT >= 7": True,
            "COUNT < UnknownNumber": False,
            "COUNT > UnknownNumber": True,
            "MISSING_COUNT < 1": True,
            "MISSING_COUNT <= 0": True,
            "MISSING_COUNT > 1": False,
            "MISSING_COUNT >= 0": True,
            "${COUNT} > ${MISSING_COUNT}": True,
            "COUNT > ${MISSING_COUNT}": True,
            "COUNT < ${MISSING_COUNT}": False,
            "COUNT < 999999999999999999999999999999": False,
        }, "SET(COUNT 7)\nSET(LIMIT 9)\n")

    def test_operand_expansion_uses_upstream_lexical_rules(self):
        with patch.dict(os.environ, {"PALETTE_COLOR": "Amber"}):
            self.evaluate({
                "SHADE == $ENV{PALETTE_COLOR}": True,
                'EMPTY_SHADE == "${unknown value}"': True,
                r'"\$" == "$"': True,
                '"${}" == "${}"': True,
                '"${" == "${"': True,
                '"$OTHER{SHADE}" == "$OTHER{SHADE}"': True,
                '"${PALETTE_RESOURCE_GLOBAL}" == ""': False,
                'SHADE == "$SHADE"': False,
            }, "SET(SHADE Amber)\nSET(EMPTY_SHADE)\n")

    def test_included_comparison_uses_each_modules_current_values(self):
        files = {
            "shared/choice.inc": (
                "IF(SHADE == Amber)\nCFLAGS(-DWARM)\n"
                "ELSE()\nCFLAGS(-DCOOL)\nENDIF()\n"
            ),
        }
        for target, shade in (("warm", "Amber"), ("cool", "Cobalt")):
            files[f"{target}/ya.make"] = library(
                f"SET(SHADE {shade})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/choice.inc)\nSRCS(pigment.cpp)\n"
            )
            files[f"{target}/pigment.cpp"] = "int pigment;\n"
        graph = Tree(self, files).graph("warm", "cool", "--sandboxing")
        for target, taken, skipped in (("warm", "WARM", "COOL"), ("cool", "COOL", "WARM")):
            flags = cc_args(graph, f"$(B)/{target}/pigment.cpp.o")
            self.assertIn(f"-D{taken}", flags)
            self.assertNotIn(f"-D{skipped}", flags)


if __name__ == "__main__":
    unittest.main(verbosity=2)
