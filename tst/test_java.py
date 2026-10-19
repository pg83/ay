import hashlib
import unittest

import lib


def resource_lib(name, sbr):
    return f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE({name} sbr:{sbr})\nEND()\n"


def java_tree(**extra):
    files = {
        "build/platform/python/ymake_python3/ya.make": resource_lib("YMAKE_PYTHON3", 1),
        "build/platform/java/jdk/ya.make": resource_lib("JDK_DEFAULT", 2),
        "build/platform/java/jdk/jdk17/ya.make": resource_lib("JDK17", 3),
        "build/platform/java/jdk/jdk26/ya.make": resource_lib("JDK26", 5),
        "build/platform/java/protoc/ya.make": resource_lib("JAVA_PROTOC", 4),
    }
    files.update(extra)
    return files


def contrib(path, *peers, jar=None, src=None):
    body = "JAVA_CONTRIB()\n"
    if peers:
        body += "PEERDIR(\n" + "".join(f"    {p}\n" for p in peers) + ")\n"
    if jar:
        body += f"JAR_RESOURCE({jar})\n"
    if src:
        body += f"SRC_RESOURCE({src})\n"
    return {f"{path}/ya.make": body + "END()\n"}


def proxy(path, default):
    return {f"{path}/ya.make": f"JAVA_CONTRIB_PROXY()\nPEERDIR({default})\nEND()\n"}


def jv_node(graph, jar):
    return lib.node_by_output(graph, jar)


def writer_args(node, file_name):
    for cmd in node["cmds"]:
        args = cmd["cmd_args"]
        if "--file" in args and args[args.index("--file") + 1].endswith(file_name):
            start = args.index("--ya-start-command-file") + 1
            return args[start:args.index("--ya-end-command-file")]
    raise AssertionError(f"no writer of {file_name}")


class JavaContribTest(unittest.TestCase):
    def test_sandbox_jar_and_sources(self):
        files = java_tree(**contrib("contrib/java/org/x/lib/1.0", jar=101, src=102))
        graph = lib.make(files, "contrib/java/org/x/lib/1.0")
        node = lib.node_by_output(graph, "$(B)/contrib/java/org/x/lib/1.0/lib-1.0.jar")
        self.assertEqual(node["kv"], {"p": "SB", "pc": "yellow", "show_out": "yes"})
        self.assertEqual(node["requirements"]["network"], "full")
        self.assertEqual(node["outputs"], [
            "$(B)/contrib/java/org/x/lib/1.0/lib-1.0.jar",
            "$(B)/contrib/java/org/x/lib/1.0/lib-1.0-sources.jar",
        ])
        self.assertEqual([c["cmd_args"][2:] for c in node["cmds"]], [
            ["--resource-file", "$(RESOURCE_ROOT)/sbr/101/resource", "--resource-id", "101",
             "--copy-to", "$(B)/contrib/java/org/x/lib/1.0/lib-1.0.jar"],
            ["--resource-file", "$(RESOURCE_ROOT)/sbr/102/resource", "--resource-id", "102",
             "--copy-to", "$(B)/contrib/java/org/x/lib/1.0/lib-1.0-sources.jar"],
        ])
        self.assertEqual(node["cmds"][0]["cwd"], "$(B)/contrib/java/org/x/lib/1.0")

    def test_local_jar_and_empty_sources(self):
        files = java_tree(**{
            "contrib/java/org/x/local/2.0/ya.make": "JAVA_CONTRIB(local-2.0)\nLOCAL_JAR(local.jar)\nEND()\n",
            "contrib/java/org/x/local/2.0/local.jar": "",
        })
        graph = lib.make(files, "contrib/java/org/x/local/2.0")
        node = lib.node_by_output(graph, "$(B)/contrib/java/org/x/local/2.0/local-2.0.jar")
        self.assertEqual(node["kv"], {"p": "FS", "pc": "yellow"})
        self.assertEqual([c["cmd_args"][2:] for c in node["cmds"][:2]], [
            ["copy", "$(S)/contrib/java/org/x/local/2.0/local.jar", "$(B)/contrib/java/org/x/local/2.0/local-2.0.jar"],
            ["md", "$(B)/contrib/java/org/x/local/2.0/fake-src"],
        ])
        self.assertEqual(node["cmds"][2]["cmd_args"], [
            "$(B)/resources/JDK17/bin/jar", "cfvM", "$(B)/contrib/java/org/x/local/2.0/local-2.0-sources.jar", ".",
        ])
        self.assertEqual(node["cmds"][2]["cwd"], "$(B)/contrib/java/org/x/local/2.0/fake-src")


