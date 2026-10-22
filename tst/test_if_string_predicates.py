import unittest

import lib
from test_ymake_conf import Tree, cc_args, library


class IfStringPredicatesTest(unittest.TestCase):
    def evaluate(self, expected, prelude=""):
        body = prelude
        for index, condition in enumerate(expected):
            body += (
                f"IF({condition})\nCFLAGS(-DTAKEN_{index})\n"
                f"ELSE()\nCFLAGS(-DSKIPPED_{index})\nENDIF()\n"
            )
        graph = Tree(self, {
            "orchard/ya.make": library(body + "SRCS(blossom.cpp)\n"),
            "orchard/blossom.cpp": "int blossom;\n",
        }).graph("orchard", "--sandboxing")
        flags = cc_args(graph, "$(B)/orchard/blossom.cpp.o")
        for index, (condition, taken) in enumerate(expected.items()):
            with self.subTest(condition=condition):
                self.assertEqual(f"-DTAKEN_{index}" in flags, taken)
                self.assertEqual(f"-DSKIPPED_{index}" in flags, not taken)

    def test_matches_treats_regex_metacharacters_as_literal_substrings(self):
        self.evaluate({
            'MARKS MATCHES "("': True,
            'PLAIN MATCHES "("': False,
            'MARKS MATCHES "^a.b$"': True,
            'MARKS MATCHES "[c]+(d)?*|"': True,
            'MARKS MATCHES "."': True,
            'PLAIN MATCHES "."': False,
            'PLAIN MATCHES "^f.o$"': False,
            'PLAIN MATCHES "f.*"': False,
            'PLAIN MATCHES "f|x"': False,
            'PLAIN MATCHES "foo"': True,
        }, 'SET(MARKS "before^a.b$[c]+(d)?*|after")\nSET(PLAIN foo)\n')

    def test_matches_compares_ascii_and_non_ascii_bytes_exactly(self):
        self.evaluate({
            'BLOOM MATCHES "rOsE"': False,
            'BLOOM MATCHES "AMBER"': False,
            'BLOOM MATCHES "ROSE"': True,
            'BLOOM MATCHES "Amber"': True,
            'BLOOM MATCHES "AmberROSE"': True,
            'BLOOM MATCHES "missing"': False,
            '"École" MATCHES "é"': False,
            '"École" MATCHES "ÉCOLE"': False,
            '"École" MATCHES "École"': True,
            '"Жук" MATCHES "ж"': False,
            '"Жук" MATCHES "Ж"': True,
            '"Kelvin" MATCHES "k"': False,
            '"ſeed" MATCHES "s"': False,
            '"İris" MATCHES "i"': True,
            '"İ" MATCHES "i"': False,
            '"ı" MATCHES "I"': False,
        }, "SET(BLOOM AmberROSE)\n")

    def test_ends_with_is_a_literal_case_sensitive_suffix(self):
        self.evaluate({
            'BLOOM ENDS_WITH "Rose"': True,
            'BLOOM ENDS_WITH "rose"': False,
            'BLOOM ENDS_WITH "ROSE"': False,
            'BLOOM ENDS_WITH "AmberRose"': True,
            'BLOOM ENDS_WITH "Amber"': False,
            'BLOOM ENDS_WITH "xAmberRose"': False,
            'BLOOM ENDS_WITH "Rose$"': False,
            '"bud.(" ENDS_WITH ".("': True,
            '"École" ENDS_WITH "École"': True,
            '"École" ENDS_WITH "école"': False,
        }, "SET(BLOOM AmberRose)\n")

    def test_empty_and_undefined_values_follow_string_predicates(self):
        self.evaluate({
            'BLOOM MATCHES ""': True,
            'EMPTY_BLOOM MATCHES ""': True,
            'UNDEFINED_BLOOM MATCHES ""': True,
            'UNDEFINED_BLOOM MATCHES "x"': False,
            'BLOOM MATCHES ${UNDEFINED_NEEDLE}': True,
            'EMPTY_BLOOM MATCHES ${UNDEFINED_NEEDLE}': True,
            'UNDEFINED_BLOOM MATCHES ${EMPTY_BLOOM}': True,
            'UNDEFINED_BLOOM MATCHES UNDEFINED_NEEDLE': False,
            'BLOOM ENDS_WITH ""': True,
            'EMPTY_BLOOM ENDS_WITH ""': True,
            'UNDEFINED_BLOOM ENDS_WITH ""': True,
            'UNDEFINED_BLOOM ENDS_WITH "x"': False,
            'BLOOM ENDS_WITH ${UNDEFINED_NEEDLE}': True,
            'EMPTY_BLOOM ENDS_WITH ${UNDEFINED_NEEDLE}': True,
            'UNDEFINED_BLOOM ENDS_WITH ${EMPTY_BLOOM}': True,
            'UNDEFINED_BLOOM ENDS_WITH UNDEFINED_NEEDLE': False,
        }, "SET(BLOOM Rose)\nSET(EMPTY_BLOOM)\n")

    def test_right_operands_are_literal_unless_explicitly_expanded(self):
        self.evaluate({
            'BLOOM MATCHES NEEDLE': False,
            'BLOOM MATCHES ${NEEDLE}': True,
            'BLOOM MATCHES "${NEEDLE}"': True,
            '${BLOOM} MATCHES ${NEEDLE}': True,
            'LABEL MATCHES NEEDLE': True,
            'LABEL MATCHES ${NEEDLE}': False,
            'BLOOM ENDS_WITH NEEDLE': False,
            'BLOOM ENDS_WITH ${NEEDLE}': True,
            'BLOOM ENDS_WITH "${NEEDLE}"': True,
            '${BLOOM} ENDS_WITH ${NEEDLE}': True,
            'LABEL ENDS_WITH NEEDLE': True,
            'LABEL ENDS_WITH ${NEEDLE}': False,
        }, 'SET(BLOOM AmberRose)\nSET(NEEDLE Rose)\nSET(LABEL "flower NEEDLE")\n')

    def test_cached_lexical_include_uses_each_module_context(self):
        files = {
            "shared/petals.inc": (
                'IF(BLOOM MATCHES "Rose")\nCFLAGS(-DHAS_ROSE)\n'
                'ELSE()\nCFLAGS(-DNO_ROSE)\nENDIF()\n'
                'IF(BLOOM MATCHES ${NEEDLE})\nCFLAGS(-DHAS_NEEDLE)\n'
                'ELSE()\nCFLAGS(-DNO_NEEDLE)\nENDIF()\n'
                'IF(BLOOM ENDS_WITH Rose)\nCFLAGS(-DTAIL_ROSE)\n'
                'ELSE()\nCFLAGS(-DOTHER_TAIL)\nENDIF()\n'
                'IF(BLOOM ENDS_WITH ${NEEDLE})\nCFLAGS(-DTAIL_NEEDLE)\n'
                'ELSE()\nCFLAGS(-DOTHER_NEEDLE)\nENDIF()\n'
            ),
        }
        for target, bloom, needle in (("amber", "WildRose", "Rose"),
                                      ("indigo", "Snowdrop", "Snow")):
            files[f"{target}/ya.make"] = library(
                f"SET(BLOOM {bloom})\nSET(NEEDLE {needle})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/petals.inc)\nSRCS(petal.cpp)\n"
            )
            files[f"{target}/petal.cpp"] = "int petal;\n"
        for targets in (("amber", "indigo"), ("indigo", "amber")):
            graph = Tree(self, files).graph(*targets, "--sandboxing")
            for target in targets:
                flags = cc_args(graph, f"$(B)/{target}/petal.cpp.o")
                for present, absent in (("HAS_ROSE", "NO_ROSE"),
                                        ("TAIL_ROSE", "OTHER_TAIL"),
                                        ("TAIL_NEEDLE", "OTHER_NEEDLE")):
                    with self.subTest(targets=targets, target=target, flag=present):
                        self.assertEqual(f"-D{present}" in flags, target == "amber")
                        self.assertEqual(f"-D{absent}" in flags, target == "indigo")
                self.assertIn("-DHAS_NEEDLE", flags)
                self.assertNotIn("-DNO_NEEDLE", flags)

    def test_configuration_field_calculator_keeps_its_comparison_rules(self):
        graph = Tree(self, {
            "orchard/ya.make": library("SRCS(blossom.cpp)\n"),
            "orchard/blossom.cpp": '#include <equal.h>\n#include <lexical.h>\n#include <field.h>\n',
            "build/conf/sysincl.conf": (
                "CONF_LEFT=Amber\nCONF_RIGHT=Amber\nCONF_NUMBER=12\nSYSINCL=\n"
                "when ($CONF_LEFT == $CONF_RIGHT) {\nSYSINCL+=build/custom/equal.yml\n}\n"
                'when ($CONF_LEFT < "Zebra") {\nSYSINCL+=build/custom/lexical.yml\n}\n'
                "when ($CONF_NUMBER > 2) {\nSYSINCL+=build/custom/forbidden.yml\n}\n"
                "when ($CONF_NUMBER < 2) {\nSYSINCL+=build/custom/field.yml\n}\n"
            ),
            **{
                f"build/custom/{name}.yml": f"- includes:\n  - {name}.h: orchard/{name}.h\n"
                for name in ("equal", "lexical", "field")
            },
            **{f"orchard/{name}.h": "" for name in ("equal", "lexical", "field")},
            "build/custom/forbidden.yml": "invalid sysincl YAML\n",
        }).graph("orchard", "--sandboxing")
        compiled = lib.node_by_output(graph, "$(B)/orchard/blossom.cpp.o")
        self.assertEqual(set(compiled["inputs"]), {
            "$(S)/orchard/blossom.cpp", "$(S)/orchard/equal.h",
            "$(S)/orchard/lexical.h", "$(S)/orchard/field.h",
        })


if __name__ == "__main__":
    unittest.main(verbosity=2)
