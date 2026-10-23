import unittest

import lib
import test_go_tests as go
import test_proto_cpp as cpp


PARCEL = "$(B)/wire/parcel/libwire-parcel.a"
COMMON = "$(B)/wire/common/libwire-common.a"
RUNTIME = "$(B)/vendor/fable.net/runtime/runtime.a"
SUPPORT = "$(B)/vendor/fable.net/support/support.a"
TOOL = "$(B)/tools/ornament/ornament"


def fixture():
    files = go.go_proto_world()
    files.update(cpp.proto_files())
    files.update({
        "shared/plugin.inc": (
            "GO_PROTO_PLUGIN(ornament _ornament.pb.go tools/ornament "
            "DEPS wire/common vendor/fable.net/runtime)\n"
        ),
        "wire/parcel/ya.make": (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(CPP_PROTO GO_PROTO)\n"
            "INCLUDE(${ARCADIA_ROOT}/shared/plugin.inc)\n"
            "PEERDIR(wire/common)\nSRCS(parcel.proto)\nEND()\n"
        ),
        "wire/parcel/parcel.proto": 'syntax = "proto3";\nimport "wire/common/common.proto";\nmessage Parcel {}\n',
        "wire/common/ya.make": f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(CPP_PROTO GO_PROTO)\nSRCS(common.proto)\nEND()\n",
        "wire/common/common.proto": 'syntax = "proto3";\nmessage Common {}\n',
        "native/ya.make": (
            f"UNITTEST()\n{lib.BARE_MODULE}SIZE(MEDIUM)\n"
            "PEERDIR(wire/parcel native/config)\nSRCS(check.cpp)\nEND()\n"
        ),
        "native/check.cpp": '#include "wire/parcel/parcel.pb.h"\nint check;\n',
        "native/config/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(FABLE_TOOL sbr:6701)\nEND()\n",
        "library/cpp/testing/unittest_main/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SRCS(main.cpp)\nEND()\n",
        "library/cpp/testing/unittest_main/main.cpp": "int main(){return 0;}\n",
        "native/bridge/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SRCS(bridge.cpp)\nEND()\n",
        "native/bridge/bridge.cpp": "int bridge;\n",
    })
    go.go_module(files, "vendor/fable.net/support")
    go.go_module(files, "vendor/fable.net/runtime", macros="PEERDIR(vendor/fable.net/support)\n")
    go.go_module(files, "tools/ornament", module="GO_PROGRAM")
    go.go_module(files, "goclient", module="GO_PROGRAM", macros="PEERDIR(wire/parcel native/bridge)\n")
    return files


def command_args(node):
    return [a for cmd in node["cmds"] for a in cmd["cmd_args"]]