class JavaDependencyManagementTest(unittest.TestCase):
    def library(self, body, **extra):
        files = java_tree(**{
            "lib/ya.make": "JAVA_LIBRARY_NON_SPLIT()\n" + body + "END()\n",
            **contrib("contrib/java/x/a/1.0", "contrib/java/x/b/1.0", jar=1),
            **contrib("contrib/java/x/a/2.0", "contrib/java/x/b/1.0", jar=2),
            **contrib("contrib/java/x/b/1.0", jar=3),
            **contrib("contrib/java/x/b/2.0", jar=4),
            **contrib("contrib/java/x/c/1.0", "contrib/java/x/b/2.0", jar=5),
            **proxy("contrib/java/x/a", "contrib/java/x/a/1.0"),
        })
        files.update(extra)
        graph = lib.make(files, "lib")
        return writer_args(jv_node(graph, "$(B)/lib/lib.jar"), "bfg.txt")

    def test_proxy_resolves_by_dependency_management(self):
        classpath = self.library("PEERDIR(contrib/java/x/a)\nDEPENDENCY_MANAGEMENT(contrib/java/x/a/2.0)\n")
        self.assertEqual(classpath, ["contrib/java/x/a/2.0/a-2.0.jar", "contrib/java/x/b/1.0/b-1.0.jar"])

    def test_exclude_drops_transitive_peers(self):
        classpath = self.library(
            "PEERDIR(contrib/java/x/a)\nDEPENDENCY_MANAGEMENT(contrib/java/x/a/2.0)\nEXCLUDE(contrib/java/x/b)\n")
        self.assertEqual(classpath, ["contrib/java/x/a/2.0/a-2.0.jar"])

    def test_first_version_in_breadth_order_wins(self):
        classpath = self.library("PEERDIR(contrib/java/x/a/1.0 contrib/java/x/c/1.0)\n")
        self.assertEqual(classpath, [
            "contrib/java/x/a/1.0/a-1.0.jar", "contrib/java/x/b/1.0/b-1.0.jar", "contrib/java/x/c/1.0/c-1.0.jar",
        ])

    def test_managed_version_overrides_transitive_one(self):
        classpath = self.library(
            "PEERDIR(contrib/java/x/a/1.0 contrib/java/x/c/1.0)\nDEPENDENCY_MANAGEMENT(contrib/java/x/b/2.0)\n")
        self.assertEqual(classpath, [
            "contrib/java/x/a/1.0/a-1.0.jar", "contrib/java/x/b/2.0/b-2.0.jar", "contrib/java/x/c/1.0/c-1.0.jar",
        ])


