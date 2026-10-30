from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("ay macro-audit diagnostics")
class MacroAuditTest(MakeTest):
    def test_known_keywords_empty_arguments_and_regular_words(self):
        self.write("ya.make", 'RESOURCES_LIBRARY()\nMESSAGE(OUT ordinary "")\nEND()\n')
        result = self.make("--dump-ignored-macros", dump_graph=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(result.stderr, r"OUT\s+× 1")
        self.assertIn('=== unhandled service-keyword arguments (not present as a "…" literal in *.go) ===\n  (none)', result.stderr)

    def test_empty_report(self):
        self.write("ya.make", "RESOURCES_LIBRARY()\nEND()\n")
        result = self.make("--dump-ignored-macros", dump_graph=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertIn("=== ya.make macros gen acknowledges but emits nothing for ===\n  (none)", result.stderr)
        self.assertIn("=== uppercase service-keyword arguments seen per macro ===\n  (none)", result.stderr)
        self.assertIn('=== unhandled service-keyword arguments (not present as a "…" literal in *.go) ===\n  (none)', result.stderr)

    def test_ignored_macros_and_user_keywords_are_counted(self):
        self.write("ya.make", (
            "RESOURCES_LIBRARY()\nSOURCE_GROUP(first)\nSOURCE_GROUP(second)\n"
            "MESSAGE(E2E_AUDIT_ALPHA)\nMESSAGE(E2E_AUDIT_ALPHA E2E_AUDIT_BETA)\nEND()\n"
        ))
        result = self.make("--dump-ignored-macros", dump_graph=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(result.stderr, r"SOURCE_GROUP\s+× 2")
        sections = result.stderr.split("=== unhandled service-keyword arguments", 1)
        self.assertEqual(len(sections), 2)
        for section in sections:
            self.assertRegex(section, r"E2E_AUDIT_ALPHA\s+× 2")
            self.assertRegex(section, r"E2E_AUDIT_BETA\s+× 1")
            self.assertLess(section.index("E2E_AUDIT_ALPHA"), section.index("E2E_AUDIT_BETA"))
