"""Priority-2 SRC actions use native producer readiness, not input closure depth."""
import unittest

import lib
from test_proto_cpp import proto_files


def world(before=True, shadow=False, root="build", kind="proto", chain=False):
    files = proto_files()
    lib.tool_program(files, "tools/stencil", "stencil")
    prefix = {"build": "${BINDIR}/", "source": "${CURDIR}/", "relative": ""}[root]
    gen = f"RUN_PROGRAM(tools/stencil OUT_NOAUTO alpha.{kind} beta.{kind})\n"
    if chain:
        gen = (f"RUN_PROGRAM(tools/stencil IN ${{BINDIR}}/seed.txt OUT_NOAUTO alpha.{kind} beta.{kind})\n"
               "RUN_PROGRAM(tools/stencil OUT_NOAUTO seed.txt)\n")
    calls = f"SRC({prefix}alpha.{kind} -DSOURCE_ALPHA)\nSRC({prefix}beta.{kind} -DSOURCE_BETA)\n"
    files["ochre/ya.make"] = (
        f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{lib.BARE_MODULE}"
        + (gen + calls if before else calls + gen)
        + "SRCS(steady.proto native.cpp)\nEND()\n"
    )
    files["ochre/steady.proto"] = 'syntax = "proto3";\n'
    files["ochre/native.cpp"] = "int native;\n"
    if shadow:
        for name in ("alpha", "beta"):
            files[f"ochre/{name}.{kind}"] = 'syntax = "proto3";\n' if kind == "proto" else "int item;\n"
    return files


def objects(kind, pending=False):
    generated = [f"{n}.pb.cc.o" if kind == "proto" else f"{n}.cpp.o" for n in ("alpha", "beta")]
    if kind == "proto":
        return ["native.cpp.o", *(["steady.pb.cc.o"] + generated if pending else generated + ["steady.pb.cc.o"])]
    return ["native.cpp.o", *generated, "steady.pb.cc.o"] if pending else [*generated, "native.cpp.o", "steady.pb.cc.o"]