class JavaLibraryTest(unittest.TestCase):
    def test_split_library_peers_interface_jar(self):
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY()\nPEERDIR(dep)\nJAVA_SRCS(SRCDIR src **/*)\nEND()\n",
            "app/src/b/B.java": "",
            "app/src/A.java": "",
            "app/src/a/res.txt": "",
            "dep/ya.make": "JAVA_LIBRARY()\nEND()\n",
        })
        lib.tool_program(files, "contrib/tools/ijar", "ijar")
        graph = lib.make(files, "app")
        full = jv_node(graph, "$(B)/app/app.jar")
        self.assertEqual(writer_args(full, "bfg.txt"), ["dep/dep-interface.jar"])
        salt = hashlib.md5(b"src **/* no").hexdigest()
        prepare = full["cmds"][0]["cmd_args"]
        start = prepare.index("--ya-start-command-file") + 1
        self.assertEqual(prepare[start:prepare.index("--ya-end-command-file")], [
            "--jsources", f"$(B)/app/misc/uniq_{salt}.src.txt",
            "--resources", f"$(B)/app/misc/uniq_{salt}.res.txt",
            "--srcdir", "src",
            "$(S)/app/src/A.java", "$(S)/app/src/a/res.txt", "$(S)/app/src/b/B.java",
        ])
        ijar = lib.node_by_output(graph, "$(B)/dep/dep-interface.jar")
        self.assertEqual(ijar["kv"], {"exclude_from_idea": "yes", "p": "IJ", "pc": "light-blue"})
        self.assertEqual(ijar["cmds"][0]["cmd_args"][1:], [
            "--target_label", "//dep:dep", "--injecting_rule_kind", "arcadia_ijar",
            "$(B)/dep/dep.jar", "$(B)/dep/dep-interface.jar",
        ])


class JavaProtoTest(unittest.TestCase):
    def test_proto_library_java_variant(self):
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(proto/a)\nEND()\n",
            "proto/a/ya.make": "PROTO_LIBRARY()\nPROTO_NAMESPACE(proto)\nSRCS(sub/a.proto)\nPEERDIR(proto/b)\nEND()\n",
            "proto/a/sub/a.proto": 'syntax = "proto3";\n',
            "proto/b/ya.make": "PROTO_LIBRARY()\nPROTO_NAMESPACE(GLOBAL proto/b)\nSRCS(b.proto)\nEND()\n",
            "proto/b/b.proto": 'syntax = "proto3";\n',
            **contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=7),
        })
        graph = lib.make(files, "app")
        pb = lib.node_by_output(graph, "$(B)/proto/a/sub/a.jsrc")
        self.assertEqual(pb["kv"], {"p": "PB", "pc": "yellow"})
        self.assertEqual(pb["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(pb["cmds"][0]["cmd_args"][2:], [
            "--tar-output", "$(B)/proto/a/sub/a.jsrc", "--protoc-out-dir", "$(B)/java_out",
            "$(B)/resources/JAVA_PROTOC/bin/protoc",
            "-I=./proto", "-I=$(B)", "-I=$(S)",
            "-I=$(S)/contrib/libs/protobuf/src", "-I=$(S)/proto", "-I=$(S)/proto/b",
            "-I=$(S)", "--java_out=$(B)/java_out", "proto/a/sub/a.proto",
        ])
        jv = lib.node_by_output(graph, "$(B)/proto/a/proto-a.jar")
        args = jv["cmds"][0]["cmd_args"]
        delims = [i for i, a in enumerate(args) if a == "DELIM"]
        self.assertEqual(args[delims[0] - 1], "$(B)/proto/a/sub/a.jsrc")
        self.assertEqual(args[delims[0] + 1:delims[1]], ["-J-XX:ActiveProcessorCount=4", "-source", "11", "-target", "11"])
        self.assertEqual(args[delims[1] + 1:delims[2]], [
            "$(B)/contrib/java/com/google/protobuf/protobuf-java/4.33.0/protobuf-java-4.33.0.jar",
            "$(B)/proto/b/proto-b.jar",
        ])


