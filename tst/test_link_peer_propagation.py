import unittest

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
X86_64 = ("--target-platform", "default-linux-x86_64")


def library(body, platform=NO_PLATFORM):
    return f"LIBRARY()\n{platform}{body}END()\n"


def link_args(graph, output):
    node = lib.node_by_output(graph, output)
    return next(
        cmd["cmd_args"]
        for cmd in node["cmds"]
        if any(arg.endswith("link_exe.py") for arg in cmd["cmd_args"])
    )


def link_group(args):
    return args[args.index("-Wl,--start-group") + 1:args.index("-Wl,--end-group")]


def includes(graph, output):
    return [
        arg
        for arg in lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]
        if arg.startswith("-I")
    ]


def closure_fixture():
    files = {
        "a/ya.make": library(
            "SRCS(GLOBAL g.cpp a.cpp)\nADDINCL(ONE_LEVEL a/inc)\nLD_PLUGIN(pa.py)\n"
        ),
        "a/g.cpp": "int g;\n",
        "a/a.cpp": "int a;\n",
        "a/pa.py": "\n",
        "a/inc/x.h": "\n",
        "a2/ya.make": library(
            "SRCS(a2.cpp)\nADDINCL(ONE_LEVEL a2/inc GLOBAL a2/inc)\n"
        ),
        "a2/a2.cpp": "int a2;\n",
        "a2/inc/y.h": "\n",
        "b/ya.make": library(
            "SRCS(b.cpp)\nPEERDIR(a a2 dyn a)\nADDINCL(${ARCADIA_BUILD_ROOT}/b)\n"
        ),
        "b/b.cpp": "int b;\n",
        "c/ya.make": library("SRCS(c.cpp)\nADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/b)\n"),
        "c/c.cpp": "int c;\n",
        "dyn/ya.make": (
            f"DYNAMIC_LIBRARY(d)\n{NO_PLATFORM}EXPORTS_SCRIPT(d.exports)\n"
            "DYNAMIC_LIBRARY_FROM(c)\nEND()\n"
        ),
        "dyn/d.exports": "{};\n",
        "build/platform/local_so/ya.make": library(""),
        "p/ya.make": (
            f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(b)\nLD_PLUGIN(pp.py)\nEND()\n"
        ),
        "p/m.cpp": "int main(){return 0;}\n",
        "p/pp.py": "\n",
    }
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files


def defaults_fixture():
    return {
        "contrib/libs/cxxsupp/libcxx/ya.make": library(
            "SRCS(x.cpp)\n"
            "ADDINCL(GLOBAL contrib/libs/cxxsupp/libcxx/include)\n"
            "CXXFLAGS(GLOBAL -nostdinc++)\n"
            "PEERDIR(contrib/libs/cxxsupp/builtins)\n"
        ),
        "contrib/libs/cxxsupp/builtins/ya.make": library(
            "ADDINCL(GLOBAL contrib/libs/cxxsupp/builtins/include)\n"
        ),
        "contrib/libs/cxxsupp/builtins/include/b.h": "\n",
        "contrib/libs/cxxsupp/libcxx/x.cpp": "int x;\n",
        "contrib/libs/cxxsupp/libcxx/include/v": "\n",
        "build/cow/on/ya.make": library(
            "SRCS(cow.cpp)\nADDINCL(GLOBAL build/cow/on/include)\n"
        ),
        "build/cow/on/cow.cpp": "int cow;\n",
        "build/cow/on/include/c.h": "\n",
        "library/cpp/cpuid_check/ya.make": library("SRCS(cc.cpp)\n"),
        "library/cpp/cpuid_check/cc.cpp": "int cc;\n",
        "library/cpp/malloc/jemalloc/ya.make": library("SRCS(j.cpp)\n"),
        "library/cpp/malloc/jemalloc/j.cpp": "int j;\n",
        "library/cpp/malloc/api/ya.make": library("SRCS(api.cpp)\n", "NO_UTIL()\n"),
        "library/cpp/malloc/api/api.cpp": "int api;\n",
        "library/cpp/testing/unittest_main/ya.make": library(""),
        "arp/ya.make": library("SRCS(z.cpp)\nAR_PLUGIN(arplug)\n"),
        "arp/z.cpp": "int z;\n",
    }


LIBCXX = "contrib/libs/cxxsupp/libcxx/liblibs-cxxsupp-libcxx.a"
COW = "build/cow/on/libbuild-cow-on.a"
JEMALLOC = "library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a"
CPUID = "library/cpp/cpuid_check/liblibrary-cpp-cpuid_check.a"


