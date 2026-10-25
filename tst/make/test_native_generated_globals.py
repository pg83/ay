import unittest

from tst import lib
from tst.make.test_proto_cpp import proto_files
from tst.make.test_yql import udf_fixture


WRITER = "sdg/tier0/tools/bip/writer"


def library(body):
    return f"LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n"


def world():
    files = proto_files()
    lib.tool_program(files, WRITER, "metadata-writer")
    files[f"{WRITER}/ya.make"] = files[f"{WRITER}/ya.make"].replace("END()", "PEERDIR(telemetry/schema)\nEND()")
    files[f"{WRITER}/main.cpp"] = '#include "telemetry/schema/message.pb.h"\nint main() {}\n'
    files["telemetry/schema/ya.make"] = "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(message.proto)\nEND()\n"
    files["telemetry/schema/message.proto"] = 'syntax = "proto3";\nmessage Event {}\n'
    return files


def closure(graph, node):
    by_uid = {entry["uid"]: entry for entry in graph["graph"]}
    seen, pending = set(), [node["uid"]]
    while pending:
        uid = pending.pop()
        if uid not in seen:
            seen.add(uid)
            pending.extend(by_uid[uid]["deps"])
    return [by_uid[uid] for uid in seen]


class NativeGeneratedGlobalsTest(unittest.TestCase):
    def test_yql_static_contexts_retain_own_nonparsed_global_results(self):
        for dependency in (False, True):
            for tag in ("YQL_UDF_STATIC", "YQL_UDF_STATIC_CURRENT"):
                with self.subTest(dependency=dependency, tag=tag):
                    files = udf_fixture()
                    files["filters/ya.make"] = (
                        f"YQL_UDF(filter)\n{lib.BARE_MODULE}UDF_NO_PROBE()\n"
                        f"ONLY_TAGS({tag})\nPEERDIR(support)\n"
                        "LD_PLUGIN(local.py)\nSRCS(body.cpp)\nEND()\n"
                    )
                    files["filters/body.cpp"] = "int body;\n"
                    files["filters/local.py"] = ""
                    files["support/ya.make"] = library("LD_PLUGIN(borrowed.py)\nSRCS(support.cpp)")
                    files["support/borrowed.py"] = ""
                    files["support/support.cpp"] = "int support;\n"
                    files["checks/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(filters)\nEND()\n"
                    targets = ("checks", "-t") if dependency else ("filters",)
                    graph = lib.make(files, *targets, "--strip-packages-from-results")
                    plugin = lib.node_by_output(graph, "$(B)/filters/local.py.pyplugin")
                    self.assertIn(plugin["uid"], graph["result"])
                    self.assertIn("$(S)/filters/local.py", plugin["inputs"])
                    self.assertEqual(plugin["outputs"], ["$(B)/filters/local.py.pyplugin"])
                    for node in graph["graph"]:
                        if "$(B)/support/borrowed.py.pyplugin" in node["outputs"]:
                            self.assertNotIn(node["uid"], graph["result"])

    def test_yql_shared_context_uses_global_plugin_without_a_separate_result(self):
        files = udf_fixture()
        files["filters/ya.make"] = (
            f"YQL_UDF(filter)\n{lib.BARE_MODULE}UDF_NO_PROBE()\n"
            "ONLY_TAGS(YQL_UDF_SHARED)\nLD_PLUGIN(local.py)\nSRCS(body.cpp)\nEND()\n"
        )
        files["filters/body.cpp"] = "int body;\n"
        files["filters/local.py"] = ""
        graph = lib.make(files, "filters", "--strip-packages-from-results")
        plugin = lib.node_by_output(graph, "$(B)/filters/local.py.pyplugin")
        shared = lib.node_by_output(graph, "$(B)/filters/libfilter.so")
        self.assertIn(plugin["uid"], shared["deps"])
        self.assertNotIn(plugin["uid"], graph["result"])
        self.assertIn(shared["uid"], graph["result"])

    def test_library_result_retains_generated_global_and_its_host_tool_closure(self):
        files = world()
        files["localizer/ya.make"] = library("SDC_ROSNODE_INFO(fleet/navigation/localizer)\nSRCS(GLOBAL entry.cpp)")
        files["localizer/entry.cpp"] = "int entry;\n"
        graph = lib.make(files, "localizer", "--strip-packages-from-results")
        meta = lib.node_by_output(graph, "$(B)/localizer/rosnode.sdcmeta")
        writer = lib.node_by_output(graph, f"$(B)/{WRITER}/metadata-writer")
        self.assertIn(meta["uid"], graph["result"])
        self.assertEqual(meta["cmds"][0]["cmd_args"], [f"$(B)/{WRITER}/metadata-writer", "rosnode", "--node-path=fleet/navigation/localizer"])
        self.assertEqual(meta["cmds"][0]["stdout"], "$(B)/localizer/rosnode.sdcmeta")
        self.assertEqual(meta["inputs"], [f"$(B)/{WRITER}/metadata-writer"])
        self.assertEqual(meta["deps"], [writer["uid"]])
        producers = closure(graph, meta)
        self.assertIn("PB", {node["kv"]["p"] for node in producers})
        for node in producers:
            if node is not meta:
                self.assertEqual(node["platform"], "default-linux-x86_64")
        self.assertEqual(meta["platform"], "default-linux-aarch64")
        archive = lib.node_by_output(graph, "$(B)/localizer/liblocalizer.global.a")
        self.assertIn(archive["uid"], graph["result"])
        self.assertNotIn(meta["uid"], archive["deps"])
        self.assertNotIn(meta["outputs"][0], archive["inputs"])

    def test_other_global_metadata_forms_are_library_results(self):
        for statement, output in (
            ("SDC_SOLIB_INFO(https://example.invalid/plugin)", "solib.sdcmeta"),
            ("SDC_RESOURCE_INFO(navigation model https://example.invalid/model)", "model.global.sdcmeta"),
            ("SDC_SB_RESOURCE_INFO(71 weights UNPACK)", "weights.global.sdcmeta"),
        ):
            with self.subTest(statement=statement):
                files = world()
                files["metadata/ya.make"] = library(statement)
                graph = lib.make(files, "metadata")
                meta = lib.node_by_output(graph, f"$(B)/metadata/{output}")
                self.assertIn(meta["uid"], graph["result"])

    def test_requested_library_does_not_retain_borrowed_peer_globals(self):
        files = world()
        files["root/ya.make"] = library("PEERDIR(peer)\nSDC_ROSNODE_INFO(fleet/root)")
        files["peer/ya.make"] = library("SDC_ROSNODE_INFO(fleet/borrowed)")
        graph = lib.make(files, "root", "--add-modules-to-results")
        own = lib.node_by_output(graph, "$(B)/root/rosnode.sdcmeta")
        self.assertIn(own["uid"], graph["result"])
        self.assertFalse(any("$(B)/peer/rosnode.sdcmeta" in node["outputs"] for node in graph["graph"]))

    def test_complete_targets_do_not_retain_unused_global_metadata(self):
        for declaration in ("PROGRAM(app)", "DLL(plugin)"):
            with self.subTest(declaration=declaration):
                files = world()
                files["app/ya.make"] = f"{declaration}\n{lib.BARE_MODULE}SDC_ROSNODE_INFO(fleet/application)\nSRCS(main.cpp)\nEND()\n"
                files["app/main.cpp"] = "int main() {}\n"
                graph = lib.make(files, "app")
                self.assertFalse(any("$(B)/app/rosnode.sdcmeta" in node["outputs"] for node in graph["graph"]))

    def test_bundle_keeps_generated_global_as_a_dependency(self):
        files = world()
        files["bundle/ya.make"] = "SDC_UNION(collection)\nSDC_ROSNODE_INFO(fleet/bundle)\nEND()\n"
        graph = lib.make(files, "bundle")
        meta = lib.node_by_output(graph, "$(B)/bundle/rosnode.sdcmeta")
        bundle = lib.only_node_by_kind(graph, "UN")
        self.assertIn(meta["uid"], bundle["deps"])
        self.assertNotIn(meta["uid"], graph["result"])

    def test_host_and_target_module_contexts_keep_distinct_global_producers(self):
        files = world()
        files["shared/ya.make"] = library(
            "IF(ARCH_X86_64)\nSDC_ROSNODE_INFO(fleet/host)\nELSE()\nSDC_ROSNODE_INFO(fleet/target)\nENDIF()"
        )
        lib.tool_program(files, "tools/consumer", "consumer")
        files["tools/consumer/ya.make"] = files["tools/consumer/ya.make"].replace("END()", "PEERDIR(shared)\nUSE_SDC_META()\nEND()")
        files["generated/ya.make"] = library("RUN_PROGRAM(tools/consumer OUT generated.cpp)")
        graph = lib.make(files, "shared", "generated")
        metas = [node for node in graph["graph"] if "$(B)/shared/rosnode.sdcmeta" in node["outputs"]]
        self.assertEqual(len(metas), 2)
        for meta in metas:
            host = meta["platform"] == "default-linux-x86_64"
            self.assertIn("--node-path=fleet/host" if host else "--node-path=fleet/target", meta["cmds"][0]["cmd_args"])
            self.assertEqual(meta["uid"] in graph["result"], not host)
        consumer = lib.node_by_output(graph, "$(B)/tools/consumer/consumer")
        host_meta = next(node for node in metas if node["platform"] == "default-linux-x86_64")
        self.assertIn(host_meta["uid"], consumer["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