class JavaProgramTest(unittest.TestCase):
    def tree(self):
        return java_tree(**{
            "app/ya.make": "JAVA_PROGRAM()\nPEERDIR(contrib/java/x/a/1.0)\nJAVA_SRCS(SRCDIR src **/*)\nEND()\n",
            "app/src/A.java": "",
            "build/platform/java/scriptgen/ya.make": resource_lib("SCRIPTGEN", 9),
            "gen/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nRUN_JAVA_PROGRAM(Main IN in.txt OUT_DIR out CLASSPATH app)\nEND()\n",
            "gen/in.txt": "",
            **contrib("contrib/java/x/a/1.0", jar=1),
        })

    def test_runnable_collects_produced_classpath(self):
        graph = lib.make(self.tree(), "app")
        jp = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
        self.assertEqual(jp["kv"], {"p": "JP", "pc": "light-blue", "show_out": "yes"})
        self.assertEqual(jp["outputs"], [
            "$(B)/app/app.run.cp.jar", "$(B)/app/run.sh", "$(B)/app/app.run.cp.jar.cplst", "$(B)/app/app.tar",
            "$(B)/app/app/app.jar", "$(B)/app/app/a-1.0.jar",
        ])
        self.assertEqual(writer_args(jp, "run-bf.txt"), [
            "app/app.jar", "app/a-1.0.jar", "app/platform-python-ymake_python3.pkg.fake",
            "app/platform-java-scriptgen.pkg.fake", "app/java-jdk-jdk17.pkg.fake",
            "app/platform-java-jdk.pkg.fake", "app/java-jdk-jdk26.pkg.fake",
        ])
        produced = [i for i in jp["inputs"] if i.startswith("$(B)/")]
        self.assertEqual(produced, ["$(B)/app/app.jar", "$(B)/contrib/java/x/a/1.0/a-1.0.jar"])
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertEqual({jar["uid"], lib.node_by_output(graph, produced[1])["uid"]} - set(jp["deps"]), set())

    def test_run_java_program_packs_out_dir_for_the_module(self):
        graph = lib.make(self.tree(), "gen")
        gentar = "$(B)/gen/out.119b890a970620da84eb1a8574f10d77.gentar"
        rj = lib.node_by_output(graph, gentar)
        self.assertEqual(rj["kv"], {"p": "RJ", "pc": "blue", "tared_kind": "nodir"})
        self.assertEqual(rj["cmds"][1]["cmd_args"][-3:], ["-classpath", "@$(B)/app/app.run.cp.jar.cplst", "Main"])
        self.assertEqual(rj["cmds"][2]["cmd_args"][2:], ["--pack", "out", "--outs", gentar])
        self.assertIn("$(B)/app/app.run.cp.jar", rj["inputs"])
        self.assertIn("$(S)/gen/in.txt", rj["inputs"])
        self.assertIn(lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")["uid"], rj["deps"])
        jv = lib.node_by_output(graph, "$(B)/gen/gen.jar")
        prepare = jv["cmds"][0]["cmd_args"]
        self.assertEqual(prepare[prepare.index("--ya-start-command-file") + 1], gentar)
        self.assertIn(gentar, jv["inputs"])
        self.assertIn(rj["uid"], jv["deps"])


class JavaSandboxedInputsTest(unittest.TestCase):
    def test_compile_and_interface_jars_take_their_build_inputs(self):
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY()\nPEERDIR(dep contrib/java/x/a/1.0)\nEND()\n",
            "dep/ya.make": "JAVA_LIBRARY()\nEND()\n",
            **contrib("contrib/java/x/a/1.0", jar=1),
        })
        lib.tool_program(files, "contrib/tools/ijar", "ijar")
        graph = lib.make(files, "app")
        jv = jv_node(graph, "$(B)/app/app.jar")
        self.assertEqual([i for i in jv["inputs"] if i.startswith("$(B)/")], [
            "$(B)/dep/dep-interface.jar", "$(B)/contrib/java/x/a/1.0/a-1.0.jar",
        ])
        ijar = lib.node_by_output(graph, "$(B)/dep/dep-interface.jar")
        self.assertEqual([i for i in ijar["inputs"] if i.startswith("$(B)/")], [
            "$(B)/contrib/tools/ijar/ijar", "$(B)/dep/dep.jar",
        ])

    def test_java_proto_takes_imports_and_plugins(self):
        files = java_tree(**{
            "proto/a/ya.make": "PROTO_LIBRARY()\nGRPC()\nSRCS(a.proto)\nPEERDIR(proto/b)\nEND()\n",
            "proto/a/a.proto": 'syntax = "proto3";\nimport "proto/b/b.proto";\n',
            "proto/b/ya.make": "PROTO_LIBRARY()\nSRCS(b.proto)\nEND()\n",
            "proto/b/b.proto": 'syntax = "proto3";\n',
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(proto/a)\nEND()\n",
            "build/platform/java/grpc/ya.make": (
                "PROGRAM(grpc)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n"),
            "build/platform/java/grpc/main.cpp": "int main(){return 0;}\n",
            **contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=7),
            **contrib("contrib/java/io/grpc/grpc-stub/1.51.0", jar=8),
            **contrib("contrib/java/io/grpc/grpc-protobuf/1.51.0", jar=9),
            **contrib("contrib/java/javax/annotation/javax.annotation-api/1.3.1", jar=10),
        })
        graph = lib.make(files, "app")
        pb = lib.node_by_output(graph, "$(B)/proto/a/a.jsrc")
        self.assertIn("$(S)/proto/b/b.proto", pb["inputs"])
        self.assertIn("$(B)/build/platform/java/grpc/grpc", pb["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
