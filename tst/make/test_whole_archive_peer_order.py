import unittest

from tst import lib
from tst.make.test_link_proto_peers import between, fixture as proto_fixture, link_args


def fixture():
    files = proto_fixture()
    for name, peers, excluded in (
        ("seed", "", False),
        ("first", "seed", False),
        ("ghost", "seed", True),
        ("tail", "seed", False),
    ):
        files[f"{name}/ya.make"] = (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}"
            + ("EXCLUDE_TAGS(CPP_PROTO)\n" if excluded else "")
            + f"PEERDIR({peers})\nSRCS(note.proto)\nEND()\n"
        )
        files[f"{name}/note.proto"] = 'syntax = "proto3";\nmessage Note {}\n'
    files["ordinary/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}SRCS(value.cpp)\nEND()\n"
    files["ordinary/value.cpp"] = "int value;\n"
    for name, peers in (("left", "ordinary first ghost"), ("right", "ghost tail first")):
        files[f"{name}/ya.make"] = (
            f"PY3_LIBRARY()\n{lib.BARE_MODULE}PEERDIR({peers})\nPY_SRCS(value.py)\nEND()\n"
        )
        files[f"{name}/value.py"] = "value = 1\n"
    for name, peers in (
        ("runner", "left right"),
        ("reversed", "right left"),
        ("direct", "first ghost tail"),
    ):
        files[f"{name}/ya.make"] = f"PY3_PROGRAM()\n{lib.BARE_MODULE}PEERDIR({peers})\nEND()\n"
    files["plain/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(ordinary first)\nSRCS(main.cpp)\nEND()\n"
    files["plain/main.cpp"] = "int main() {}\n"
    return files


def whole(args):
    return [args[i + 1] for i, value in enumerate(args) if value == "--whole-archive-libs"]


def archive(name):
    return f"{name}/lib{name}.a"


def globals_for(names):
    return [f"{name}/libpy3{name}.global.a" for name in names]


class WholeArchivePeerOrderTest(unittest.TestCase):
    def check_plan(self, graph, target, order, global_order):
        node = lib.node_by_output(graph, f"$(B)/{target}/{target}")
        args = link_args(graph, f"$(B)/{target}/{target}")
        self.assertEqual(whole(args), [archive(name) for name in order])
        self.assertEqual(
            between(args, "--ya-start-command-file", "--ya-end-command-file"),
            globals_for(global_order),
        )
        # Whole-archive values don't turn excluded C++ siblings into producers.
        self.assertNotIn("$(B)/ghost/libghost.a", node["inputs"])
        self.assertFalse(any("$(B)/ghost/libghost.a" in n["outputs"] for n in graph["graph"]))
        for name in ("seed", "first", "tail"):
            produced = lib.node_by_output(graph, f"$(B)/{archive(name)}")
            self.assertIn(produced["uid"], node["deps"])
            self.assertIn(f"$(B)/{archive(name)}", node["inputs"])
            compiled = lib.node_by_output(graph, f"$(B)/{name}/note.pb.cc.o")
            pb = lib.node_by_output(graph, f"$(B)/{name}/note.pb.h")
            protoc = lib.node_by_output(graph, "$(B)/contrib/tools/protoc/protoc")
            self.assertIn(compiled["uid"], produced["deps"])
            self.assertIn(pb["uid"], compiled["deps"])
            self.assertIn(protoc["uid"], pb["deps"])
            self.assertIn(f"$(S)/{name}/note.proto", pb["inputs"])
            self.assertEqual(protoc["platform"], "default-linux-x86_64")
            self.assertEqual(pb["platform"], "default-linux-aarch64")
        for path in globals_for(global_order):
            produced = lib.node_by_output(graph, f"$(B)/{path}")
            self.assertIn(produced["uid"], node["deps"])
            self.assertIn(f"$(B)/{path}", node["inputs"])
        return args

    def test_direct_peers_keep_command_only_values_between_produced_values(self):
        graph = lib.make(fixture(), "direct")
        self.check_plan(graph, "direct", ["seed", "first", "ghost", "tail"], ["seed", "first", "ghost", "tail"])

    def test_diamond_merges_globals_in_peer_order_with_stable_deduplication(self):
        graph = lib.make(fixture(), "runner")
        args = self.check_plan(graph, "runner", ["seed", "first", "ghost", "tail"], ["seed", "first", "ghost", "left", "tail", "right"])
        normal = between(args, "-Wl,--start-group", "-Wl,--end-group")
        self.assertEqual(normal, [archive(n) for n in ("ordinary", "seed", "first")]
                         + ["contrib/python/protobuf/libcontrib-python-protobuf.a", archive("tail")])
        self.assertEqual(len(whole(args)), len(set(whole(args))))
        ordinary = lib.node_by_output(graph, "$(B)/ordinary/libordinary.a")
        compiled = lib.node_by_output(graph, "$(B)/ordinary/value.cpp.o")
        linked = lib.node_by_output(graph, "$(B)/runner/runner")
        self.assertIn(compiled["uid"], ordinary["deps"])
        self.assertIn(ordinary["uid"], linked["deps"])
        self.assertIn("$(B)/ordinary/libordinary.a", linked["inputs"])

    def test_reversing_direct_branches_changes_the_first_visit_order(self):
        graph = lib.make(fixture(), "reversed")
        self.check_plan(graph, "reversed", ["seed", "ghost", "tail", "first"], ["seed", "ghost", "tail", "first", "right", "left"])

    def test_extra_graph_roots_do_not_reorder_the_consumer_closure(self):
        for roots in (("right", "runner"), ("runner", "right")):
            with self.subTest(roots=roots):
                graph = lib.make(fixture(), *roots)
                self.check_plan(graph, "runner", ["seed", "first", "ghost", "tail"], ["seed", "first", "ghost", "left", "tail", "right"])

    def test_cpp_consumers_keep_protos_as_ordinary_archives(self):
        graph = lib.make(fixture(), "plain")
        args = link_args(graph, "$(B)/plain/plain")
        self.assertEqual(whole(args), [])
        self.assertEqual(between(args, "--ya-start-command-file", "--ya-end-command-file"), [])
        self.assertEqual(between(args, "-Wl,--start-group", "-Wl,--end-group"),
                         [archive(n) for n in ("ordinary", "seed", "first")])

    def test_peer_flags_and_local_global_archives_remain_separate_inputs(self):
        files = fixture()
        files["runner/ya.make"] = files["runner/ya.make"].replace("END()", "SRCS(GLOBAL embedded.a)\nEND()")
        files["runner/embedded.a"] = "archive payload\n"
        graph = lib.make(files, "runner")
        node = lib.node_by_output(graph, "$(B)/runner/runner")
        self.assertEqual(whole(link_args(graph, "$(B)/runner/runner")),
                         [archive(n) for n in ("seed", "first", "ghost", "tail")] + ["runner/embedded.a"])
        self.assertIn("$(S)/runner/embedded.a", node["inputs"])

    def test_host_and_target_producer_availability_does_not_partition_flags(self):
        files = fixture()
        files["ghost/ya.make"] = files["ghost/ya.make"].replace(
            "EXCLUDE_TAGS(CPP_PROTO)", "IF(ARCH_X86_64)\nEXCLUDE_TAGS(CPP_PROTO)\nENDIF()"
        )
        files["caller/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(runner OUT_NOAUTO receipt.cpp)\nSRCS(receipt.cpp)\nEND()\n"
        )
        graph = lib.make(files, "caller", "runner")
        runners = [n for n in graph["graph"] if "$(B)/runner/runner" in n["outputs"]]
        self.assertEqual({n["platform"] for n in runners}, {"default-linux-aarch64", "default-linux-x86_64"})
        for node in runners:
            with self.subTest(platform=node["platform"]):
                args = next(c["cmd_args"] for c in node["cmds"] if any(a.endswith("link_exe.py") for a in c["cmd_args"]))
                self.assertEqual(whole(args), [archive(n) for n in ("seed", "first", "ghost", "tail")])
                self.assertEqual("$(B)/ghost/libghost.a" in node["inputs"], node["platform"] == "default-linux-aarch64")
                for name in ("seed", "first", "tail"):
                    producer = next(n for n in graph["graph"] if f"$(B)/{archive(name)}" in n["outputs"] and n["platform"] == node["platform"])
                    self.assertIn(producer["uid"], node["deps"])
        generated = lib.node_by_output(graph, "$(B)/caller/receipt.cpp")
        host = next(n for n in runners if n["platform"] == "default-linux-x86_64")
        self.assertIn(host["uid"], generated["deps"])
        self.assertIn("$(B)/runner/runner", generated["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
