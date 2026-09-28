import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


MODULE = "m"
OBJECT = "$(B)/m/a.cpp.o"
LIBRARY = (
    "LIBRARY()\n"
    "NO_LIBC()\n"
    "NO_RUNTIME()\n"
    "NO_UTIL()\n"
    "SRCS(a.cpp)\n"
    "END()\n"
)
READ_CHUNK = 1 << 20


def make_process(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-include-parse-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            if isinstance(content, bytes):
                path.write_bytes(content)
            else:
                path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        return subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )


def compile_closure(source, headers, *args):
    files = {f"{MODULE}/ya.make": LIBRARY, f"{MODULE}/a.cpp": source}
    for header in headers:
        files[f"{MODULE}/{header}"] = ""
    result = make_process(files, MODULE, "-k", *args)
    if result.returncode != 0:
        raise AssertionError(f"make failed:\n{result.stderr}")
    graph = json.loads(result.stdout)
    inputs = lib.node_by_output(graph, OBJECT)["inputs"]
    included = {
        path[len(f"$(S)/{MODULE}/"):]
        for path in inputs
        if path.startswith(f"$(S)/{MODULE}/") and path != f"$(S)/{MODULE}/a.cpp"
    }
    return included, result.stderr


def filler(size, line="int filler_value_0123456789;\n"):
    count = size // len(line)
    text = line * count
    return text + " " * (size - len(text))


class CIncludeDirectiveParsingTest(unittest.TestCase):
    def test_directive_spellings_and_comment_positions(self):
        source = (
            "int before;\n"
            '#import "imported.h"\n'
            '  #  include "indented.h"\n'
            'int x; # include "midline.h"\n'
            '/* lead */ #include "after_block.h"\n'
            '#/* a */include/* b */"between_comments.h"\n'
            "/*\n"
            '#include "in_block.h"\n'
            "*/\n"
            "/* opens here\n"
            '#include "in_block_tail.h"\n'
            'closes */ #include "after_close.h"\n'
            "int y; /*\n"
            '#include "trailing_comment_body.h"\n'
            "*/\n"
            '#include_next <next.h>\n'
            '#includex "word.h"\n'
            "#define X 1\n"
            "#/\n"
            '#include "unterminated.h\n'
            "#include <dollar$sign.h>\n"
        )
        headers = [
            "imported.h", "indented.h", "midline.h", "after_block.h",
            "between_comments.h", "in_block.h", "in_block_tail.h",
            "after_close.h", "trailing_comment_body.h", "next.h", "word.h",
            "unterminated.h", "dollar$sign.h",
        ]
        included, _ = compile_closure(source, headers)
        # A block comment hides a directive only when it opens at the start
        # of its line; include_next and non-keyword spellings are ignored.
        self.assertEqual(
            {
                "imported.h", "indented.h", "after_block.h",
                "between_comments.h", "trailing_comment_body.h",
            },
            included,
        )

    def test_computed_and_ignored_includes(self):
        source = (
            "#include COMPUTED_H\n"
            "#include $(DOLLAR_H)\n"
            "#include IGNORED_H // Y_IGNORE\n"
            '#include "ignored.h" //   Y_IGNORE\n'
            '#include "ignored_too.h" /* note */ // Y_IGNORE\n'
            '#include "kept.h" // not ignored\n'
            "#include <bracket[1].h>\n"
            "#include MACRO[2]\n"
            "#include BACKTRACE_HEADER\n"
            "#include OPENSSL_UNISTD\n"
            "#include\n"
            "#include \\\n"
            '  "continued.h"\n'
        )
        headers = [
            "COMPUTED_H", "IGNORED_H", "ignored.h", "ignored_too.h",
            "kept.h", "bracket[1].h", "continued.h",
        ]
        included, warnings = compile_closure(source, headers)
        self.assertEqual({"COMPUTED_H", "kept.h"}, included)
        # OPENSSL_UNISTD maps to <unistd.h>; a line continuation yields the
        # computed include "\" exactly as the upstream ragel grammar does.
        self.assertIn('unresolved include <unistd.h>', warnings)
        self.assertIn('unresolved include "\\"', warnings)
        self.assertNotIn("continued.h", warnings)
        self.assertNotIn("BACKTRACE_HEADER", warnings)

    def test_directives_at_end_of_file(self):
        cases = {
            '#include "last.h"': {"last.h"},
            '#include "last.h" /* unterminated': {"last.h"},
            "#include": set(),
            "#include   ": set(),
            "#inc": set(),
            "#": set(),
            "#  /* open": set(),
            "#include LAST_H": {"LAST_H"},
            '/*\n#include "last.h"': set(),
        }
        for tail, expected in cases.items():
            with self.subTest(tail=tail):
                included, _ = compile_closure(
                    '#include "first.h"\n' + tail,
                    ["first.h", "last.h", "LAST_H"],
                )
                self.assertEqual({"first.h"} | expected, included)

    def test_byte_order_mark_is_skipped(self):
        files = {
            f"{MODULE}/ya.make": LIBRARY,
            f"{MODULE}/a.cpp": b'\xef\xbb\xbf#include "bom.h"\n',
            f"{MODULE}/bom.h": "",
        }
        graph = json.loads(make_process(files, MODULE).stdout)
        self.assertIn(
            "$(S)/m/bom.h",
            lib.node_by_output(graph, OBJECT)["inputs"],
        )


