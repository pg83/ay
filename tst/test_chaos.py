import re
import unittest
from pathlib import Path

import lib


ROOT = Path(__file__).resolve().parent.parent
POINT = re.compile(r'newChaos(?:Fault|Number)\("([^"]+)"\)')
ARMED = re.compile(r"(?<![\w-])([a-z][a-z0-9-]*)=\S")

FILES = {
    "lib/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.cpp)\nEND()\n",
    "lib/a.cpp": "int a() { return 0; }\n",
}


def chaos(words):
    return lib.make_process(FILES, "lib", env={"AY_CHAOS": words})


class ChaosWordsTest(unittest.TestCase):
    def test_malformed_words_stop_the_process(self):
        for word in ("assert", "no-such-point=1", "assert=x", "assert=-1"):
            with self.subTest(word=word):
                result = chaos(word)
                self.assertEqual(result.returncode, 2)
                self.assertEqual(result.stdout, "")
                self.assertEqual(result.stderr, f'AY_CHAOS: bad word "{word}"\n')

    def test_unarmed_points_change_nothing(self):
        self.assertEqual(lib.make(FILES, "lib", env={"AY_CHAOS": ""}), lib.make(FILES, "lib"))


class ChaosAssertTest(unittest.TestCase):
    def test_failed_assert_is_reported_as_an_error(self):
        result = chaos("assert=0")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertRegex(result.stderr, r"^\x1b\[31m.+\x1b\[0m\n$")

    def test_foreign_panic_crashes_through_try(self):
        result = chaos("foreign-panic=0")
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("panic: chaos: foreign panic [recovered", result.stderr)


class ChaosRegistryTest(unittest.TestCase):
    def test_every_point_is_unique_and_armed_by_a_test(self):
        names = [
            name
            for source in sorted(ROOT.glob("*.go"))
            for name in POINT.findall(source.read_text())
        ]
        self.assertEqual(len(names), len(set(names)), names)
        armed = {
            name
            for path in sorted((ROOT / "tst").glob("test_*.py"))
            for name in ARMED.findall(path.read_text())
        }
        self.assertEqual(sorted(set(names) - armed), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
