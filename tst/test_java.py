import hashlib
import unittest

import lib
from lib import java_tree, resource_lib


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
    def test_run_java_archive_hash_preserves_lexical_quotes(self):
        files = self.tree()
        files["gen/ya.make"] = (
            'JAVA_LIBRARY_NON_SPLIT()\nSET(VALUE example)\n'
            'RUN_JAVA_PROGRAM(Main --value="${VALUE}" OUT_DIR out CLASSPATH app)\nEND()\n'
        )
        graph = lib.make(files, "gen")
        salt = hashlib.md5(b'Main --value="example" OUT_DIR out CLASSPATH app').hexdigest()
        archive = lib.node_by_output(graph, f"$(B)/gen/out.{salt}.gentar")
        self.assertEqual(archive["cmds"][1]["cmd_args"][-2:], ["Main", "--value=example"])

    def test_run_java_input_directories_are_normalized_before_globbing(self):
        files = self.tree()
        files["gen/ya.make"] = (
            "JAVA_LIBRARY_NON_SPLIT()\nRUN_JAVA_PROGRAM(Main IN_DIR ${CURDIR}/in.txt/.. OUT_DIR out CLASSPATH app)\nEND()\n"
        )
        archive = next(n for n in lib.make(files, "gen")["graph"] if n["kv"]["p"] == "RJ")
        self.assertIn("$(S)/gen/in.txt", archive["inputs"])
        self.assertFalse(any("/../" in p for p in archive["inputs"]))

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

    def test_run_java_program_tool_is_built_for_the_host(self):
        graph = lib.make(self.tree(), "gen")
        cp = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
        self.assertEqual(cp["platform"], "default-linux-x86_64")
        self.assertEqual(jv_node(graph, "$(B)/gen/gen.jar")["platform"], "default-linux-aarch64")

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


class JavaJniTest(unittest.TestCase):
    def test_dll_peer_is_a_pic_shared_object_packed_into_the_jar(self):
        bare = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nADD_DLLS_TO_JAR()\nPEERDIR(jni)\nEND()\n",
            "jni/ya.make": f"DLL(app_jni)\n{bare}EXPORTS_SCRIPT(jni.exports)\nSRCS(j.cpp)\nPEERDIR(dep)\nEND()\n",
            "jni/j.cpp": "int j;\n",
            "jni/jni.exports": "{};\n",
            "dep/ya.make": f"LIBRARY()\n{bare}SRCS(d.cpp)\nEND()\n",
            "dep/d.cpp": "int d;\n",
        })
        graph = lib.make(files, "app")
        so = lib.node_by_output(graph, "$(B)/jni/libapp_jni.so")
        link = next(c["cmd_args"] for c in so["cmds"] if "$(S)/build/scripts/link_dyn_lib.py" in c["cmd_args"])
        self.assertIn("$(B)/jni/j.cpp.pic.o", link)
        self.assertIn("-Wl,--version-script=$(S)/jni/jni.exports", link)
        self.assertIn("dep/libdep.a", link)
        self.assertIn("-fPIC", lib.node_by_output(graph, "$(B)/dep/d.cpp.pic.o")["cmds"][0]["cmd_args"])
        jv = jv_node(graph, "$(B)/app/app.jar")
        self.assertIn("jni/libapp_jni.so", writer_args(jv, "bfg.txt"))
        self.assertIn("$(B)/jni/libapp_jni.so", jv["inputs"])
        self.assertIn(so["uid"], jv["deps"])
        jni = next(c["cmd_args"] for c in jv["cmds"] if "link_or_copy_to_dir" in c["cmd_args"])
        start = jni.index("--ya-start-command-file")
        self.assertEqual(jni[start:], [
            "--ya-start-command-file", "$(B)/jni/libapp_jni.so", "--ya-end-command-file", "$(B)/app/cls",
        ])


