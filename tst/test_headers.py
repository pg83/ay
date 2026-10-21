import unittest

import lib
from test_proto_cpp import proto_files


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def header_files():
    files = proto_files()
    files.update({
        "proto/ya.make": f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{BARE}SRCS(a.proto)\nEND()\n",
        "proto/a.proto": 'syntax = "proto2";\nmessage A {}\n',
        "producer/local.cpp": '#include "public.h"\nint local;\n',
        "producer/public.h": '#include "proto/a.pb.h"\n',
        "consumer/ya.make": f"PROGRAM()\n{BARE}PEERDIR(producer)\nSRCS(main.cpp)\nEND()\n",
        "consumer/main.cpp": '#include "producer/public.h"\nint main() {}\n',
        "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
    })
    return files


class HeadersTest(unittest.TestCase):
    def check_export(self, files, expected):
        graph = lib.make(files, "consumer")
        link = lib.node_by_output(graph, "$(B)/consumer/consumer")
        proto = lib.node_by_output(graph, "$(B)/proto/a.pb.h")
        self.assertEqual("$(B)/proto/a.pb.h" in link["inputs"], expected)
        self.assertEqual(proto["uid"] in link["deps"], expected)
        self.assertNotIn("$(S)/producer/public.h", link["inputs"])

    def test_headers_exports_generated_includes_of_discovered_headers(self):
        for macro, expected in (
            ("", False),
            ("HEADERS(.)", True),
            ("HEADERS(${ARCADIA_ROOT}/producer)", True),
            ("SET(HEADER_DIR .)\nHEADERS(${HEADER_DIR})", True),
            ("HEADERS(. EXCLUDE public.h)", False),
            ("HEADERS(. EXCLUDE *.h)", False),
            ("HEADERS()", False),
        ):
            with self.subTest(macro=macro):
                files = header_files()
                files["producer/ya.make"] = f"LIBRARY()\n{BARE}PEERDIR(proto)\nSRCS(local.cpp)\n{macro}\nEND()\n"
                self.check_export(files, expected)

    def test_headers_uses_the_cpp_header_glob_without_implicit_recursion(self):
        for name, expected in (
            ("public.h", True),
            ("public.H", True),
            ("public.hh", True),
            ("public.hpp", True),
            ("public.hxx", True),
            ("public.ipp", True),
            ("public.inc", False),
            ("nested/public.h", False),
        ):
            with self.subTest(name=name):
                files = header_files()
                files[f"producer/{name}"] = files.pop("producer/public.h")
                files["producer/local.cpp"] = "int local;\n"
                files["consumer/main.cpp"] = "int main() {}\n"
                files["producer/ya.make"] = f"LIBRARY()\n{BARE}PEERDIR(proto)\nSRCS(local.cpp)\nHEADERS(.)\nEND()\n"
                self.check_export(files, expected)

    def test_cpp_style_adds_headers_even_when_lint_is_disabled(self):
        for macro in ("STYLE_CPP()", "NO_LINT()\nSTYLE_CPP()", "STYLE_CPP_USER_SESSIONS()"):
            with self.subTest(macro=macro):
                files = header_files()
                files["producer/ya.make"] = f"LIBRARY()\n{BARE}PEERDIR(proto)\nSRCS(local.cpp)\n{macro}\nEND()\n"
                self.check_export(files, True)

    def test_automatic_cpp_linter_adds_module_headers(self):
        files = header_files()
        files.update({
            "build/conf/autoincludes.json": '["producer"]\n',
            "producer/linters.make.inc": "STYLE_CPP()\n",
            "producer/ya.make": f"LIBRARY()\n{BARE}PEERDIR(proto)\nSRCS(local.cpp)\nEND()\n",
        })
        self.check_export(files, True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