class CIncludeChunkBoundaryTest(unittest.TestCase):
    """Sources above 1 MiB are read as two chunks split at READ_CHUNK."""

    def test_directive_straddling_the_chunk_boundary(self):
        head = '#include "head.h"\n'
        straddle = '#include "straddle.h"\n'
        body = head + filler(READ_CHUNK - len(head) - 5)
        source = body + straddle + '#include "tail.h"\n#include "eof.h"'
        self.assertLess(len(body), READ_CHUNK)
        self.assertGreater(len(body) + len(straddle), READ_CHUNK)
        included, _ = compile_closure(
            source, ["head.h", "straddle.h", "tail.h", "eof.h"]
        )
        self.assertEqual({"head.h", "straddle.h", "tail.h", "eof.h"}, included)

    def test_first_chunk_without_newline(self):
        source = "int x = 0" + " + 0" * (READ_CHUNK // 4) + ";\n" + '#include "after.h"\n'
        included, _ = compile_closure(source, ["after.h"])
        self.assertEqual({"after.h"}, included)

    def test_tail_chunk_without_newline(self):
        body = filler(READ_CHUNK - len('#include "tail'))
        source = body + '#include "tail.h"'
        self.assertEqual(READ_CHUNK + len('.h"'), len(source))
        included, _ = compile_closure(source, ["tail.h"])
        self.assertEqual({"tail.h"}, included)

    def test_first_chunk_ending_at_newline(self):
        source = filler(READ_CHUNK - 1) + "\n" + '#include "second.h"\n'
        included, _ = compile_closure(source, ["second.h"])
        self.assertEqual({"second.h"}, included)

    def test_block_comment_spanning_the_boundary(self):
        opener = '#include "before.h"\n/* comment spans the chunk boundary\n'
        source = (
            opener
            + filler(READ_CHUNK - len(opener) - 30)
            + '#include "hidden_one.h"\n'
            + '#include "hidden_two.h"\n'
            + '*/\n#include "after.h"\n'
        )
        included, _ = compile_closure(
            source, ["before.h", "hidden_one.h", "hidden_two.h", "after.h"]
        )
        self.assertEqual({"before.h", "after.h"}, included)

    def test_block_comment_open_through_both_chunks(self):
        opener = '#include "before.h"\n/* never closed\n'
        source = (
            opener
            + filler(READ_CHUNK - len(opener) - 10)
            + '#include "hidden.h"\n'
            + '#include "also_hidden.h"\n'
        )
        included, _ = compile_closure(
            source, ["before.h", "hidden.h", "also_hidden.h"]
        )
        self.assertEqual({"before.h"}, included)

    def test_block_comment_opened_in_straddling_line_piece(self):
        prefix = '#include "before.h"\n'
        body = prefix + filler(READ_CHUNK - len(prefix) - 2)
        source = (
            body
            + "/* open\n"
            + '#include "hidden.h"\n'
            + '*/\n#include "after.h"\n'
        )
        included, _ = compile_closure(
            source, ["before.h", "hidden.h", "after.h"]
        )
        self.assertEqual({"before.h", "after.h"}, included)


if __name__ == "__main__":
    unittest.main(verbosity=2)