def swig_jni_tree(**extra):
    bare = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
    swig_lib = "contrib/tools/swig/Lib"
    files = java_tree(**{
        f"{swig_lib}/java/ya.make": (
            f"LIBRARY()\n{bare}ADDINCL(GLOBAL FOR swig {swig_lib}/java GLOBAL FOR swig {swig_lib})\nEND()\n"
        ),
        "contrib/libs/jdk/ya.make": f"LIBRARY()\n{bare}ADDINCL(GLOBAL contrib/libs/jdk/include)\nEND()\n",
        f"{swig_lib}/java/java.swg": "",
        f"{swig_lib}/swig.swg": "",
        f"{swig_lib}/go.swg": "",
        f"{swig_lib}/perl5.swg": "",
        f"{swig_lib}/python.swg": "",
        "jni/ya.make": (
            f"DLL_JAVA(jni-lib)\n{bare}ADD_DLLS_TO_JAR()\nPEERDIR(dep)\n"
            "SRCS(impl.cpp wrap.swg skipped.c)\nEND()\n"
        ),
        "jni/impl.cpp": "int impl;\n",
        "jni/skipped.c": "int skipped;\n",
        "jni/wrap.swg": '%include "api.i"\n',
        "jni/api.i": '%{\n#include "jni/api.h"\n%}\n%include "jni/api.h"\n',
        "jni/api.h": '#include "dep/gen.h"\n#include <jni.h>\n',
        "contrib/libs/jdk/include/jni.h": "#include <jni_md>\n",
        "contrib/libs/jdk/include/jni_md": "#include <jni_types.h>\n",
        "contrib/libs/jdk/include/jni_types.h": "",
        "dep/ya.make": f"LIBRARY()\n{bare}SRCS(gen.h.in d.cpp)\nEND()\n",
        "dep/gen.h.in": "",
        "dep/d.cpp": "int d;\n",
        **extra,
    })
    lib.tool_program(files, "contrib/tools/swig", "swig")
    return files


class JavaSwigTest(unittest.TestCase):
    def test_header_includes_inherit_cpp_parser_for_ambiguous_extensions(self):
        files = swig_jni_tree(**{
            "jni/api.h": '#include "detail.i"\n',
            "jni/detail.i": '#include "nested.h"\n',
            "jni/nested.h": "",
        })
        graph = lib.make(files, "jni")
        jsrc = lib.node_by_output(graph, "$(B)/jni/wrap.swg.jsrc")
        self.assertIn("$(S)/jni/nested.h", jsrc["inputs"])

    def test_dll_java_wraps_a_swig_interface_for_jni_and_java(self):
        graph = lib.make(swig_jni_tree(), "jni")
        head = [
            "$(B)/resources/YMAKE_PYTHON3/bin/python3", "$(S)/build/scripts/jni_swig.py",
            "--swig", "$(B)/contrib/tools/swig/swig", "--default-module", "wrap", "--src", "$(S)/jni/wrap.swg",
        ]
        include = ["--", "-I$(B)", "-I$(S)", "-I$(S)/contrib/tools/swig/Lib/java", "-I$(S)/contrib/tools/swig/Lib"]
        package = ["--package-by-file", "ru/yandex/jni/wrap.swg"]
        cpp = lib.node_by_output(graph, "$(B)/jni/wrap_wrap.swg.h")
        self.assertEqual(cpp["kv"], {"p": "SW", "pc": "yellow"})
        self.assertEqual(cpp["outputs"], ["$(B)/jni/wrap_wrap.swg.h", "$(B)/jni/wrap_wrap.swg.cpp"])
        self.assertEqual(cpp["cmds"][0]["cmd_args"][2:], head[2:] + ["--out-header", "$(B)/jni/wrap_wrap.swg.h"]
                         + package + include + ["-o", "$(B)/jni/wrap_wrap.swg.cpp"])
        for path in ("$(S)/jni/api.i", "$(S)/jni/api.h", "$(B)/dep/gen.h", "$(S)/contrib/tools/swig/Lib/java/java.swg",
                     "$(S)/contrib/libs/jdk/include/jni.h", "$(S)/contrib/libs/jdk/include/jni_types.h"):
            self.assertIn(path, cpp["inputs"])
        compiled = lib.node_by_output(graph, "$(B)/jni/wrap_wrap.swg.cpp.pic.o")
        self.assertIn("-Wno-deprecated-declarations", compiled["cmds"][0]["cmd_args"])
        for path in ("$(B)/jni/wrap_wrap.swg.cpp", "$(B)/jni/wrap_wrap.swg.h", "$(S)/jni/api.h", "$(B)/dep/gen.h"):
            self.assertIn(path, compiled["inputs"])
        so = lib.node_by_output(graph, "$(B)/jni/libjni-lib.so")
        link = next(c["cmd_args"] for c in so["cmds"] if "$(S)/build/scripts/link_dyn_lib.py" in c["cmd_args"])
        self.assertIn("$(B)/jni/impl.cpp.pic.o", link)
        self.assertIn("$(B)/jni/wrap_wrap.swg.cpp.pic.o", link)
        self.assertFalse([n for n in graph["graph"] if any("skipped" in o for o in n["outputs"])])

        jsrc = lib.node_by_output(graph, "$(B)/jni/wrap.swg.jsrc")
        self.assertEqual(jsrc["kv"], {"p": "SW", "pc": "yellow"})
        self.assertEqual(jsrc["cmds"][0]["cmd_args"][2:], head[2:] + package
                         + ["--jsrc", "$(B)/jni/wrap.swg.jsrc"] + include + ["-o", "$(B)/jni/unused.cpp"])
        self.assertEqual(sorted(jsrc["inputs"]), sorted(cpp["inputs"]))
        gen = [n for n in graph["graph"] if "$(B)/dep/gen.h" in n["outputs"]]
        self.assertEqual(len(gen), 2)
        self.assertEqual({n["uid"] for n in gen} & set(cpp["deps"]) & set(jsrc["deps"]), set())

        jv = jv_node(graph, "$(B)/jni/jni-lib.jar")
        prep = jv["cmds"][0]["cmd_args"]
        self.assertEqual(prep[prep.index("--ya-start-command-file"):], [
            "--ya-start-command-file", "$(B)/jni/wrap.swg.jsrc", "--ya-end-command-file",
        ])
        self.assertIn(jsrc["uid"], jv["deps"])
        self.assertEqual(writer_args(jv, "bfg.txt"), [
            "jni/libjni-lib.so",
            "build/platform/java/jdk/platform-java-jdk.pkg.fake",
            "build/platform/java/jdk/jdk17/java-jdk-jdk17.pkg.fake",
            "build/platform/python/ymake_python3/platform-python-ymake_python3.pkg.fake",
            "contrib/tools/swig/Lib/java/libswig-Lib-java.a",
            "contrib/libs/jdk/libcontrib-libs-jdk.a",
        ])
        self.assertIn(so["uid"], jv["deps"])


