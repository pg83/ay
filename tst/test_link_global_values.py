"""Native postfix global values, including a Python program's library self-peer."""
import unittest

import lib


def command_with(node):
    return next(c["cmd_args"] for c in node["cmds"]
                if any(a.endswith(("link_exe.py", "link_dyn_lib.py")) for a in c["cmd_args"]))

BARE = lib.BARE_MODULE
LINKER = ["-Wl,linker", "-Wl,shared"]
SEED = ["-Wl,seed", "-Wl,shared"]
ELM = ["-Wl,elm", "-Wl,shared"]
FIR = ["-Wl,fir", "-Wl,shared"]
REPEAT = ["-Wl,repeat", "-Wl,repeat", "-Wl,tail"]
OWNER = ["-Wl,owner", "-Wl,shared"]


def fixture(kind="PROGRAM", peers="elm fir clone repeat", own=True):
    files = {}
    for path, body in (
        ("seed", "LDFLAGS(-Wl,seed -Wl,shared)\n"),
        ("elm", "PEERDIR(seed)\nLDFLAGS(-Wl,elm -Wl,shared)\n"),
        ("fir", "PEERDIR(seed)\nLDFLAGS(-Wl,fir -Wl,shared)\n"),
        ("clone", "LDFLAGS(-Wl,seed -Wl,shared)\n"),
        ("repeat", "LDFLAGS(-Wl,repeat -Wl,repeat)\nLDFLAGS(-Wl,tail)\n"),
    ):
        files[path + "/ya.make"] = f"LIBRARY()\n{BARE}{body}SRCS(unit.cpp)\nEND()\n"
        files[path + "/unit.cpp"] = "int witness;\n"
    files["build/platform/lld/ya.make"] = "RESOURCES_LIBRARY()\nLDFLAGS(-Wl,linker -Wl,shared)\nEND()\n"
    files["app/ya.make"] = (
        f"{kind}()\n{BARE}PEERDIR({peers})\nSRCS(unit.cpp)\n"
        + ("LDFLAGS(-Wl,owner -Wl,shared)\n" if own else "") + "END()\n"
    )
    files["app/unit.cpp"] = "int main() {return 0;}\n"
    for path in ("contrib/libs/python", "library/python/runtime_py3/main",
                 "library/python/import_tracing/constructor", "library/python/testing/import_test",
                 "contrib/tools/python3/Modules/_sqlite", "library/cpp/malloc/jemalloc"):
        files[path + "/ya.make"] = f"LIBRARY()\n{BARE}SRCS(native.cpp)\nEND()\n"
        files[path + "/native.cpp"] = "int runtime_witness;\n"
    return files


def values(node):
    return [a for a in command_with(node) if a.startswith("-Wl,") and a.split(",")[1] in
            ("linker", "shared", "seed", "elm", "fir", "repeat", "tail", "owner")]


