from tst.make.lib import MakeTest, PROJECT


class ProjectVariantsTest(MakeTest):
    def test_sdc_install_files(self):
        kinds = "COMMON_BIN COMMON_LIB ETC_ROS INSTALL_ROOT LAUNCH LIB PROFILE_HOOKS PYTHON SHARE TS".split()
        self.write("ya.make", "UNION(answer)\nSDC_INSTALL(" + " ".join(kind + " " + kind.lower() + ".txt" for kind in kinds) + ")\nEND()\n")
        for kind in kinds:
            self.write(kind.lower() + ".txt", "42\n")
        graph = self.graph()
        self.assertTrue(any("sdc_install/" in output for node in graph["graph"] for output in node["outputs"]))

    def test_sdc_install_filter(self):
        self.write("ya.make", "UNION(answer)\nSET(SDC_PACKAGE_NAME custom)\nSET(SDC_INSTALL_KINDS SHARE)\nSDC_INSTALL(LAUNCH package.xml COMMON_BIN ignored.txt SHARE value.txt)\nEND()\n")
        self.write("package.xml", "<package><name>custom</name></package>\n")
        self.write("value.txt", "42\n")
        self.write("ignored.txt", "17\n")
        graph = self.graph()
        self.assertTrue(any("sdc_install/share/custom/value.txt" in output for node in graph["graph"] for output in node["outputs"]))

    def test_sdc_install_target(self):
        self.write("ya.make", f"UNION(answer)\nSDC_INSTALL(COMMON_BIN TARGET {PROJECT}/tool AS custom)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nUSE_SDC_META()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        self.assertTrue(any("sdc_install/common_bin/custom" in output for node in graph["graph"] for output in node["outputs"]))

    def test_sdc_supernode(self):
        self.write("ya.make", f"UNION(answer)\nSET(SDC_USE_SUPERNODE yes)\nSDC_INSTALL(LIB NODE node {PROJECT}/unused)\nEND()\n")
        graph = self.graph()
        self.assertTrue(any("sdc_install/lib/answer/node" in output for node in graph["graph"] for output in node["outputs"]))

    def test_sdc_union_share_variant(self):
        self.write("ya.make", f"PACKAGE()\nPEERDIR({PROJECT}/component)\nEND()\n")
        self.write("component/ya.make", f"MLAUNCH_LIBRARY(component)\nGOLAUNCH_SRCS(component.go)\nRESOURCE_PEERDIR({PROJECT}/install)\nEND()\n")
        self.write("component/component.go", "package component\nfunc Answer() int { return 42 }\n")
        self.write("install/ya.make", f"SDC_UNION(install)\nSDC_INSTALL(SHARE value.txt LIB TARGET {PROJECT}/tool)\nEND()\n")
        self.write("install/value.txt", "42\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nUSE_SDC_META()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        union = self.node(graph, "install/install.share.pkg.fake")
        self.assertTrue(any(output.endswith("sdc_install/share/install/value.txt") for output in union["outputs"]))
        outputs = {output for node in graph["graph"] for output in node["outputs"]}
        self.assertNotIn(f"$(B)/{PROJECT}/install/install.pkg.fake", outputs)
        self.assertNotIn(f"$(B)/{PROJECT}/tool/tool", outputs)

    def test_sdc_union_global_meta(self):
        self.write("install/ya.make", "SDC_UNION(install)\nSDC_RESOURCE_INFO(perception answer https://example.com/answer.tar.gz UNPACK)\nEND()\n")
        meta, global_meta = f"$(B)/{PROJECT}/install/answer.sdcmeta", f"$(B)/{PROJECT}/install/answer.global.sdcmeta"
        for flags in ("", "USE_SDC_META()\n"):
            with self.subTest(flags=flags):
                self.write("ya.make", f"PACKAGE()\n{flags}PEERDIR({PROJECT}/install)\nEND()\n")
                graph = self.graph()
                union = self.node(graph, "install/install.pkg.fake")
                self.assertIn(meta, union["outputs"])
                self.assertNotIn(global_meta, union["outputs"])
                self.assertIn(global_meta, union["inputs"])
                package = next(node for node in graph["graph"] if node["outputs"][0].endswith(".final.pkg.fake"))
                self.assertLessEqual({meta, global_meta}, set(package["inputs"]))
                copy = next(command["cmd_args"] for command in package["cmds"] if "--dest-dir" in command["cmd_args"])
                position = {arg.rsplit("/", 1)[-1]: i for i, arg in enumerate(copy)}
                self.assertLess(position["answer.global.sdcmeta"], position["answer.sdcmeta"])

    def test_proto_python_namespace(self):
        self.write("ya.make", f"PY3_LIBRARY()\nPEERDIR({PROJECT}/proto)\nPROTO_TO_NAMESPACE(IMPORT_ALL_IN_INIT IN_MSG answer_pb2)\nEND()\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nPY_NAMESPACE(TOP_LEVEL)\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.node(graph, "msg/answer_pb2.py")
        self.node(graph, "msg/__init__.py")

    def test_yql_python_udf(self):
        self.write("ya.make", "LIBRARY()\nREGISTER_YQL_PYTHON_UDF(NAME CustomPythonE2E RESOURCE_NAME answer)\nEND()\n")
        self.node(self.graph(), "CustomPythonE2E.yql_python_udf.cpp")

    def test_alice_proto_includes(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nPEERDIR({PROJECT}/alice/proto)\nALICE_GENERATE_FUNCTION_PROTO_INCLUDES(${{ARCADIA_ROOT}}/{PROJECT}/alice/proto/answer.proto)\nEND()\n")
        self.write("alice/proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("alice/proto/answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { message Nested {} optional int32 value = 1; }\n')
        self.node(self.graph(), "proto_includes.cpp")

    def test_matrixnet(self):
        self.write("ya.make", "LIBRARY()\nPEERDIR(kernel/matrixnet)\nBUILD_MN(answer.mn answer)\nEND()\n")
        self.write("answer.mn", "\n")
        self.node(self.graph(), "mn.answer.cpp")

    def test_archive_keys(self):
        self.write("ya.make", "LIBRARY()\nARCHIVE_BY_KEYS(NAME answer.archive KEYS answer DONTCOMPRESS value.txt)\nRESOURCE(answer.archive /archive)\nEND()\n")
        self.write("value.txt", "42\n")
        self.node(self.graph(), "answer.archive")

    def test_go_resource(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nRESOURCE_FILES(PREFIX e2e/ value.txt)\nRESOURCE(- e2e.answer=42)\nEND()\n")
        self.write("answer.go", "package answer\n")
        self.write("value.txt", "42\n")
        graph = self.graph()
        self.assertTrue(any(path.endswith(".res.go") for node in graph["graph"] for path in node["outputs"]))

    def test_go_generated_embed(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nCOPY_FILE(value.txt generated/value.txt)\nGO_EMBED_BINDIR(generated FILES generated/value.txt)\nEND()\n")
        self.write("answer.go", 'package answer\nimport "embed"\n//go:embed generated/*\nvar Files embed.FS\n')
        self.write("value.txt", "42\n")
        graph = self.graph()
        self.node(graph, "generated/value.txt")

    def test_maps_sproto(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.proto helper.cpp)\nYMAPS_SPROTO(helper.cpp answer.proto)\nRESOURCE(answer.sproto.h /schema)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        self.write("helper.cpp", "int helper() { return 42; }\n")
        graph = self.graph()
        self.node(graph, "answer.sproto.h")
        self.node(graph, "helper.cpp.o")
        self.assertFalse(any(path.endswith("helper.sproto.h") for node in graph["graph"] for path in node["outputs"]))