class PeerPropagationTest(unittest.TestCase):
    def test_transitive_global_dynamic_plugin_and_include_closures(self):
        graph = lib.make(closure_fixture(), "p")
        args = link_args(graph, "$(B)/p/p")
        self.assertEqual(
            args[args.index("--start-plugins"):args.index("--end-plugins") + 1],
            ["--start-plugins", "$(B)/p/pp.py.pyplugin", "$(B)/a/pa.py.pyplugin", "--end-plugins"],
        )
        self.assertEqual(
            args[args.index("--ya-start-command-file") + 1:args.index("--ya-end-command-file")],
            ["a/liba.global.a"],
        )
        self.assertEqual(
            link_group(args),
            ["a/liba.a", "a2/liba2.a", "dyn/libd.so", "b/libb.a"],
        )
        program = lib.node_by_output(graph, "$(B)/p/p")
        self.assertEqual(program["outputs"], ["$(B)/p/p", "$(B)/p/libd.so"])

        self.assertEqual(
            includes(graph, "$(B)/b/b.cpp.o"),
            ["-I$(B)", "-I$(S)", "-I$(B)/b", "-I$(S)/a/inc", "-I$(S)/a2/inc"],
        )
        self.assertEqual(
            includes(graph, "$(B)/p/m.cpp.o"),
            ["-I$(B)", "-I$(S)", "-I$(S)/a2/inc", "-I$(B)/b"],
        )

    def test_default_peers_are_deduplicated_against_unittest_peer(self):
        cases = {
            "build/cow/on": [LIBCXX, COW, JEMALLOC, CPUID],
            "library/cpp/malloc/jemalloc": [LIBCXX, JEMALLOC, COW, CPUID],
            "library/cpp/cpuid_check": [LIBCXX, CPUID, COW, JEMALLOC],
            "contrib/libs/cxxsupp/libcxx": [LIBCXX, COW, JEMALLOC, CPUID],
        }
        for peer, expected in cases.items():
            with self.subTest(peer=peer):
                files = defaults_fixture()
                files["ut/ya.make"] = (
                    f"UNITTEST_FOR({peer})\nALLOCATOR(J)\nSRCS(t.cpp)\nEND()\n"
                )
                files[f"{peer}/t.cpp"] = "int t;\n"
                graph = lib.make(files, "ut", *X86_64)
                self.assertEqual(link_group(link_args(graph, "$(B)/ut/ut")), expected)
                test_object = next(
                    node for node in graph["graph"]
                    if node["outputs"][0].startswith("$(B)/ut/") and node["kv"]["p"] == "CC"
                )
                self.assertEqual(
                    [arg for arg in test_object["cmds"][0]["cmd_args"] if arg.startswith("-I$(S)/")],
                    [
                        "-I$(S)/contrib/libs/cxxsupp/libcxx/include",
                        "-I$(S)/contrib/libs/cxxsupp/builtins/include",
                        "-I$(S)/build/cow/on/include",
                    ],
                )

    def test_malloc_api_forces_nostdinc_once(self):
        graph = lib.make(defaults_fixture(), "library/cpp/malloc/api", *X86_64)
        args = lib.node_by_output(graph, "$(B)/library/cpp/malloc/api/api.cpp.o")["cmds"][0]["cmd_args"]
        self.assertEqual(args.count("-nostdinc++"), 2)

        files = defaults_fixture()
        files["contrib/libs/cxxsupp/libcxx/ya.make"] = library("SRCS(x.cpp)\n")
        graph = lib.make(files, "library/cpp/malloc/api", *X86_64)
        args = lib.node_by_output(graph, "$(B)/library/cpp/malloc/api/api.cpp.o")["cmds"][0]["cmd_args"]
        self.assertEqual(args.count("-nostdinc++"), 2)
        self.assertLess(
            args.index("-nostdinc++"),
            args.index("$(S)/library/cpp/malloc/api/api.cpp"),
        )

    def test_ar_plugin_is_passed_to_archiver(self):
        graph = lib.make(defaults_fixture(), "arp")
        archive = lib.node_by_output(graph, "$(B)/arp/libarp.a")
        args = archive["cmds"][0]["cmd_args"]
        self.assertEqual(
            args[args.index("--"):],
            ["--", "--plugin", "$(S)/arp/arplug.pyplugin", "--",
             "$(B)/arp/libarp.a", "$(B)/arp/z.cpp.o"],
        )
        self.assertIn("$(S)/arp/arplug.pyplugin", archive["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
