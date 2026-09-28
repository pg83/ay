import json
import unittest

import lib


READ_CHUNK = 1 << 20
CYTHON_PY2 = "contrib/tools/cython_py2/Cython/Includes"


def library(*lines):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        + "".join(f"{line}\n" for line in lines)
        + "END()\n"
    )


def inputs_of(graph, output):
    return set(lib.node_by_output(graph, output)["inputs"])


def padded_to_boundary(head, line, filler, offset):
    """Pad `head` with `filler` lines so that `line` starts `offset` bytes before READ_CHUNK."""
    size = READ_CHUNK - offset - len(head)
    body = filler * (size // len(filler))
    return head + body + " " * (size - len(body)) + line


class AsmIncludeTest(unittest.TestCase):
    def test_asm_sources_use_for_asm_addincl(self):
        files = {
            "m/ya.make": library("ADDINCL(FOR asm asmdir inc)", "SRCS(x.S y.asm z.cpp)"),
            "m/x.S": "#include <a.inc>\n#include <b.h>\n",
            "m/y.asm": '%include "c.inc"\n',
            "m/z.cpp": "#include <a.inc>\n",
            "asmdir/a.inc": "",
            "asmdir/c.inc": "",
            "inc/b.h": "",
        }
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "m", "-k")
        self.assertEqual(
            {"$(S)/m/x.S", "$(S)/asmdir/a.inc", "$(S)/inc/b.h"},
            inputs_of(graph, "$(B)/m/x.S.o"),
        )
        self.assertIn("$(S)/asmdir/c.inc", inputs_of(graph, "$(B)/m/y.o"))
        # C++ sources do not see ADDINCL(FOR asm ...).
        self.assertEqual({"$(S)/m/z.cpp"}, inputs_of(graph, "$(B)/m/z.cpp.o"))

    def test_includes_addressed_by_root_prefix(self):
        files = {
            "m/ya.make": library("RUN_PROGRAM(tools/gen OUT gen.inc)", "SRCS(y.asm)"),
            "m/y.asm": '%include "$(B)/m/gen.inc"\n%include "$(S)/m/src.inc"\n',
            "m/src.inc": "",
        }
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "m", "-k")
        self.assertEqual(
            {"$(B)/contrib/tools/yasm/yasm", "$(S)/m/y.asm", "$(B)/m/gen.inc", "$(S)/m/src.inc"},
            inputs_of(graph, "$(B)/m/y.o"),
        )


class CythonIncludeTest(unittest.TestCase):
    def test_cimport_probes_and_py2_include_overrides(self):
        files = {
            "m/ya.make": library("BUILDWITH_CYTHON_CPP(a.pyx)"),
            "m/a.pyx": (
                "from m.mod cimport foo\n"
                "from m.pkg cimport sub, other\n"
                "cimport m.plain\n"
                "from util.generic.string cimport TString\n"
                "from util.generic.hash cimport THashMap\n"
                "from util.generic.hash_set cimport THashSet\n"
                "from util.system.types cimport ui8\n"
                "from contrib.tools.cython_py2.Cython.Includes.wrap cimport w\n"
                "cdef extern from <sys.h>:\n"
                "    pass\n"
            ),
            "m/a.pxd": "",
            "m/mod.pxd": "",
            "m/mod/foo/__init__.pxd": "",
            "m/pkg/__init__.pxd": "",
            "m/pkg/sub.pxd": "",
            "m/pkg/other/__init__.pxd": "",
            "m/pkg/other.pxd": "",
            "m/plain.pxd": "",
            "m/plain/__init__.pxd": "",
            "util/generic/string.pxd": (
                "from libcpp.string cimport string\nfrom libcpp.vector cimport vector\n"
            ),
            "util/generic/hash.pxd": "from libcpp.pair cimport pair\n",
            "util/generic/hash_set.pxd": "from libcpp.pair cimport pair\n",
            "util/system/types.pxd": "from libc.stdint cimport uint8_t\n",
            f"{CYTHON_PY2}/wrap.pxd": (
                "from libc.stdio cimport FILE\nfrom other.thing cimport x\n"
            ),
            f"{CYTHON_PY2}/libcpp/string.pxd": "",
            f"{CYTHON_PY2}/libcpp/pair.pxd": "",
            f"{CYTHON_PY2}/libc/stdint.pxd": "",
            f"{CYTHON_PY2}/libc/stdio.pxd": "",
            "contrib/tools/cython/cython.py": "",
            "sys.h": "",
        }
        graph = lib.make(files, "m", "-k")
        self.assertEqual(
            {
                "$(B)/m/a.pyx.cpp",
                "$(S)/contrib/tools/cython/cython.py",
                "$(S)/m/a.pyx",
                # The sibling .pxd of a .pyx is always probed.
                "$(S)/m/a.pxd",
                # A found module .pxd suppresses the per-name probes.
                "$(S)/m/mod.pxd",
                # A package: each name probes <name>/__init__.pxd first and
                # falls back to <name>.pxd only when that probe missed.
                "$(S)/m/pkg/__init__.pxd",
                "$(S)/m/pkg/sub.pxd",
                "$(S)/m/pkg/other/__init__.pxd",
                "$(S)/m/plain.pxd",
                "$(S)/util/generic/string.pxd",
                "$(S)/util/generic/hash.pxd",
                "$(S)/util/generic/hash_set.pxd",
                "$(S)/util/system/types.pxd",
                f"$(S)/{CYTHON_PY2}/wrap.pxd",
                f"$(S)/{CYTHON_PY2}/libcpp/string.pxd",
                f"$(S)/{CYTHON_PY2}/libcpp/pair.pxd",
                f"$(S)/{CYTHON_PY2}/libc/stdint.pxd",
                f"$(S)/{CYTHON_PY2}/libc/stdio.pxd",
                "$(S)/sys.h",
            },
            inputs_of(graph, "$(B)/m/a.pyx.cpp.o"),
        )