class GoProtoPluginContextTest(unittest.TestCase):
    def assert_native(self, graph):
        link = lib.node_by_output(graph, "$(B)/native/native")
        parcel = lib.node_by_output(graph, PARCEL)
        common = lib.node_by_output(graph, COMMON)
        producer = lib.node_by_output(graph, "$(B)/wire/parcel/parcel.pb.h")
        compiled = lib.node_by_output(graph, "$(B)/wire/parcel/parcel.pb.cc.o")
        for node in (link, parcel, common, producer, compiled):
            self.assertFalse(any("/vendor/fable.net/" in i for i in node["inputs"]), node["outputs"])
            self.assertFalse(any("/go/_std_" in i for i in node["inputs"]), node["outputs"])
            self.assertNotIn(TOOL, node["inputs"])
        self.assertIn(PARCEL, link["inputs"])
        self.assertIn(COMMON, link["inputs"])
        self.assertIn(parcel["uid"], link["deps"])
        self.assertIn(common["uid"], link["deps"])
        self.assertIn("$(S)/wire/parcel/parcel.proto", producer["inputs"])
        self.assertIn("$(S)/wire/common/common.proto", producer["inputs"])
        self.assertIn("$(B)/wire/parcel/parcel.pb.cc", compiled["inputs"])
        self.assertIn(producer["uid"], compiled["deps"])
        self.assertIn(compiled["uid"], parcel["deps"])
        self.assertNotIn("$(B)/wire/parcel/parcel_ornament.pb.go", producer["outputs"])
        run = lib.node_by_output(graph, "$(B)/native/test-results/unittest/meta.json")
        globals_ = go.args_after(command_args(run), "--global-resource")
        names = [v.split("::")[0] for v in globals_]
        self.assertIn("FABLE_TOOL_RESOURCE_GLOBAL", names)
        self.assertNotIn("GO_TOOLS_RESOURCE_GLOBAL", names)
        self.assertNotIn("YOLINT_RESOURCE_GLOBAL", names)
        self.assertIn(link["uid"], run["deps"])
        self.assertIn("$(B)/native/native", command_args(run))
        self.assertIn("$(S)/native", run["inputs"])

    def assert_go(self, graph):
        client = lib.node_by_output(graph, "$(B)/goclient/goclient")
        parcel = lib.node_by_output(graph, "$(B)/wire/parcel/parcel.a")
        common = lib.node_by_output(graph, "$(B)/wire/common/common.a")
        runtime = lib.node_by_output(graph, RUNTIME)
        support = lib.node_by_output(graph, SUPPORT)
        for peer in (parcel, common, runtime, support):
            self.assertIn(peer["outputs"][0], client["inputs"])
            self.assertIn(peer["uid"], client["deps"])
        self.assertIn(RUNTIME, parcel["inputs"])
        self.assertIn(runtime["uid"], parcel["deps"])
        self.assertIn(SUPPORT, runtime["inputs"])
        self.assertIn(support["uid"], runtime["deps"])
        self.assertNotIn(COMMON, parcel["inputs"])
        producer = lib.node_by_output(graph, "$(B)/wire/parcel/parcel_ornament.pb.go")
        self.assertEqual(producer["outputs"], ["$(B)/wire/parcel/parcel.pb.go", "$(B)/wire/parcel/parcel_ornament.pb.go"])
        self.assertIn("--plugin=protoc-gen-ornament=" + TOOL, command_args(producer))
        self.assertIn("$(S)/wire/parcel/parcel.proto", producer["inputs"])
        self.assertIn("$(S)/wire/common/common.proto", producer["inputs"])
        tool = lib.node_by_output(graph, TOOL)
        self.assertIn(TOOL, producer["inputs"])
        self.assertIn(tool["uid"], producer["foreign_deps"]["tool"])
        self.assertIn(producer["uid"], parcel["deps"])
        self.assertIn("$(B)/wire/parcel/parcel_ornament.pb.go", parcel["inputs"])
        bridge = lib.node_by_output(graph, "$(B)/native/bridge/libnative-bridge.a")
        self.assertIn(bridge["outputs"][0], client["inputs"])
        self.assertIn(bridge["uid"], client["deps"])
        self.assertIn("native/bridge/libnative-bridge.a", command_args(client))

    def test_native_test_ignores_go_plugin_dependencies_and_resource_exports(self):
        graph = lib.make(fixture(), "native", "-t")
        self.assert_native(graph)
        outputs = [o for n in graph["graph"] for o in n["outputs"]]
        self.assertNotIn(RUNTIME, outputs)
        self.assertNotIn(TOOL, outputs)

    def test_shared_include_selects_distinct_cpp_go_peers_in_both_root_orders(self):
        for targets in (("native", "goclient"), ("goclient", "native")):
            with self.subTest(targets=targets):
                graph = lib.make(fixture(), *targets, "-t")
                self.assert_native(graph)
                self.assert_go(graph)

    def test_selected_go_context_keeps_plugin_and_real_native_bridge(self):
        self.assert_go(lib.make(fixture(), "goclient"))

    def test_ordinary_native_library_ignores_shared_call(self):
        files = fixture()
        files["native/ya.make"] = files["native/ya.make"].replace("UNITTEST()", "LIBRARY()")
        graph = lib.make(files, "native")
        archive = lib.node_by_output(graph, "$(B)/native/libnative.a")
        self.assertIn("$(B)/native/check.cpp.o", archive["inputs"])
        self.assertNotIn(RUNTIME, archive["inputs"])
        self.assertFalse(any(o == RUNTIME or o == TOOL for n in graph["graph"] for o in n["outputs"]))

    def test_go_call_without_proto_keeps_deps_and_defers_plugin_tool(self):
        files = fixture()
        go.go_module(files, "plain", macros="INCLUDE(${ARCADIA_ROOT}/shared/plugin.inc)\n")
        graph = lib.make(files, "plain")
        package = lib.node_by_output(graph, "$(B)/plain/plain.a")
        for output in (RUNTIME, SUPPORT, "$(B)/wire/common/common.a"):
            peer = lib.node_by_output(graph, output)
            self.assertIn(output, package["inputs"])
            self.assertIn(peer["uid"], package["deps"])
        self.assertFalse(any(TOOL in n["outputs"] for n in graph["graph"]))

    def test_ignored_call_does_not_bind_formals_or_resolve_missing_peers(self):
        for call in ("GO_PROTO_PLUGIN()", "GO_PROTO_PLUGIN(absent _absent.go missing/tool DEPS missing/runtime)"):
            with self.subTest(call=call):
                files = cpp.proto_files()
                files["wire/ya.make"] = f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(CPP_PROTO)\n{call}\nSRCS(p.proto)\nEND()\n"
                files["wire/p.proto"] = 'syntax = "proto3";\nmessage P {}\n'
                files["caller/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(wire)\nEND()\n"
                graph = lib.make(files, "caller")
                link = lib.node_by_output(graph, "$(B)/caller/caller")
                archive = lib.node_by_output(graph, "$(B)/wire/libwire.a")
                self.assertIn(archive["uid"], link["deps"])
                self.assertIn(archive["outputs"][0], link["inputs"])
                self.assertFalse(any("/missing/" in o for n in graph["graph"] for o in n["outputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
