import json

from tst.make.lib import MakeTest, PROJECT


class ProtoPluginsTest(MakeTest):
    def plugin(self):
        self.write("plugin/ya.make", "PY3_PROGRAM(plugin)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("plugin/main.py", "import sys\nsys.stdin.buffer.read()\n")
        self.write("peer/ya.make", "LIBRARY()\nSRCS(value.cpp)\nEND()\n")
        self.write("peer/value.cpp", "int value() { return 42; }\n")

    def test_cpp_plugins(self):
        self.plugin()
        for macro, suffixes in (("CPP_PROTO_PLUGIN0", ""), ("CPP_PROTO_PLUGIN", ".extra.h"), ("CPP_PROTO_PLUGIN2", ".extra.h .extra.cpp"), ("CPP_EV_PROTO_PLUGIN", ".extra.h")):
            with self.subTest(macro=macro):
                extension = ".ev" if macro == "CPP_EV_PROTO_PLUGIN" else ".proto"
                self.write("ya.make", f"PROTO_LIBRARY()\n{macro}(extra {PROJECT}/plugin {suffixes} DEPS {PROJECT}/peer EXTRA_OUT_FLAG e2e=true)\nSRCS(answer{extension})\nEND()\n")
                self.write("answer" + extension, 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
                graph = self.graph()
                self.assertTrue(any("protoc-gen-extra" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))
                if suffixes and macro != "CPP_EV_PROTO_PLUGIN":
                    self.assertTrue(any(path.endswith(".extra.h") for node in graph["graph"] for path in node["outputs"]))

    def test_python_plugins(self):
        self.plugin()
        self.write("ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("proto/answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
        for macro, suffixes in (("PY_PROTO_PLUGIN", "_extra.py"), ("PY_PROTO_PLUGIN2", "_extra.py _other.py")):
            with self.subTest(macro=macro):
                self.write("proto/ya.make", f"PROTO_LIBRARY()\n{macro}(extra {suffixes} {PROJECT}/plugin)\nSRCS(answer.proto)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(path.endswith("_extra.py") for node in graph["graph"] for path in node["outputs"]))

    def test_java_plugin(self):
        self.plugin()
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("proto/ya.make", f"PROTO_LIBRARY()\nJAVA_PROTO_PLUGIN(extra {PROJECT}/plugin)\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any("protoc-gen-extra" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_yt_proto_yson(self):
        self.write("ya.make", "GO_LIBRARY()\nYT_ORM_PROTO_YSON(answer.proto OUT_OPTS e2e=true)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any("proto-yson-gen" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_apphost_options(self):
        self.write("ya.make", "PROTO_LIBRARY()\nAPPHOST(ITEM_DISPATCHER E2EDispatcher ITEM_DISPATCHER_HEADER dispatcher.h)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
        self.write("dispatcher.h", "#pragma once\n")
        self.node(self.graph(), "answer.apphost.h")
