from tst.make.lib import MakeTest, PROJECT


class FilesystemRetryContextsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "answer.h"\nint answer() { return 42; }\n')
        self.write("answer.h", "#pragma once\n")

    def test_filesystem_reads_retry_interrupted_system_calls(self):
        for fault in ("eintr-open", "eintr-openat", "eintr-read", "eintr-fstatat", "eintr-getdents"):
            with self.subTest(fault=fault):
                compiled = self.node(self.graph(env={"AY_CHAOS": fault + "=0"}), "answer.cpp.o")
                self.assertIn(f"$(S)/{PROJECT}/answer.h", compiled["inputs"])

    def test_unknown_directory_entry_types_are_resolved(self):
        compiled = self.node(self.graph(env={"AY_CHAOS": "dirent-unknown=1"}), "answer.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/answer.h", compiled["inputs"])

    def test_broken_directory_entries_do_not_hide_source_files(self):
        (self.project / "dangling.cpp").symlink_to("missing.cpp")
        compiled = self.node(self.graph(env={"AY_CHAOS": "dirent-unknown=1"}), "answer.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/answer.cpp", compiled["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/dangling.cpp", compiled["inputs"])
