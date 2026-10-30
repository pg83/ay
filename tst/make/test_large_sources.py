from tst.make.lib import MakeTest, PROJECT


class LargeSourcesTest(MakeTest):
    def test_include_after_a_megabyte_of_leading_whitespace_without_newlines(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", " " * (2 * 1024 * 1024) + '#include "dependency.h"')
        self.write("dependency.h", "#pragma once\n")
        self.assertIn(f"$(S)/{PROJECT}/dependency.h", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_unclosed_multichunk_comments_do_not_create_include_dependencies(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '/*\n#include "absent.h"\n' + "x" * (2 * 1024 * 1024))
        self.node(self.graph(), "answer.cpp.o")

    def test_preprocessor_directives_can_end_before_an_include_target(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        for text in ("#include", "# /* unfinished"):
            with self.subTest(text=text):
                self.write("answer.cpp", text)
                self.node(self.graph(), "answer.cpp.o")

    def test_includes_across_large_source_chunks(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("dependency.h", "#pragma once\n#define ANSWER 42\n")
        # Exercise large source reads with a long line and multiline comments;
        # includes inside the comments must never become file dependencies.
        self.write("answer.cpp", '/*\n' + '#include "absent.h"\n' * 7000 + '*/\n// ' + 'x' * 180000 + '\n#include "dependency.h"\nint answer() { return ANSWER; }\n')
        inputs = self.node(self.graph(), "answer.cpp.o")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/dependency.h", inputs)
        self.assertFalse(any(path.endswith("/absent.h") for path in inputs))

    def test_resource_selected_directories(self):
        self.write("ya.make", "LIBRARY()\nALL_RESOURCE_FILES_FROM_DIRS(PREFIX /data/ STRIP data data data/nested)\nEND()\n")
        self.write("data/one.txt", "1\n")
        self.write("data/nested/two.json", "{}\n")
        graph = self.graph()
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/data/one.txt", inputs)
        self.assertIn(f"$(S)/{PROJECT}/data/nested/two.json", inputs)

    def test_resource_glob_is_not_a_file(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE_FILES(PREFIX /data/ data/**/*.txt)\nEND()\n")
        self.write("data/nested/two.txt", "2\n")
        self.assertNotEqual(self.make().returncode, 0)