class JavaStartTargetsTest(unittest.TestCase):
    def test_recursed_tests_and_dlls_are_results(self):
        bare = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\nRECURSE(jni)\nRECURSE_FOR_TESTS(ut)\n",
            "app/ut/ya.make": "JUNIT5()\nPEERDIR(app)\nEND()\n",
            "app/jni/ya.make": f"DLL(app_jni)\n{bare}SRCS(j.cpp)\nEND()\n",
            "app/jni/j.cpp": "int j;\n",
            "devtools/junit5-runner/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
            "devtools/jtest-annotations/junit5/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
            "build/platform/java/jacoco-agent/ya.make": resource_lib("JACOCO_AGENT", 6),
        })
        graph = lib.make(files, "app")
        results = [graph["graph"][ref]["outputs"][0] for ref in graph["result"]]
        self.assertEqual(results, ["$(B)/app/app.jar", "$(B)/app/jni/libapp_jni.so"])
        graph = lib.make(files, "app", "--target-platform-tests")
        results = [graph["graph"][ref]["outputs"][0] for ref in graph["result"]]
        self.assertEqual(results, ["$(B)/app/app.jar", "$(B)/app/jni/libapp_jni.so", "$(B)/app/ut/app-ut.test.cp.jar",
                                   "$(B)/app/ut/test-results/app-ut/meta.json"])
        jt = lib.node_by_output(graph, "$(B)/app/ut/app-ut.test.cp.jar")
        self.assertEqual(jt["kv"], {"p": "JT"})
        self.assertEqual(writer_args(jt, "run-bf.txt"), [
            "$(B)/app/ut/app-ut.jar",
            "$(B)/devtools/junit5-runner/devtools-junit5-runner.jar",
            "$(B)/devtools/jtest-annotations/junit5/jtest-annotations-junit5.jar",
            "$(B)/app/app.jar",
        ])
        self.assertEqual(jt["cmds"][1]["cmd_args"][1:], [
            "$(S)/build/scripts/make_manifest_from_bf.py", "$(B)/app/ut/run-bf.txt", "$(B)/app/ut/app-ut.test.cp.jar",
        ])


