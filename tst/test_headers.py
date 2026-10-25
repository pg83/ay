import json
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
    def test_listed_peer_header_waits_for_a_real_source_consumer(self):
        files = {
            "petal/ya.make": f"PROGRAM(Petal)\n{BARE}ALLOCATOR(FAKE)\nPEERDIR(oak)\nADDINCL(petal/support)\nSRCS(main.cpp)\nEND()\n",
            "petal/main.cpp": '#include "shared/mock.hpp"\nint main(){return 0;}\n',
            "oak/ya.make": f"LIBRARY()\n{BARE}SRCS(owner.cpp ${{ARCADIA_ROOT}}/shared/mock.hpp)\nEND()\n",
            "oak/owner.cpp": "int owner(){return 0;}\n",
            "shared/mock.hpp": "#include <harness/h.hpp>\n",
            "petal/support/harness/h.hpp": '#include "tail.hpp"\n',
            "petal/support/harness/tail.hpp": "#pragma once\n",
            "older/ya.make": f"PROGRAM(Older)\n{BARE}ALLOCATOR(FAKE)\nPEERDIR(oak)\nSRCS(main.cpp)\nEND()\n",
            "older/main.cpp": "int main(){return 0;}\n",
        }
        for targets in (("older", "petal"), ("petal", "older")):
            with self.subTest(targets=targets):
                result = lib.make_process(files, targets[-1], *targets[:-1],
                                          "--target-platform", "default-linux-x86_64")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stderr, "")
                graph = json.loads(result.stdout)
                compile = lib.node_by_output(graph, "$(B)/petal/main.cpp.o")
                self.assertCountEqual(compile["inputs"], [
                    "$(S)/petal/main.cpp",
                    "$(S)/shared/mock.hpp",
                    "$(S)/petal/support/harness/h.hpp",
                    "$(S)/petal/support/harness/tail.hpp",
                ])
                self.assertEqual(compile["deps"], [])
                link = lib.node_by_output(graph, "$(B)/older/Older")
                self.assertNotIn("$(S)/shared/mock.hpp", link["inputs"])
                self.assertNotIn("$(S)/petal/support/harness/h.hpp", link["inputs"])

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
        for macro in ("STYLE_CPP()", "NO_LINT()\nSTYLE_CPP()", "STYLE_CPP_USER_SESSIONS()", "STYLE_CPP_YT(CONFIG_TYPE .clang-format)", "NO_LINT()\nSTYLE_CPP_YT()"):
            with self.subTest(macro=macro):
                files = header_files()
                files["producer/ya.make"] = f"LIBRARY()\n{BARE}PEERDIR(proto)\nSRCS(local.cpp)\n{macro}\nEND()\n"
                self.check_export(files, True)

    def test_weak_headers_do_not_resolve_before_required_compilation(self):
        for peers in ("weak producer", "producer weak"):
            with self.subTest(peers=peers):
                files = header_files()
                files["proto/ya.make"] = files["proto/ya.make"].replace(
                    "SRCS(a.proto)", "PROTO_NAMESPACE(GLOBAL proto)\nSRCS(a.proto)",
                )
                files.update({
                    "producer/ya.make": f"LIBRARY()\n{BARE}PEERDIR(proto)\nSRCS(local.cpp)\nEND()\n",
                    "producer/public.h": '#include <a.pb.h>\n',
                    "weak/ya.make": f"LIBRARY()\n{BARE}SRCS(empty.cpp)\nHEADERS(.)\nEND()\n",
                    "weak/empty.cpp": "",
                    "weak/public.h": '#include <producer/public.h>\n',
                    "consumer/ya.make": f"PROGRAM()\n{BARE}PEERDIR({peers})\nSRCS(main.cpp)\nEND()\n",
                    "consumer/main.cpp": "int main() {}\n",
                })
                self.check_export(files, True)

    def test_automatic_cpp_linter_adds_module_headers(self):
        for macro in ("STYLE_CPP()", "STYLE_CPP_YT(CONFIG_TYPE .clang-format)"):
            with self.subTest(macro=macro):
                files = header_files()
                files.update({
                    "build/conf/autoincludes.json": '["producer"]\n',
                    "producer/linters.make.inc": macro + "\n",
                    "producer/ya.make": f"LIBRARY()\n{BARE}PEERDIR(proto)\nSRCS(local.cpp)\nEND()\n",
                })
                self.check_export(files, True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