class GlobalLinkValuesTest(unittest.TestCase):
    def check_graph(self, graph, expected, kind="PROGRAM"):
        output = "$(B)/app/" + ("libapp.so" if kind == "DLL" else "app")
        node = lib.node_by_output(graph, output)
        self.assertEqual(values(node), expected)
        ids = {n["uid"] for n in graph["graph"]}
        self.assertTrue(set(graph["result"]) <= ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))
        for name in ("seed", "elm", "fir", "clone", "repeat"):
            outputs = [n for n in graph["graph"] if n["outputs"] == [f"$(B)/{name}/lib{name}.a"]]
            if outputs:
                archive = outputs[0]
                self.assertIn(archive["uid"], node["deps"])
                self.assertIn(archive["outputs"][0], node["inputs"])
                source = next(n for n in graph["graph"] if n["uid"] in archive["deps"]
                              and n["kv"]["p"] == "CC" and f"$(S)/{name}/unit.cpp" in n["inputs"])
                self.assertEqual(source["inputs"][0], f"$(S)/{name}/unit.cpp")
                self.assertIn(source["uid"], archive["deps"])
        args = command_with(node)
        # EXTRALIBS is an independent global variable, after the entire LDFLAGS stream.
        if "-lextra_sentinel" in args:
            self.assertLess(args.index("-Wl,tail"), args.index("-lextra_sentinel"))
        return node

    def test_postfix_whole_values_and_local_repeated_tokens(self):
        files = fixture()
        files["repeat/ya.make"] = files["repeat/ya.make"].replace("END()", "EXTRALIBS(extra_sentinel)\nEND()")
        self.check_graph(lib.make(files, "app"), LINKER + SEED + ELM + FIR + REPEAT + OWNER)

    def test_reversed_siblings_preserve_diamond_value_identity(self):
        self.check_graph(lib.make(fixture(peers="fir elm clone repeat"), "app"),
                         LINKER + SEED + FIR + ELM + REPEAT + OWNER)

    def test_duplicate_owner_is_appended_after_peers(self):
        files = fixture(peers="seed clone", own=False)
        files["app/ya.make"] = files["app/ya.make"].replace("END()", "LDFLAGS(-Wl,seed -Wl,shared)\nEND()")
        self.check_graph(lib.make(files, "app"), LINKER + SEED + SEED)

    def test_explicit_linker_peer_keeps_implicit_first_position(self):
        for peers in ("build/platform/lld elm fir", "elm fir build/platform/lld"):
            with self.subTest(peers=peers):
                self.check_graph(lib.make(fixture(peers=peers, own=False), "app"), LINKER + SEED + ELM + FIR)

    def test_python_library_self_values_precede_linker_then_binary_own(self):
        for roots in ((), ("elm",), ("app", "elm")):
            with self.subTest(roots=roots):
                graph = lib.make(fixture("PY3_PROGRAM"), "app", *roots)
                node = self.check_graph(graph, SEED + ELM + FIR + REPEAT + OWNER + LINKER + OWNER)
                owner = lib.node_by_output(graph, "$(B)/app/libpy3app.a")
                self.assertIn(owner["uid"], node["deps"])
                self.assertIn(owner["outputs"][0], node["inputs"])

    def test_native_single_context_python_program_keeps_linker_first(self):
        self.check_graph(lib.make(fixture("PY3_PROGRAM_BIN"), "app"), LINKER + SEED + ELM + FIR + REPEAT + OWNER)

    def test_python_self_scalar_changes_order_without_changing_membership(self):
        files = fixture("PY3_PROGRAM", peers="seed", own=False)
        files["seed/ya.make"] = files["seed/ya.make"].replace("-Wl,seed -Wl,shared", "-Wl,seed")
        self.check_graph(lib.make(files, "app"), ["-Wl,seed"] + LINKER)

    def test_disabled_implicit_peer_and_library_consumer(self):
        files = fixture()
        files["app/ya.make"] = files["app/ya.make"].replace("END()", "SET(NEED_PLATFORM_PEERDIRS no)\nEND()")
        graph = lib.make(files, "app")
        self.check_graph(graph, SEED + ELM + FIR + REPEAT + OWNER)
        self.assertNotIn("-Wl,linker", command_with(lib.node_by_output(graph, "$(B)/app/app")))
        files = fixture(kind="LIBRARY")
        files["reader/ya.make"] = f"PROGRAM()\n{BARE}PEERDIR(app)\nSRCS(main.cpp)\nEND()\n"
        files["reader/main.cpp"] = "int main(){return 0;}\n"
        graph = lib.make(files, "reader")
        self.assertEqual(values(lib.node_by_output(graph, "$(B)/reader/reader")), LINKER + SEED + ELM + FIR + REPEAT + OWNER)

    def test_pic_dll_and_host_program_keep_native_contexts(self):
        files = fixture("DLL")
        self.check_graph(lib.make(files, "app"), LINKER + SEED + ELM + FIR + REPEAT + OWNER, kind="DLL")
        files = fixture()
        lib.tool_program(files, "tools/runner", "runner")
        files["tools/runner/ya.make"] = files["tools/runner/ya.make"].replace("END()", "PEERDIR(elm fir)\nLDFLAGS(-Wl,owner -Wl,shared)\nEND()")
        files["app/ya.make"] = files["app/ya.make"].replace("END()", "RUN_PROGRAM(tools/runner OUT result.cpp)\nEND()")
        graph = lib.make(files, "app")
        self.check_graph(graph, LINKER + SEED + ELM + FIR + REPEAT + OWNER)
        tool = lib.node_by_output(graph, "$(B)/tools/runner/runner")
        self.assertEqual(values(tool), LINKER + SEED + ELM + FIR + OWNER)
        generated = lib.node_by_output(graph, "$(B)/app/result.cpp")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(tool["outputs"][0], generated["inputs"])
        self.assertNotEqual(tool.get("platform"), lib.node_by_output(graph, "$(B)/app/app").get("platform"))


if __name__ == "__main__":
    unittest.main()
