import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
LAST_COMMON_DEFINE = "-D__LONG_LONG_SUPPORTED"
FIRST_NO_LIBC_FLAG = "-UNDEBUG"
SIMD_VARIANTS = ["AVX", "AVX2", "AVX512", "AMX", "SSE2", "SSE3", "SSSE3", "SSE4", "SSE41", "XOP"]


def ay_make(files, target="a", *args, platform="default-linux-aarch64"):
    with tempfile.TemporaryDirectory(prefix="ay-yamake-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n'
            '[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        result = subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", platform,
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
        graph = json.loads(result.stdout) if result.returncode == 0 else None
        return result.returncode, graph, ANSI.sub("", result.stderr).strip()


def module(kind, body, sources="x.cpp"):
    return f"{kind}()\n" + NO_PLATFORM + body + f"\nSRCS({sources})\nEND()\n"


def args_of(graph, output, index=0):
    return lib.node_by_output(graph, output)["cmds"][index]["cmd_args"]


def link_args(graph, output):
    return lib.node_by_output(graph, output)["cmds"][-2]["cmd_args"]


def user_cflags(args):
    start = args.index(LAST_COMMON_DEFINE) + 1
    return args[start:args.index(FIRST_NO_LIBC_FLAG, start)]


def outputs_by_kind(graph, kind):
    return sorted(
        output
        for node in graph["graph"]
        if node["kv"]["p"] == kind
        for output in node["outputs"]
    )


def strip_vcs(graph):
    return [node for node in graph["graph"] if node["outputs"] != ["$(B)/vcs.json"]]


class YaMakeMacrosTest(unittest.TestCase):
    def make(self, files, target="a", *args, **kwargs):
        code, graph, stderr = ay_make(files, target, *args, **kwargs)
        self.assertEqual(code, 0, stderr)
        return graph

    def test_compile_and_link_switches(self):
        baseline = self.make({"a/ya.make": module("PROGRAM", ""), "a/x.cpp": ""})
        graph = self.make({
            "a/ya.make": module("PROGRAM", (
                "NO_COMPILER_WARNINGS()\nNO_EXPORT_DYNAMIC_SYMBOLS()\n"
                "CLANG_WARNINGS(-Wno-foo)\nYQL_LAST_ABI_VERSION()\nYQL_ABI_VERSION(2 3 4)"
            )),
            "a/x.cpp": "",
        })
        before = args_of(baseline, "$(B)/a/x.cpp.o")
        after = args_of(graph, "$(B)/a/x.cpp.o")
        self.assertIn("-Werror", before)
        self.assertNotIn("-Werror", after)
        self.assertEqual(after.count("-Wno-everything"), 2)
        for flag in (
            "-Wno-foo", "-DUSE_CURRENT_UDF_ABI_VERSION",
            "-DUDF_ABI_VERSION_MAJOR=2", "-DUDF_ABI_VERSION_MINOR=3",
            "-DUDF_ABI_VERSION_PATCH=4",
        ):
            self.assertNotIn(flag, before)
            self.assertIn(flag, after)
        self.assertIn("-rdynamic", link_args(baseline, "$(B)/a/a"))
        self.assertNotIn("-rdynamic", link_args(graph, "$(B)/a/a"))
        no_shadow = self.make({"a/ya.make": module("PROGRAM", "NO_WSHADOW()"), "a/x.cpp": ""})
        self.assertNotIn("-Wno-shadow", before)
        self.assertIn("-Wno-shadow", args_of(no_shadow, "$(B)/a/x.cpp.o"))

    def test_per_source_macros(self):
        body = "SRC(y.cpp -DSRC1)\nSRC_C_NO_LTO(z.c)\nSRC_C_AVX(y.cpp -DAV)\n" + "".join(
            f"SRC_C_{variant}(y.cpp)\n" for variant in SIMD_VARIANTS[1:]
        )
        files = {
            "a/ya.make": module("PROGRAM", body),
            "a/x.cpp": "", "a/y.cpp": "", "a/z.c": "",
        }
        graph = self.make(files)
        self.assertEqual(outputs_by_kind(graph, "CC"), sorted(
            ["$(B)/a/x.cpp.o", "$(B)/a/y.cpp.o", "$(B)/a/z.c.o"]
            + [f"$(B)/a/y.cpp.{variant.lower()}.o" for variant in SIMD_VARIANTS]
        ))
        self.assertIn("-DSRC1", args_of(graph, "$(B)/a/y.cpp.o"))
        avx = args_of(graph, "$(B)/a/y.cpp.avx.o")
        self.assertEqual(avx[avx.index("-mavx"):avx.index("-mavx") + 3], ["-mavx", "-mpclmul", "-DAV"])
        self.assertNotIn("-DSRC1", avx)
        self.assertIn("-msse4.1", args_of(graph, "$(B)/a/y.cpp.sse41.o"))
        host = self.make(files, platform="default-linux-x86_64")
        self.assertIn("-mcx16", args_of(host, "$(B)/a/y.cpp.avx.o"))

    def test_linker_plugin(self):
        graph = self.make({
            "a/ya.make": module("PROGRAM", "LD_PLUGIN(p.py)"),
            "a/x.cpp": "", "a/p.py": "",
        })
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/p.py.pyplugin")["inputs"], ["$(S)/a/p.py"],
        )
        self.assertIn("$(B)/a/p.py.pyplugin", link_args(graph, "$(B)/a/a"))

    def test_declare_in_dirs(self):
        files = {
            "a/ya.make": module("LIBRARY", (
                "DECLARE_IN_DIRS(D *.txt DIRS d1 d2 EXCLUDES skip.txt)\n"
                "DECLARE_IN_DIRS(R *.txt DIRS d1 RECURSIVE EXCLUDES other/*.txt)\n"
                "DECLARE_IN_DIRS(S *.txt DIRS sd SRCDIR s)\n"
                "DECLARE_IN_DIRS(T *.txt DIRS top SRCDIR ${ARCADIA_ROOT})\n"
                "CFLAGS(-DD ${D_FILES} -DDS=${D_SRCDIR} -DR ${R_FILES}"
                " -DS ${S_FILES} -DSS=${S_SRCDIR} -DT ${T_FILES} -DTS=${T_SRCDIR})"
            )),
            "a/x.cpp": "",
            "a/d1/a.txt": "", "a/d1/b.txt": "", "a/d1/skip.txt": "", "a/d1/c.cpp": "",
            "a/d1/sub/x.txt": "", "a/d1/sub/deeper/z.txt": "", "a/d2/c.txt": "",
            "a/s/sd/q.txt": "", "top/t.txt": "",
        }
        graph = self.make(files)
        self.assertEqual(user_cflags(args_of(graph, "$(B)/a/x.cpp.o")), [
            "-DD", "d1/a.txt", "d1/b.txt", "d2/c.txt", "-DDS=",
            "-DR", "d1/a.txt", "d1/b.txt", "d1/skip.txt", "d1/sub/x.txt",
            "d1/sub/deeper/z.txt",
            "-DS", "s/sd/q.txt", "-DSS=s",
            "-DT", "$(S)/top/t.txt", "-DTS=$(S)",
        ])

    def test_set_resource_uri_from_json(self):
        bundle = json.dumps({"by_platform": {
            "linux-aarch64": {"uri": "sbr:111"},
            "linux": {"uri": "sbr:222"},
            "darwin-arm64": {"uri": "sbr:333"},
        }})
        body = (
            "SET_RESOURCE_URI_FROM_JSON(LOCAL res.json)\n"
            "SET_RESOURCE_URI_FROM_JSON(ROOTED ${ARCADIA_ROOT}/shared/res.json)\n"
            "SET_RESOURCE_URI_FROM_JSON(ABSENT only_windows.json)\n"
            "CFLAGS(-DLOCAL=${LOCAL} -DROOTED=${ROOTED} -DABSENT=${ABSENT})"
        )
        files = {
            "a/ya.make": module("LIBRARY", body),
            "a/x.cpp": "",
            "a/res.json": bundle,
            "shared/res.json": bundle,
            "a/only_windows.json": json.dumps({"by_platform": {"win32": {"uri": "sbr:444"}}}),
        }
        self.assertEqual(user_cflags(args_of(self.make(files), "$(B)/a/x.cpp.o")), [
            "-DLOCAL=sbr:111", "-DROOTED=sbr:111", "-DABSENT=${ABSENT}",
        ])
        host = self.make(files, platform="default-linux-x86_64")
        self.assertEqual(user_cflags(args_of(host, "$(B)/a/x.cpp.o"))[:2], [
            "-DLOCAL=sbr:222", "-DROOTED=sbr:222",
        ])

    def test_copy_file_variants(self):
        graph = self.make({
            "a/ya.make": module("LIBRARY", (
                "COPY_FILE(data.txt out/data.txt)\n"
                "COPY_FILE(AUTO gen/src.cpp copied.cpp OUTPUT_INCLUDES a/inc.h)\n"
                "COPY_FILE(AUTO TEXT gen/t.h ${BINDIR}/t.h INDUCED_DEPS a/inc.h)\n"
                "COPY_FILE_WITH_CONTEXT(gen/w.cpp ${ARCADIA_BUILD_ROOT}/elsewhere/w.cpp)\n"
                "COPY_FILE(AUTO gen/src2.cpp ${ARCADIA_BUILD_ROOT}/other/c2.cpp)\n"
                "COPY(AUTO WITH_CONTEXT FROM gen f1.cpp f2.h OUTPUT_INCLUDES a/inc.h INDUCED_DEPS a/inc.h)\n"
                "COPY(FROM gen/../gen plain.txt)\n"
                "SRCS(dup.cpp)\n"
                "COPY(AUTO FROM gen dup.cpp)\n"
                "COPY_FILE(AUTO gen/dup.cpp dup.cpp)"
            )),
            "a/x.cpp": "", "a/data.txt": "", "a/inc.h": "", "a/dup.cpp": "",
            "a/gen/src.cpp": "", "a/gen/t.h": "", "a/gen/w.cpp": "",
            "a/gen/src2.cpp": "", "a/gen/f1.cpp": "", "a/gen/f2.h": "",
            "a/gen/plain.txt": "", "a/gen/dup.cpp": "",
        })
        self.assertEqual(outputs_by_kind(graph, "CP"), [
            "$(B)/a/copied.cpp", "$(B)/a/dup.cpp", "$(B)/a/f1.cpp", "$(B)/vcs.json",
        ])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/copied.cpp")["inputs"],
            ["$(S)/a/gen/src.cpp", "$(S)/a/inc.h"],
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/liba.a")["inputs"][:-1],
            ["$(B)/a/copied.cpp.o", "$(B)/a/f1.cpp.o", "$(B)/a/dup.cpp.o", "$(B)/a/x.cpp.o"],
        )

    def test_data_only_macros_leave_a_cpp_graph_unchanged(self):
        files = {"a/ya.make": module("PROGRAM", ""), "a/x.cpp": ""}
        baseline = strip_vcs(self.make(files))
        body = (
            "LICENSE(MIT)\nVERSION(1 2 ${UNSET_PART})\nTOOLCHAIN(clang)\nTOOLCHAIN(a b)\n"
            "MAVEN_GROUP_ID(x)\n"
            "STYLE_RUFF(CONFIG_TYPE x CHECK_FORMAT RUN_IN_SOURCE_ROOT)\n"
            "PRIMARY_OUTPUT(foo)\nPRIMARY_OUTPUT()\nSPLIT_DWARF()\nNO_SPLIT_DWARF()\n"
            "NO_PLATFORM_RESOURCES()\n"
            "GO_TEST_SRCS(a_test.go)\nGO_XTEST_SRCS(b_test.go)\n"
            "GO_SKIP_TESTS(test_x)\nGO_EMBED_PATTERN(x)\n"
            "BISON_GEN_C()\nBISON_GEN_CPP()\nBISON_FLAGS(-v)\nFLATC_FLAGS(--x)\n"
            "EXCLUDE_TAGS(GO_PROTO JAVA_PROTO X)\nYA_CONF_JSON(conf.json)\n"
            "NO_OPTIMIZE()\nNO_OPTIMIZE_PY_PROTOS()\nOPTIMIZE_PY_PROTOS()\n"
            "NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nNO_EXTENDED_SOURCE_SEARCH()\n"
            "ENABLE(MUSL_LITE PYBUILD_NO_PYC PYBUILD_NO_PY PY_PROTO_MYPY_ENABLED"
            " PYTHON_SQLITE3 USE_ASMLIB OTHER)\n"
            "DISABLE(PYTHON_SQLITE3 NEED_GOOGLE_PROTO_PEERDIRS USE_ASMLIB OTHER)\n"
            "SET_APPEND(SFLAGS -sf)\nSET_APPEND(_PROTOC_FLAGS --pf)\n"
            "CUDA_NVCC_FLAGS(-x)\nNO_MYPY()\nNO_CHECK_IMPORTS(foo.*)\nNO_CHECK_IMPORTS()\n"
            "PY_NAMESPACE(ns)\nFLATC_FLAGS(--y)\nOWNER(g:team)"
        )
        code, graph, stderr = ay_make({"a/ya.make": module("PROGRAM", body), "a/x.cpp": ""})
        self.assertEqual((code, stderr), (0, ""))
        self.assertEqual(strip_vcs(graph), baseline)


if __name__ == "__main__":
    unittest.main(verbosity=2)
