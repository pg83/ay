import unittest

import lib


def flatbuffers_tree(sources):
    files = {
        "contrib/libs/flatbuffers/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(fb.cpp)\nEND()\n"
        ),
        "contrib/libs/flatbuffers/fb.cpp": "int fb(){return 0;}\n",
        "contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h": "",
        "build/scripts/cpp_flatc_wrapper.py": "",
        "m/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            f"SRCS({' '.join(sources)})\nEND()\n"
        ),
    }
    lib.tool_program(files, "contrib/libs/flatbuffers/flatc", "flatc")
    return files


def schema_imports(files, schema):
    graph = lib.make(files, "m")
    node = lib.node_by_output(graph, f"$(B)/m/{schema}.h")
    return {
        path[len("$(S)/m/"):]
        for path in node["inputs"]
        if path.startswith("$(S)/m/") and path != f"$(S)/m/{schema}"
    }


class FlatbuffersIncludeTest(unittest.TestCase):
    def test_comments_strings_and_raw_strings_hide_includes(self):
        schema = (
            'include "m/plain.fbs";\n'
            'include\t  "m/spaced.fbs"  ;  \n'
            'include "m/crlf.fbs";\r\n'
            '// include "m/line_comment.fbs";\n'
            '/* include "m/block.fbs"; */\n'
            "/*\n"
            'include "m/block_multi.fbs";\n'
            "*/\n"
            'include "m/after_block.fbs"; /* trailing */\n'
            'attribute "/*";\n'
            'include "m/after_string.fbs";\n'
            'attribute "\\"/*";\n'
            'include "m/after_escaped_quote.fbs";\n'
            "attribute '/*';\n"
            'include "m/after_char.fbs";\n'
            "attribute '\\'/*';\n"
            'include "m/after_escaped_char.fbs";\n'
            'attribute "unterminated /*\n'
            'include "m/after_open_string.fbs";\n'
            "attribute 'unterminated /*\n"
            'include "m/after_open_char.fbs";\n'
            'x = R"(\n'
            'include "m/in_raw.fbs";\n'
            ')";\n'
            'include "m/after_raw.fbs";\n'
            'x = R"tag(\n'
            'include "m/in_raw_tag.fbs";\n'
            ')other" )tag";\n'
            'include "m/after_raw_tag.fbs";\n'
            'fooR"(not raw /* )";\n'
            'include "m/after_ident_raw.fbs";\n'
            'R"no_paren_on_line /*\n'
            'include "m/after_bad_raw.fbs";\n'
            'R"0123456789abcdefgh( /*\n'
            'include "m/after_long_delim.fbs";\n'
            "#include <x/*y>\n"
            'include "m/after_hash_angle.fbs";\n'
            '  #\tinclude_next "p/*\\"q"\n'
            'include "m/after_hash_next.fbs";\n'
            "#include <open/*\n"
            'include "m/after_hash_open.fbs";\n'
            "*/\n"
            "#define X /*\n"
            'include "m/after_hash_define.fbs";\n'
            "*/\n"
            "#include MACRO /*\n"
            'include "m/after_hash_macro.fbs";\n'
            "*/\n"
            'include "";\n'
            'include "m/no_close.fbs\n'
            'include "m/no_semicolon.fbs"\n'
            "include 'm/single.fbs';\n"
            "include\n"
            'includex "m/word.fbs";\n'
            'include "m/last.fbs";'
        )
        names = [
            "plain", "spaced", "crlf", "line_comment", "block", "block_multi",
            "after_block", "after_string", "after_escaped_quote", "after_char",
            "after_escaped_char", "after_open_string", "after_open_char",
            "in_raw", "after_raw", "in_raw_tag", "after_raw_tag",
            "after_ident_raw", "after_bad_raw", "after_long_delim",
            "after_hash_angle", "after_hash_next", "after_hash_open",
            "after_hash_define", "after_hash_macro", "no_close",
            "no_semicolon", "single", "word", "last",
        ]
        files = flatbuffers_tree(["a.fbs"] + [f"{name}.fbs" for name in names])
        files["m/a.fbs"] = schema
        for name in names:
            files[f"m/{name}.fbs"] = "namespace n;\n"
        self.assertEqual(
            {
                "plain.fbs", "spaced.fbs", "crlf.fbs", "after_block.fbs",
                "after_string.fbs", "after_escaped_quote.fbs",
                "after_char.fbs", "after_escaped_char.fbs",
                "after_open_string.fbs", "after_open_char.fbs",
                "after_raw.fbs", "after_raw_tag.fbs", "after_ident_raw.fbs",
                "after_bad_raw.fbs", "after_long_delim.fbs",
                "after_hash_angle.fbs", "after_hash_next.fbs", "last.fbs",
            },
            schema_imports(files, "a.fbs"),
        )

    def test_trailing_constructs_at_end_of_file(self):
        cases = {
            'include "m/b.fbs";\nx = R"d(unterminated )d': {"b.fbs"},
            'include "m/b.fbs";\n#include <open': {"b.fbs"},
            'include "m/b.fbs";\n#include "a\\': {"b.fbs"},
            'include "m/b.fbs";\n#include   ': {"b.fbs"},
            'include "m/b.fbs";\n#  ': {"b.fbs"},
            'include "m/b.fbs";\n   ': {"b.fbs"},
            'include "m/b.fbs";\n/* open': {"b.fbs"},
            'R"(\ninclude "m/hidden.fbs";\n)"\ninclude "m/b.fbs";': {"b.fbs"},
        }
        for schema, expected in cases.items():
            with self.subTest(schema=schema):
                files = flatbuffers_tree(["a.fbs", "b.fbs"])
                files["m/a.fbs"] = schema
                files["m/b.fbs"] = "namespace b;\n"
                self.assertEqual(expected, schema_imports(files, "a.fbs"))

    def test_transitive_imports_across_large_schema(self):
        files = flatbuffers_tree(["a.fbs", "b.fbs", "c.fbs"])
        files["m/a.fbs"] = (
            "// " + "x" * (1 << 20) + "\n" + 'include "m/b.fbs";\n'
        )
        files["m/b.fbs"] = 'include "m/c.fbs";\n'
        files["m/c.fbs"] = "namespace c;\n"
        self.assertEqual({"b.fbs", "c.fbs"}, schema_imports(files, "a.fbs"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
