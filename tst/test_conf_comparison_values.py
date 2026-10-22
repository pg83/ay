import unittest

import lib
from test_link_conf import link, tree


class ConfComparisonValuesTest(unittest.TestCase):
    def test_equality_uses_fieldcalculator_string_and_numeric_values(self):
        for condition, expected in (
            ('$ANDROID_API == "0"', True),
            ('$ANDROID_API == 0', False),
            ('$ANDROID_API == 1', True),
            ('0 == $ANDROID_API', False),
            ('1 == $ANDROID_API', True),
            ('$C_COMPILER == $C_COMPILER', True),
            ('$ABSENT_CONFIG_VALUE == ""', True),
            ('$ABSENT_CONFIG_VALUE == 0', True),
            ('0 == ""', True),
            ('1 == "17"', True),
            ('"17" == 17', False),
            ('12 == 12', True),
            ('007 == "007"', False),
            ('0 == "text"', False),
            ('"0" == 0', False),
            ('"0" == "0"', True),
            ('yes == "yes"', True),
            ('no == "no"', True),
        ):
            with self.subTest(condition=condition):
                conf = lib.FAKE_LD_CONF + (
                    f'\nwhen ({condition}) {{\nBEFORE_PEERS+=-lselected_branch\n}}\n'
                )
                graph = lib.make(tree(conf), "app")
                self.assertEqual("-lselected_branch" in link(graph, "$(B)/app/app"), expected)

    def test_typed_platform_values_remain_strings_in_ordering(self):
        for condition, expected in (
            ('$ANDROID_API < "1"', True),
            ('$ANDROID_API < 1', False),
            ('$ANDROID_API <= 1', True),
            ('$C_COMPILER < $C_COMPILER', False),
            ('$ABSENT_CONFIG_VALUE < 1', True),
        ):
            with self.subTest(condition=condition):
                conf = lib.FAKE_LD_CONF + (
                    f'\nwhen ({condition}) {{\nBEFORE_PEERS+=-lselected_branch\n}}\n'
                )
                graph = lib.make(tree(conf), "app")
                self.assertEqual("-lselected_branch" in link(graph, "$(B)/app/app"), expected)

    def test_unsupported_compound_operand_reports_an_error(self):
        for operator in ("==", "<"):
            with self.subTest(operator=operator):
                conf = lib.FAKE_LD_CONF + (
                    f'\nwhen (($OS_LINUX && $ARCH_AARCH64) {operator} "yes") {{\n'
                    'BEFORE_PEERS+=-lselected_branch\n}\n'
                )
                result = lib.make_process(tree(conf), "app")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("unexpected cond kind", result.stderr)
                self.assertNotIn("panic:", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