class InducedDepsTest(unittest.TestCase):
    def test_generator_tool_induced_deps_follow_generated_files(self):
        files = {
            "lib/ya.make": library(
                "BASE_CODEGEN(tool base_gen)",
                "SRCS(GLOBAL ${BINDIR}/base_gen.cpp GLOBAL use.cpp)",
            ),
            "lib/base_gen.in": "// input\n",
            "lib/use.cpp": "#include <lib/base_gen.h>\n",
            "tool/ya.make": (
                "PROGRAM(base_gen)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "INDUCED_DEPS(h ${ARCADIA_ROOT}/ind/h_only.h)\n"
                "INDUCED_DEPS(cpp ind/cpp_only.h)\n"
                "INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/ind/both.h ${ARCADIA_BUILD_ROOT}/ind/none.h)\n"
                "INDUCED_DEPS(h ${ARCADIA_ROOT}/ind/absent.inc)\n"
                "SRCS(main.cpp)\nEND()\n"
            ),
            "tool/main.cpp": "int main(){return 0;}\n",
            "ind/h_only.h": "",
            "ind/cpp_only.h": "",
            "ind/both.h": '#include "nested.h"\n',
            "ind/nested.h": "",
        }
        graph = lib.make(files, "lib")
        generated = {"$(B)/lib/base_gen.cpp", "$(S)/lib/base_gen.in"}
        both = {"$(S)/ind/both.h", "$(S)/ind/nested.h", "$(B)/ind/none.h"}
        self.assertEqual(
            generated | both | {"$(S)/ind/cpp_only.h"},
            inputs_of(graph, "$(B)/lib/base_gen.cpp.o"),
        )
        self.assertEqual(
            generated | both | {
                "$(S)/lib/use.cpp", "$(B)/lib/base_gen.h", "$(S)/ind/h_only.h",
                "$(S)/ind/absent.inc",
            },
            inputs_of(graph, "$(B)/lib/use.cpp.o"),
        )


class GeneratedHeaderDomainTest(unittest.TestCase):
    def test_ymaps_sproto_header_closure(self):
        files = {
            "m/ya.make": library("YMAPS_SPROTO(x.proto)", "SRCS(a.cpp)"),
            "m/x.proto": 'import "m/y.proto";\n',
            "m/y.proto": "",
            "m/a.cpp": '#include "m/x.sproto.h"\n',
            "maps/libs/sproto/ya.make": library("SRCS(s.cpp)"),
            "maps/libs/sproto/s.cpp": "",
        }
        lib.tool_program(files, "maps/libs/sproto/sprotoc", "sprotoc")
        graph = lib.make(files, "m", "-k")
        self.assertEqual(
            {"$(B)/maps/libs/sproto/sprotoc/sprotoc", "$(S)/m/x.proto"},
            inputs_of(graph, "$(B)/m/x.sproto.h"),
        )
        self.assertEqual(
            {"$(S)/m/a.cpp", "$(B)/m/x.sproto.h", "$(S)/m/x.proto"},
            inputs_of(graph, "$(B)/m/a.cpp.o"),
        )

    def test_empty_parser_context_for_unregistered_extension(self):
        files = {
            "m/ya.make": library(
                "COPY_FILE(src.m4 dst.m4 OUTPUT_INCLUDES y.inc)", "SRCS(a.cpp)"
            ),
            "m/src.m4": "",
            "m/y.inc": '#include "z.h"\n',
            "m/z.h": "",
            "m/a.cpp": '#include "dst.m4"\n',
            "build/scripts/fs_tools.py": "",
        }
        graph = lib.make(files, "m")
        # y.inc has no registered parser; reached from an .m4 closure it is
        # parsed with the (empty) m4 parser, so z.h is not followed.
        self.assertEqual(
            {"$(S)/build/scripts/fs_tools.py", "$(S)/m/src.m4", "$(S)/m/y.inc"},
            inputs_of(graph, "$(B)/m/dst.m4"),
        )


