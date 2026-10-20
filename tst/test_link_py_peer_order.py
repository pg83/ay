import unittest

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
PEERS = [
    "contrib/libs/python",
    "library/python/runtime_py3",
    "library/python/runtime_py3/main",
    "library/python/import_tracing/constructor",
    "library/python/testing/import_test",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/python/runtime/main",
    "contrib/deprecated/python/subprocess32",
    "contrib/tools/python/src/Modules/_sqlite",
    "library/python/pytest",
    "yql/essentials/tests/common/udf_test",
    "userlib",
]


def fixture():
    files = {}
    for path in PEERS:
        name = path.replace("/", "_")
        files[f"{path}/ya.make"] = (
            f"LIBRARY()\n{NO_PLATFORM}SRCS({name}.cpp)\nEND()\n"
        )
        files[f"{path}/{name}.cpp"] = f"int {name};\n"
    files["py3/ya.make"] = f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(userlib)\nEND()\n"
    files["py3min/ya.make"] = (
        f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(userlib)\n"
        "DISABLE(PYTHON_SQLITE3)\nNO_IMPORT_TRACING()\nNO_CHECK_IMPORTS()\n"
        "SET(NO_STRIP yes)\nEND()\n"
    )
    files["py3bin/ya.make"] = (
        f"PY3_PROGRAM_BIN(pb)\n{NO_PLATFORM}PEERDIR(userlib)\nEND()\n"
    )
    files["py2/ya.make"] = (
        f"PY2_PROGRAM()\n{NO_PLATFORM}"
        "PEERDIR(userlib library/python/runtime_py3)\nEND()\n"
    )
    for test in ("PY3TEST", "YQL_UDF_TEST"):
        files[f"{test.lower()}/ya.make"] = f"{test}()\n{NO_PLATFORM}PEERDIR(userlib)\nEND()\n"
    files["pylib/ya.make"] = f"PY3_LIBRARY()\n{NO_PLATFORM}SRCS(l.cpp)\nEND()\n"
    files["pylib/l.cpp"] = "int l;\n"
    files["pylibnoincl/ya.make"] = (
        f"PY3_LIBRARY()\n{NO_PLATFORM}SRCS(n.cpp)\nNO_PYTHON_INCLUDES()\nEND()\n"
    )
    files["pylibnoincl/n.cpp"] = "int n;\n"
    for app, peer in (("app", "pylib"), ("appnoincl", "pylibnoincl")):
        files[f"{app}/ya.make"] = (
            f"PROGRAM()\n{NO_PLATFORM}SRCS(main.cpp)\nPEERDIR({peer})\nEND()\n"
        )
        files[f"{app}/main.cpp"] = "int main(){return 0;}\n"
    return files


def link_args(graph, output):
    node = lib.node_by_output(graph, output)
    return next(
        cmd["cmd_args"]
        for cmd in node["cmds"]
        if any(arg.endswith("link_exe.py") for arg in cmd["cmd_args"])
    )


def link_group(args):
    return args[args.index("-Wl,--start-group") + 1:args.index("-Wl,--end-group")]


PYTHON = "contrib/libs/python/libcontrib-libs-python.a"
RUNTIME = "library/python/runtime_py3/liblibrary-python-runtime_py3.a"
MAIN = "library/python/runtime_py3/main/libpython-runtime_py3-main.a"
TRACING = (
    "library/python/import_tracing/constructor/"
    "libpython-import_tracing-constructor.a"
)
IMPORT_TEST = "library/python/testing/import_test/libpython-testing-import_test.a"
SQLITE = "contrib/tools/python3/Modules/_sqlite/libpython3-Modules-_sqlite.a"
JEMALLOC = "library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a"
PY2_MAIN = "library/python/runtime/main/libpython-runtime-main.a"
SUBPROCESS32 = "contrib/deprecated/python/subprocess32/libdeprecated-python-subprocess32.a"
PY2_SQLITE = "contrib/tools/python/src/Modules/_sqlite/libsrc-Modules-_sqlite.a"
USERLIB = "userlib/libuserlib.a"
PYTEST = "library/python/pytest/liblibrary-python-pytest.a"
UDF_TEST = "yql/essentials/tests/common/udf_test/libtests-common-udf_test.a"


