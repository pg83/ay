import unittest

from tst.cli import ESC, RESET, USAGE_HEAD, fatal, run_ay


class CliDispatchTest(unittest.TestCase):
    def test_top_level_usage_collapses_dev_commands(self):
        result = run_ay()
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        text = result.stderr
        self.assertTrue(text.startswith(USAGE_HEAD + "<subcommand> [args]\n"))
        self.assertIn(ESC + "[91mmake:" + RESET, text)
        self.assertIn(
            ESC + "[91mdev:" + RESET
            + "\n    🛠️ Developer tooling (dump, perf, refac, probe). Pass --verbose to list.",
            text,
        )
        self.assertNotIn("dev dump sort", text)
        self.assertNotIn("fetch", text)
        self.assertNotIn("--probe", text)

    def test_verbose_usage_lists_hidden_and_dev_commands(self):
        for flag in ("-v", "--verbose"):
            result = run_ay(flag)
            self.assertEqual(result.returncode, 2)
            text = result.stderr
            self.assertIn(ESC + "[93m--probe=map|callsite|str" + RESET, text)
            for name in (
                "fetch:", "fetch base64:", "fetch sandbox:", "make:",
                "dev cas:", "dev maxrss:", "dev dump normalize:",
                "dev perf buckethash:", "dev refac case:",
                "dev probe callsite:",
            ):
                self.assertIn(ESC + "[91m" + name + RESET, text)
            self.assertNotIn("Pass --verbose to list", text)

    def test_unknown_subcommand(self):
        for argv in (["bogus"], ["dev", "nothing"], ["-h"], [""]):
            result = run_ay(*argv)
            self.assertEqual(result.returncode, 2)
            self.assertTrue(result.stderr.startswith(
                "unknown subcommand: " + " ".join(argv) + "\n" + USAGE_HEAD
            ))

    def test_unknown_global_flags_are_fatal(self):
        cases = {
            ("--bogus",): 'unknown global flag "--bogus"',
            ("--probe=str", "make"): 'unknown --probe="str" (want map|callsite)',
        }
        for argv, message in cases.items():
            result = run_ay(*argv)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, fatal(message))


if __name__ == "__main__":
    unittest.main(verbosity=2)
