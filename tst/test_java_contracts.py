import unittest

import lib
from test_java import contrib, writer_args
from test_java_features import command, tree


class JavaContractsTest(unittest.TestCase):
    def test_explicit_jar_name_and_prefix(self):
        for args, output in (("custom PREFIX pre-", "pre-custom.jar"), ("custom PREFIX", "custom.jar")):
            with self.subTest(args=args):
                files = tree("JAVA_SRCS(src/Main.java)")
                files["app/ya.make"] = f"JAVA_LIBRARY_NON_SPLIT({args})\nJAVA_SRCS(src/Main.java)\nEND()\n"
                jar = lib.node_by_output(lib.make(files, "app"), "$(B)/app/" + output)
                self.assertIn("$(S)/app/src/Main.java", jar["inputs"])

    def test_split_library_exports_its_interface_and_transitive_peers(self):
        files = tree("PEERDIR(dependency)\nJAVA_SRCS(src/Main.java)", **{
            **contrib("dependency", jar=62),
            "consumer/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(app)\nEND()\n",
        })
        graph = lib.make(files, "consumer")
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertEqual(writer_args(jar, "bfg.txt"), ["dependency/dependency.jar"])
        interface = lib.node_by_output(graph, "$(B)/app/app-interface.jar")
        self.assertIn(jar["uid"], interface["deps"])
        consumer = lib.node_by_output(graph, "$(B)/consumer/consumer.jar")
        self.assertEqual(writer_args(consumer, "bfg.txt"), ["app/app-interface.jar", "dependency/dependency.jar"])

    def test_excluded_java_proto_has_no_java_output(self):
        files = tree("PEERDIR(proto/api)", **{
            "proto/api/ya.make": "PROTO_LIBRARY()\nEXCLUDE_TAGS(JAVA_PROTO)\nSRCS(api.proto)\nEND()\n",
            "proto/api/api.proto": 'syntax = "proto3";\n',
        })
        graph = lib.make(files, "app")
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertEqual(writer_args(jar, "bfg.txt"), [])
        self.assertFalse(any(p.startswith("$(B)/proto/api/") for n in graph["graph"] for p in n["outputs"]))

    def test_incompatible_java_peer_tags_are_reported(self):
        for module in ("JAVA_TEST_LIBRARY", "JUNIT5"):
            with self.subTest(module=module):
                files = tree("PEERDIR(dependency)", module="EXTERNAL_JAVA_LIBRARY", **{
                    "dependency/ya.make": f"{module}()\nEND()\n",
                    "consumer/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(app)\nEND()\n",
                })
                result = lib.make_process(files, "consumer")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("PEERDIR to dependency is prohibited by incompatible tags", result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_default_source_layouts_include_java_kotlin_and_resources(self):
        for macro, src, resources in (
            ("DEFAULT_JAVA_SRCS_LAYOUT", "src/main/java", "src/main/resources"),
            ("DEFAULT_JUNIT_JAVA_SRCS_LAYOUT", "java", "resources"),
        ):
            for kotlin in (False, True):
                with self.subTest(macro=macro, kotlin=kotlin):
                    files = tree(("WITH_KOTLIN()\n" if kotlin else "") + f"{macro}()", **{
                        f"app/{src}/pkg/Main.java": "class Main {}\n",
                        f"app/{src}/pkg/Util.kt": "object Util\n",
                        f"app/{resources}/nested/settings.txt": "value\n",
                    })
                    jar = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.jar")
                    self.assertIn(f"$(S)/app/{src}/pkg/Main.java", jar["inputs"])
                    self.assertIn(f"$(S)/app/{resources}/nested/settings.txt", jar["inputs"])
                    self.assertEqual(f"$(S)/app/{src}/pkg/Util.kt" in jar["inputs"], kotlin)
                    self.assertIn("--resources", command(jar, "prepare_jar_build.py"))

    def test_default_and_appended_variables_drive_sources_and_javac(self):
        files = tree("""
DEFAULT(SOURCE_DIR src)
DEFAULT(SOURCE_DIR ignored)
SET_APPEND(SOURCE_GLOBS Main.java)
SET_APPEND(SOURCE_GLOBS Skip.java)
SET(OPTIONS -Werror)
SET_APPEND(OPTIONS -Xlint:all)
JAVAC_FLAGS(${OPTIONS})
JAVA_SRCS(SRCDIR ${SOURCE_DIR} ${SOURCE_GLOBS})
""")
        jar = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.jar")
        self.assertIn("$(S)/app/src/Main.java", jar["inputs"])
        self.assertIn("$(S)/app/src/Skip.java", jar["inputs"])
        javac = command(jar, "run_javac.py")
        self.assertIn("-Werror", javac)
        self.assertIn("-Xlint:all", javac)

    def test_api_implementation_and_runtime_peers_have_distinct_classpaths(self):
        files = tree("PEERDIR(facade)\nJAVA_RUNTIME_PEERDIR(runtime)\nJAVA_TEST_PEERDIR(testonly)",
                     module="JAVA_PROGRAM", **{
            "facade/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nJAVA_API_PEERDIR(api)\nJAVA_IMPL_PEERDIR(impl)\nEND()\n",
            **contrib("api", jar=21), **contrib("impl", jar=22),
            **contrib("runtime", jar=23), **contrib("testonly", jar=24),
        })
        graph = lib.make(files, "app")
        facade = lib.node_by_output(graph, "$(B)/facade/facade.jar")
        self.assertEqual(writer_args(facade, "bfg.txt"), ["api/api.jar", "impl/impl.jar"])
        app = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertEqual(writer_args(app, "bfg.txt"), ["facade/facade.jar", "api/api.jar"])
        run = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
        self.assertEqual({p for p in writer_args(run, "run-bf.txt") if p.endswith(".jar")}, {
            "app/app.jar", "app/facade.jar", "app/api.jar", "app/impl.jar", "app/runtime.jar",
        })
        for path in ("facade/facade.jar", "api/api.jar", "impl/impl.jar", "runtime/runtime.jar"):
            self.assertIn("$(B)/" + path, run["inputs"])
        self.assertFalse(any("$(B)/testonly/testonly.jar" in n["outputs"] for n in graph["graph"]))

    def test_program_custom_script_without_default_runner(self):
        files = tree("""
ENABLE(DISABLE_SCRIPTGEN)
GENERATE_SCRIPT(OUT custom.sh)
WITH_JDK()
""", module="JAVA_PROGRAM", **{
            "app/template.tmpl": "script template\n",
            "build/platform/java/jdk/with_jdk/ya.make": lib.resource_lib("WITH_JDK17", 31),
        })
        run = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.run.cp.jar")
        self.assertIn("$(B)/app/custom.sh", run["outputs"])
        self.assertIn("$(B)/app/jdk.tar", run["outputs"])
        self.assertNotIn("$(B)/app/run.sh", run["outputs"])
        self.assertIn("$(S)/app/template.tmpl", run["inputs"])
        self.assertEqual(sum("/scriptgen" in " ".join(c["cmd_args"]) for c in run["cmds"]), 1)

    def test_auto_service_annotations_are_compile_dependencies(self):
        files = tree("AUTO_SERVICE(1.1.1)\nJAVA_SRCS(src/Main.java)", **{
            **contrib("contrib/java/com/google/auto/service/auto-service-annotations/1.1.1", jar=41),
        })
        graph = lib.make(files, "app")
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        annotations = lib.node_by_output(graph,
            "$(B)/contrib/java/com/google/auto/service/auto-service-annotations/1.1.1/auto-service-annotations-1.1.1.jar")
        self.assertIn(annotations["uid"], jar["deps"])
        self.assertIn(annotations["outputs"][0][5:], writer_args(jar, "bfg.txt"))

    def test_kotlin_proto_grpc_custom_plugin_and_common_google_apis(self):
        files = tree("PEERDIR(proto/api)", **{
            "proto/api/ya.make": "PROTO_LIBRARY()\nWITH_KOTLIN_GRPC()\nUSE_COMMON_GOOGLE_APIS()\n"
                "JAVA_PROTO_PLUGIN(custom tools/plugin DEPS custom/runtime)\n"
                "SET_APPEND(_PROTOC_FLAGS --experimental_allow_proto3_optional)\n"
                "ADDINCL(GLOBAL FOR proto imports)\n"
                "PROTOC_FATAL_WARNINGS()\nEXPORT_YMAPS_PROTO()\nSRCS(GLOBAL api.proto)\nEND()\n",
            "proto/api/api.proto": 'syntax = "proto3";\n',
            "build/platform/java/kotlin_grpc/ya.make": lib.resource_lib("KOTLIN_GRPC", 51),
            **contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=52),
            **contrib("contrib/java/com/google/protobuf/protobuf-kotlin/4.33.0", jar=53),
            **contrib("contrib/java/io/grpc/grpc-kotlin-stub/1.3.1", jar=54),
            **contrib("contrib/java/com/google/api/grpc/proto-google-common-protos/2.63.2", jar=55),
            **contrib("custom/runtime", jar=56),
        })
        lib.tool_program(files, "tools/plugin", "plugin")
        graph = lib.make(files, "app")
        proto = lib.node_by_output(graph, "$(B)/proto/api/api.jsrc")
        args = proto["cmds"][0]["cmd_args"]
        for flag in ("--kotlin_out=$(B)/java_out", "--kotlin_grpc_out=$(B)/java_out",
                     "--custom_out=$(B)/java_out", "--fatal_warnings",
                     "--experimental_allow_proto3_optional", "-I=./maps/doc/proto"):
            self.assertIn(flag, args)
        self.assertIn("-I=$(S)/imports", args)
        self.assertIn("$(B)/tools/plugin/plugin", proto["inputs"])
        self.assertEqual(proto["cmds"][0]["env"]["JAVA_HOME"], "$(B)/resources/JDK17")
        self.assertTrue(proto["cmds"][0]["env"]["KOTLIN_GRPC_JAR"].endswith("protoc-gen-grpc-kotlin-1.3.1.jar"))
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertIn("custom/runtime/custom-runtime.jar", writer_args(jar, "bfg.txt"))
        self.assertIn("contrib/java/com/google/api/grpc/proto-google-common-protos/2.63.2/proto-google-common-protos-2.63.2.jar",
                      writer_args(jar, "bfg.txt"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