class PlatformDomainTest(unittest.TestCase):
    def test_x86_64_join_sources_rescan_for_target(self):
        files = {
            "j/ya.make": library("JOIN_SRCS(all.cpp s1.cpp)", "PEERDIR(p)", "SRCS(other.cpp)"),
            "j/s1.cpp": "#include <ph.h>\n",
            "j/other.cpp": "",
            "p/ya.make": library("ADDINCL(GLOBAL p/inc)", "SRCS(p.cpp)"),
            "p/p.cpp": "",
            "p/inc/ph.h": "",
        }
        graph = lib.make(files, "j", "--target-platform", "default-linux-x86_64")
        self.assertEqual(
            {"$(S)/j/s1.cpp", "$(S)/p/inc/ph.h"}, inputs_of(graph, "$(B)/j/all.cpp")
        )

    def test_go_assembly_includes(self):
        files = {
            "g/ya.make": "GO_LIBRARY()\nSRCS(a.go b.s)\nEND()\n",
            "g/a.go": "package g\n",
            "g/b.s": '#include "textflag.h"\n#include "local.h"\n',
            "g/local.h": "",
            "build/scripts/go_fake_include/textflag.h": "",
        }
        graph = lib.make(files, "g", "-k")
        self.assertEqual(
            {
                "$(S)/g/b.s", "$(S)/g/local.h",
                "$(S)/build/scripts/go_fake_include/textflag.h",
            },
            inputs_of(graph, "$(B)/g/gen.symabis"),
        )


class ProtoImportTest(unittest.TestCase):
    def proto_graph(self, protos):
        files = {
            "build/scripts/cpp_proto_wrapper.py": "",
            "contrib/libs/protobuf/ya.make": library("SRCS(protobuf.cpp)"),
            "contrib/libs/protobuf/protobuf.cpp": "",
            "p/ya.make": (
                "PROTO_LIBRARY()\n"
                f"SRCS({' '.join(name for name in protos if name.endswith('.proto'))})\n"
                "END()\n"
            ),
        }
        lib.tool_program(files, "contrib/tools/protoc", "protoc")
        lib.tool_program(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
        for name, content in protos.items():
            files[f"p/{name}"] = content
        return lib.make(files, "p", "-k")

    def imports(self, graph, name):
        stem = name[:-len(".proto")]
        return {
            path[len("$(S)/p/"):]
            for path in inputs_of(graph, f"$(B)/p/{stem}.pb.h")
            if path.startswith("$(S)/p/") and path != f"$(S)/p/{name}"
        }

    def test_import_statement_forms(self):
        graph = self.proto_graph({
            "a.proto": (
                'syntax = "proto3";\r\n'
                'import "p/plain.proto";\r\n'
                '  import public   "p/public.proto";\n'
                'import weak <p/weak.proto>;\n'
                "import 'p/single.proto';\n"
                'import "<p/wrapped.proto>";\n'
                'import publicity "p/publicity.proto";\n'
                'importx "p/importx.proto";\n'
                'import_ "p/import_.proto";\n'
                'import9 "p/import9.proto";\n'
                'importZ "p/importZ.proto";\n'
                "import\n"
                "import;\n"
                "import public\n"
                'import "p/unterminated.proto\n'
                'import "";\n'
                'import "<>";\n'
                'import "p//comment.proto";\n'
                "\n"
                'import "p/last.proto";'
            ),
            "plain.proto": "", "public.proto": "", "weak.proto": "",
            "single.proto": "", "wrapped.proto": "", "last.proto": "",
            "publicity.proto": "", "importx.proto": "", "import_.proto": "",
            "import9.proto": "", "importZ.proto": "", "unterminated.proto": "",
            "comment.proto": "",
        })
        self.assertEqual(
            {
                "plain.proto", "public.proto", "weak.proto", "single.proto",
                "wrapped.proto", "last.proto",
            },
            self.imports(graph, "a.proto"),
        )

    def test_imports_across_the_read_chunk_boundary(self):
        straddling = padded_to_boundary(
            'syntax = "proto3";\r\n', 'import "p/straddle.proto";\r\n', "// f\r\n", 8
        )
        unterminated_tail = padded_to_boundary(
            'syntax = "proto3";\n', 'import "p/tail.proto";', "// filler\n", 5
        )
        graph = self.proto_graph({
            "a.proto": straddling + 'import "p/after.proto";\nimport "p/eof.proto";',
            "b.proto": unterminated_tail,
            "c.proto": padded_to_boundary("", "\n", "// filler line\n", 1)
            + 'import "p/after.proto";\n',
            "straddle.proto": "", "after.proto": "", "eof.proto": "", "tail.proto": "",
        })
        self.assertEqual(
            {"straddle.proto", "after.proto", "eof.proto"},
            self.imports(graph, "a.proto"),
        )
        self.assertEqual({"tail.proto"}, self.imports(graph, "b.proto"))
        self.assertEqual({"after.proto"}, self.imports(graph, "c.proto"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