class PythonPeerOrderTest(unittest.TestCase):
    def test_common_link_peers_follow_python_language_peers(self):
        files = fixture()
        for path in ("contrib/libs/libeatmydata", "build/cow/on"):
            files[path + "/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(x.cpp)\nEND()\n"
            files[path + "/x.cpp"] = ""
        graph = lib.make(files, "py3", "-DAUTOCHECK=yes")
        group = link_group(link_args(graph, "$(B)/py3/py3"))
        self.assertEqual(group[:4], [PYTHON, USERLIB,
            "contrib/libs/libeatmydata/libcontrib-libs-libeatmydata.a",
            "build/cow/on/libbuild-cow-on.a"])

    def test_py3_program_defers_runtime_peers(self):
        graph = lib.make(fixture(), "py3")
        args = link_args(graph, "$(B)/py3/py3")
        self.assertEqual(
            link_group(args),
            [PYTHON, USERLIB, JEMALLOC, SQLITE, MAIN, TRACING, IMPORT_TEST],
        )
        self.assertNotIn("-Wl,--strip-all", args)

    def test_py3_program_without_optional_runtime_peers(self):
        graph = lib.make(fixture(), "py3min")
        args = link_args(graph, "$(B)/py3min/py3min")
        self.assertEqual(link_group(args), [PYTHON, USERLIB, JEMALLOC, MAIN])
        self.assertNotIn("-Wl,--strip-all", args)

    def test_py3_program_strips_only_unsanitized_non_debug_builds(self):
        for flags, strip in (
            ((), False),
            (("--build", "fastdebug"), False),
            (("-r",), True),
            (("-r", "-DSANITIZER_TYPE=address"), False),
        ):
            args = link_args(lib.make(fixture(), "py3", *flags), "$(B)/py3/py3")
            self.assertEqual("-Wl,--strip-all" in args, strip, flags)

    def test_py3_program_bin_splices_runtime_after_python(self):
        graph = lib.make(fixture(), "py3bin")
        args = link_args(graph, "$(B)/py3bin/pb")
        self.assertEqual(
            link_group(args),
            [JEMALLOC, SQLITE, MAIN, TRACING, USERLIB, IMPORT_TEST, PYTHON],
        )
        self.assertNotIn("-Wl,--strip-all", args)

    def test_py3_test_peers_pytest_before_the_program_runtime(self):
        for test, extra in (("py3test", []), ("yql_udf_test", [UDF_TEST])):
            with self.subTest(test=test):
                files = fixture()
                for tool in ("tools/archiver", "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler", "tools/rescompressor",
                             "yql/essentials/tools/astdiff", "yql/tools/yqlrun", "yql/essentials/tools/udf_resolver"):
                    lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
                graph = lib.make(files, test, "-t")
                peers = [path[len("$(B)/"):] for path in lib.node_by_output(graph, f"$(B)/{test}/{test}")["inputs"] if path.endswith(".a")]
                expected = [PYTEST, SQLITE, MAIN, TRACING, *extra, USERLIB, IMPORT_TEST]
                self.assertEqual([peer for peer in peers if peer in expected], expected)

    def test_py2_program_peers_its_runtime_before_user_peers(self):
        graph = lib.make(fixture(), "py2")
        args = link_args(graph, "$(B)/py2/py2")
        self.assertEqual(
            link_group(args),
            [JEMALLOC, PY2_MAIN, SUBPROCESS32, PY2_SQLITE, TRACING, USERLIB, IMPORT_TEST, PYTHON, RUNTIME],
        )
        self.assertNotIn("-Wl,--strip-all", args)

    def test_python_libraries_peer_contrib_python_unless_disabled(self):
        graph = lib.make(fixture(), "app")
        self.assertEqual(
            link_group(link_args(graph, "$(B)/app/app")),
            [PYTHON, "pylib/libpy3pylib.a"],
        )
        graph = lib.make(fixture(), "appnoincl")
        self.assertEqual(
            link_group(link_args(graph, "$(B)/appnoincl/appnoincl")),
            ["pylibnoincl/libpy3pylibnoincl.a"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