class JavaModuleMacrosTest(unittest.TestCase):
    def test_variable_srcdir_is_also_scanned_at_build_time(self):
        # _FILL_JAR_GEN_SRCS (build/plugins/java.py): resolve_arc_path leaves a
        # ${ARCADIA_ROOT} SRCDIR unresolved, so the jar build appends a scan of it.
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nJAVA_SRCS(SRCDIR ${ARCADIA_ROOT}/other/src **/*)\n"
                           "JAVA_SRCS(SRCDIR own **/*)\nEND()\n",
            "other/src/A.java": "class A {}\n",
            "app/own/B.java": "class B {}\n",
        })
        args = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.jar")["cmds"][0]["cmd_args"]
        self.assertIn("$(S)/other/src/A.java", args)
        start = args.index("DELIM")
        self.assertEqual(args[start:start + 4], ["DELIM", "--append", "-d", "$(S)/other/src"])
        self.assertEqual(args.count("DELIM"), 1)

    def test_exclude_with_trailing_separator(self):
        classpath = JavaDependencyManagementTest.library(
            self, "PEERDIR(contrib/java/x/a)\nDEPENDENCY_MANAGEMENT(contrib/java/x/a/2.0)\nEXCLUDE(contrib/java/x/b/)\n")
        self.assertEqual(classpath, ["contrib/java/x/a/2.0/a-2.0.jar"])

    def test_proto_sources_resolve_by_srcdir_and_root(self):
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(p/q)\nEND()\n",
            "p/q/ya.make": "PROTO_LIBRARY()\nSRCDIR(p)\nSRCS(a.proto r/b.proto)\nEND()\n",
            "p/a.proto": 'syntax = "proto3";\n',
            "r/b.proto": 'syntax = "proto3";\n',
            **contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=7),
        })
        graph = lib.make(files, "app")
        for jsrc, proto in (("$(B)/p/a.jsrc", "p/a.proto"), ("$(B)/r/b.jsrc", "r/b.proto")):
            pb = lib.node_by_output(graph, jsrc)
            self.assertEqual(pb["cmds"][0]["cmd_args"][-1], proto)

    def test_ev_source_is_copied_to_a_proto(self):
        files = java_tree(**{
            "app/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(p)\nEND()\n",
            "p/ya.make": "PROTO_LIBRARY()\nSRCS(a.proto events.ev)\nEND()\n",
            "p/a.proto": 'syntax = "proto3";\n',
            "p/events.ev": 'syntax = "proto3";\n',
            **contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=7),
        })
        graph = lib.make(files, "app")
        ev = lib.node_by_output(graph, "$(B)/p/events_ev.proto")
        self.assertEqual(ev["kv"], {"p": "EV", "pc": "yellow"})
        self.assertEqual(ev["cmds"][0]["cmd_args"][2:], ["copy", "$(S)/p/events.ev", "$(B)/p/events_ev.proto"])
        pb = lib.node_by_output(graph, "$(B)/p/events_ev.jsrc")
        self.assertEqual(pb["cmds"][0]["cwd"], "$(B)")
        self.assertEqual(pb["cmds"][0]["cmd_args"][-1], "p/events_ev.proto")
        self.assertIn(ev["uid"], pb["deps"])
        jv = lib.node_by_output(graph, "$(B)/p/p.jar")
        args = jv["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("DELIM") - 2:args.index("DELIM")], ["$(B)/p/a.jsrc", "$(B)/p/events_ev.jsrc"])

    def test_from_sandbox_outputs_are_module_dependencies(self):
        files = java_tree(**{
            "app/ya.make": (
                "JAVA_LIBRARY_NON_SPLIT()\nFROM_SANDBOX(FILE 42 OUT_NOAUTO data.tgz)\n"
                "JAVA_SRCS(SRCDIR ${ARCADIA_BUILD_ROOT}/app FILES data.tgz)\nEND()\n"
            ),
        })
        graph = lib.make(files, "app")
        sb = lib.node_by_output(graph, "$(B)/app/data.tgz")
        self.assertEqual(sb["kv"], {"p": "SB", "pc": "yellow", "show_out": "yes"})
        jv = jv_node(graph, "$(B)/app/app.jar")
        self.assertIn("$(B)/app/data.tgz", jv["cmds"][0]["cmd_args"])
        self.assertIn("$(B)/app/data.tgz", jv["inputs"])
        self.assertIn(sb["uid"], jv["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