class ReadySrcQueueTest(unittest.TestCase):
    def check(self, graph, kind, pending=False, source=False):
        ar = lib.node_by_output(graph, "$(B)/ochre/libochre.a")
        expected = ["$(B)/ochre/" + p for p in objects(kind, pending)]
        self.assertEqual(ar["cmds"][0]["cmd_args"][10:], expected)
        self.assertEqual({p for p in ar["inputs"] if p.endswith(".o")}, set(expected))
        for output in expected:
            obj = lib.node_by_output(graph, output)
            self.assertIn(obj["uid"], ar["deps"])
            self.assertIn(output, ar["inputs"])
        for name in ("alpha", "beta"):
            out = f"$(B)/ochre/{name}.pb.h" if kind == "proto" else f"$(B)/ochre/{name}.cpp.o"
            node = lib.node_by_output(graph, out)
            self.assertIn(f"{'$(S)' if source else '$(B)'}/ochre/{name}.{kind}", node["inputs"])
            if not source:
                gen = lib.node_by_output(graph, f"$(B)/ochre/alpha.{kind}")
                self.assertEqual(gen["outputs"], [f"$(B)/ochre/alpha.{kind}", f"$(B)/ochre/beta.{kind}"])
                self.assertIn(gen["uid"], node["deps"])
                self.assertIn(f"$(B)/ochre/alpha.{kind}", node["inputs"])
                tool = lib.node_by_output(graph, "$(B)/tools/stencil/stencil")
                self.assertIn(tool["uid"], gen["deps"])
            if kind == "cpp":
                self.assertIn(f"-DSOURCE_{name.upper()}", node["cmds"][0]["cmd_args"])

    def test_ready_and_pending_generated_source_actions(self):
        for kind in ("proto", "cpp"):
            for before in (False, True):
                for root in ("relative", "build"):
                    with self.subTest(kind=kind, before=before, root=root):
                        graph = lib.make(world(before=before, root=root, kind=kind), "ochre")
                        self.check(graph, kind, pending=not before)

    def test_physical_and_explicit_roots_keep_their_selected_namespaces(self):
        for kind in ("proto", "cpp"):
            for before in (False, True):
                for root in ("source", "build"):
                    with self.subTest(kind=kind, before=before, root=root):
                        graph = lib.make(world(before=before, shadow=True, root=root, kind=kind), "ochre")
                        self.check(graph, kind, pending=root == "build" and not before, source=root == "source")

    def test_an_earlier_producer_with_a_later_input_still_waits_in_fifo(self):
        graph = lib.make(world(chain=True), "ochre")
        self.check(graph, "proto", pending=True)
        first = lib.node_by_output(graph, "$(B)/ochre/alpha.proto")
        seed = lib.node_by_output(graph, "$(B)/ochre/seed.txt")
        self.assertIn(seed["uid"], first["deps"])
        self.assertIn(seed["outputs"][0], first["inputs"])

    def test_graph_root_order_does_not_change_source_action_readiness(self):
        files = world()
        files["extra/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}SRCS(extra.cpp)\nEND()\n"
        files["extra/extra.cpp"] = "int extra;\n"
        for roots in (("extra", "ochre"), ("ochre", "extra")):
            with self.subTest(roots=roots):
                self.check(lib.make(files, *roots), "proto")

    def test_python_and_sandbox_producers_publish_the_same_queue_facts(self):
        for declaration in (
            "RUN_PYTHON3(stencil.py OUT_NOAUTO alpha.cpp beta.cpp)",
            "FROM_SANDBOX(313 OUT_NOAUTO alpha.cpp beta.cpp)",
        ):
            with self.subTest(declaration=declaration):
                files = world(kind="cpp")
                files["ochre/stencil.py"] = "# source input\n"
                files["ochre/ya.make"] = files["ochre/ya.make"].replace(
                    "RUN_PROGRAM(tools/stencil OUT_NOAUTO alpha.cpp beta.cpp)", declaration)
                graph = lib.make(files, "ochre")
                ar = lib.node_by_output(graph, "$(B)/ochre/libochre.a")
                self.assertEqual(ar["cmds"][0]["cmd_args"][10:], ["$(B)/ochre/" + p for p in objects("cpp")])
                produced = lib.node_by_output(graph, "$(B)/ochre/alpha.cpp")
                for name in ("alpha", "beta"):
                    cc = lib.node_by_output(graph, f"$(B)/ochre/{name}.cpp.o")
                    self.assertIn(produced["uid"], cc["deps"])
                    self.assertIn("$(B)/ochre/alpha.cpp", cc["inputs"])
                if declaration.startswith("RUN_PYTHON3"):
                    self.assertIn("$(S)/ochre/stencil.py", produced["inputs"])

    def test_real_gazetteer_auto_source_still_adds_two_waves(self):
        files = world()
        lib.tool_program(files, "dict/gazetteer/converter", "converter")
        files["kernel/gazetteer/proto/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
        files["ochre/raw.gztproto"] = "message Raw {}\n"
        files["ochre/ya.make"] = (
            f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{lib.BARE_MODULE}"
            "SRC(raw.gztproto)\nSRCS(steady.proto native.cpp)\nEND()\n"
        )
        graph = lib.make(files, "ochre")
        ar = lib.node_by_output(graph, "$(B)/ochre/libochre.a")
        self.assertEqual(ar["cmds"][0]["cmd_args"][10:],
                         ["$(B)/ochre/" + p for p in ("native.cpp.o", "steady.pb.cc.o", "raw.pb.cc.o")])
        converter = lib.node_by_output(graph, "$(B)/ochre/raw.proto")
        pb = lib.node_by_output(graph, "$(B)/ochre/raw.pb.h")
        self.assertIn(converter["uid"], pb["deps"])
        self.assertIn("$(S)/ochre/raw.gztproto", converter["inputs"])

    def test_global_source_proto_preserves_the_producer_fallback(self):
        files = world()
        files["ochre/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}SRCS(GLOBAL lone.proto native.cpp)\nEND()\n"
        files["ochre/lone.proto"] = 'syntax = "proto3";\n'
        graph = lib.make(files, "ochre")
        ar = lib.node_by_output(graph, "$(B)/ochre/libochre.global.a")
        cc = lib.node_by_output(graph, "$(B)/ochre/lone.pb.cc.o")
        pb = lib.node_by_output(graph, "$(B)/ochre/lone.pb.h")
        protoc = lib.node_by_output(graph, "$(B)/contrib/tools/protoc/protoc")
        self.assertEqual(ar["cmds"][0]["cmd_args"][10:], ["$(B)/ochre/lone.pb.cc.o"])
        self.assertIn(cc["uid"], ar["deps"])
        self.assertIn(pb["uid"], cc["deps"])
        self.assertIn(protoc["uid"], pb["deps"])
        self.assertIn("$(S)/ochre/lone.proto", pb["inputs"])

    def test_unrecorded_inputs_keep_source_and_producer_identity(self):
        for source in (True, False):
            with self.subTest(source=source):
                files = world(kind="cpp")
                body = "SRC(${CURDIR}/absent.cpp)" if source else "SRC(${BINDIR}/absent.cpp)"
                files["ochre/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}{body}\nSRCS(native.cpp)\nEND()\n"
                graph = lib.make(files, "ochre")
                cc = lib.node_by_output(graph, "$(B)/ochre/absent.cpp.o")
                self.assertIn(("$(S)" if source else "$(B)") + "/ochre/absent.cpp", cc["inputs"])
                self.assertEqual(cc["deps"], [])

        files = world(kind="cpp")
        files["ochre/input.cpp.in"] = "int value;\n"
        files["ochre/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}CONFIGURE_FILE(input.cpp.in configured.cpp)\n"
            "SRC(${BINDIR}/configured.cpp)\nSRCS(native.cpp)\nEND()\n"
        )
        graph = lib.make(files, "ochre")
        cc = lib.node_by_output(graph, "$(B)/ochre/configured.cpp.o")
        cf = lib.node_by_output(graph, "$(B)/ochre/configured.cpp")
        self.assertIn(cf["uid"], cc["deps"])
        self.assertIn("$(S)/ochre/input.cpp.in", cf["inputs"])

    def test_a_registered_peer_producer_is_available_in_the_consumer(self):
        files = world(kind="cpp")
        files["peer/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(tools/stencil OUT_NOAUTO shared.cpp)\nEND()\n"
        )
        files["ochre/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(peer)\n"
            "SRC(${ARCADIA_BUILD_ROOT}/peer/shared.cpp)\nSRCS(native.cpp)\nEND()\n"
        )
        graph = lib.make(files, "ochre")
        ar = lib.node_by_output(graph, "$(B)/ochre/libochre.a")
        cc = next(n for n in graph["graph"] if n.get("kv", {}).get("p") == "CC" and "$(B)/peer/shared.cpp" in n["inputs"])
        self.assertEqual(ar["cmds"][0]["cmd_args"][10:], [cc["outputs"][0], "$(B)/ochre/native.cpp.o"])
        generated = lib.node_by_output(graph, "$(B)/peer/shared.cpp")
        self.assertIn(generated["uid"], cc["deps"])
        self.assertIn(cc["uid"], ar["deps"])

    def test_a_physical_primary_input_does_not_wait_for_its_generated_import(self):
        files = world(before=False, shadow=True)
        del files["ochre/beta.proto"]
        files["ochre/alpha.proto"] = 'syntax = "proto3";\nimport "ochre/beta.proto";\n'
        files["ochre/ya.make"] = files["ochre/ya.make"].replace(
            "SRC(${BINDIR}/alpha.proto", "SRC(${CURDIR}/alpha.proto")
        graph = lib.make(files, "ochre")
        ar = lib.node_by_output(graph, "$(B)/ochre/libochre.a")
        self.assertEqual(ar["cmds"][0]["cmd_args"][10:], ["$(B)/ochre/" + p for p in (
            "native.cpp.o", "alpha.pb.cc.o", "steady.pb.cc.o", "beta.pb.cc.o")])
        pb = lib.node_by_output(graph, "$(B)/ochre/alpha.pb.h")
        generated = lib.node_by_output(graph, "$(B)/ochre/alpha.proto")
        self.assertIn("$(S)/ochre/alpha.proto", pb["inputs"])
        self.assertIn("$(B)/ochre/beta.proto", pb["inputs"])
        self.assertIn(generated["uid"], pb["deps"])

    def test_cyclic_source_actions_preserve_existing_cycle_diagnostics(self):
        for python in (False, True):
            files = world(kind="cpp")
            body = (
                "RUN_PYTHON3(${BINDIR}/second.py OUT_NOAUTO first.py alpha.cpp)\n"
                "RUN_PYTHON3(${BINDIR}/first.py OUT_NOAUTO second.py beta.cpp)\n"
                if python else
                "RUN_PROGRAM(tools/stencil IN ${BINDIR}/beta.cpp OUT_NOAUTO alpha.cpp)\n"
                "RUN_PROGRAM(tools/stencil IN ${BINDIR}/alpha.cpp OUT_NOAUTO beta.cpp)\n"
            )
            files["ochre/ya.make"] = (
                f"LIBRARY()\n{lib.BARE_MODULE}" + body +
                "SRC(${BINDIR}/alpha.cpp)\nSRCS(native.cpp)\nEND()\n"
            )
            with self.subTest(python=python):
                result = lib.make_process(files, "ochre")
                self.assertNotEqual(result.returncode, 0)
                diagnostic = "dependency cycle among generating macros"
                self.assertIn(diagnostic, result.stderr)

    def test_host_and_target_source_queues_are_independent(self):
        files = world(kind="cpp")
        lib.tool_program(files, "tools/consumer", "consumer")
        files["tools/consumer/ya.make"] = files["tools/consumer/ya.make"].replace("END()", "PEERDIR(ochre)\nEND()")
        files["caller/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(tools/consumer OUT body.cpp)\nEND()\n"
        graph = lib.make(files, "caller", "ochre")
        archives = [n for n in graph["graph"] if "$(B)/ochre/libochre.a" in n["outputs"]]
        self.assertEqual({n["platform"] for n in archives}, {"default-linux-x86_64", "default-linux-aarch64"})
        for ar in archives:
            host = ar["platform"] == "default-linux-x86_64"
            suffix = ".pic.o" if host else ".o"
            self.assertEqual(ar["cmds"][0]["cmd_args"][10:],
                             ["$(B)/ochre/" + p + suffix for p in ("alpha.cpp", "beta.cpp", "native.cpp", "steady.pb.cc")])
            for output in ar["cmds"][0]["cmd_args"][10:]:
                cc = next(n for n in graph["graph"] if output in n["outputs"] and n["platform"] == ar["platform"])
                self.assertIn(cc["uid"], ar["deps"])
        host_archive = next(n for n in archives if n["platform"] == "default-linux-x86_64")
        tool = lib.node_by_output(graph, "$(B)/tools/consumer/consumer")
        generated = lib.node_by_output(graph, "$(B)/caller/body.cpp")
        self.assertIn(host_archive["uid"], tool["deps"])
        self.assertIn(tool["uid"], generated["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
