from tst.make.lib import MakeTest


class ArchiveRootedMissingInputTest(MakeTest):
    def test_archives_reject_missing_rooted_source_files(self):
        self.write("ya.make", f"LIBRARY()\nARCHIVE(NAME values.inc ${{ARCADIA_ROOT}}/{self.project_name}/missing.txt)\nRESOURCE(values.inc /data)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing.txt", result.stderr)
