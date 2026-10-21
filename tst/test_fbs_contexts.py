import unittest

import lib
from test_proto_flatc import flatc_files


class FbsContextTest(unittest.TestCase):
    def test_java_generated_schema_keeps_the_native_generator_dependency(self):
        files = lib.java_tree(**flatc_files())
        files.update({
            "f/ya.make": (
                "FBS_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nONLY_TAGS(JAVA_FBS)\n"
                "SET(CONTEXT java_fbs)\n"
                "RUN_PROGRAM(tools/gen --source ${CURDIR} ENV CONTEXT=$CONTEXT DIR=${BINDIR} CWD ${BINDIR} OUT_NOAUTO model.fbs dep.fbs OUTPUT_INCLUDES ${BINDIR}/dep.fbs)\n"
                "SRCS(model.fbs)\nEND()\n"
            ),
            "contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make": "JAVA_CONTRIB()\nEND()\n",
        })
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "f", "--sandboxing")
        generated = lib.node_by_output(graph, "$(B)/f/model.fbs")
        self.assertEqual(generated["cmds"][0]["env"]["CONTEXT"], "java_fbs")
        self.assertEqual(generated["cmds"][0]["env"]["DIR"], "$(B)/f")
        self.assertEqual(generated["cmds"][0]["cwd"], "$(B)/f")
        self.assertEqual(generated["cmds"][0]["cmd_args"][-2:], ["--source", "$(S)/f"])
        source = lib.node_by_output(graph, "$(B)/f/model.fbs.jsrc")
        self.assertIn(generated["uid"], source["deps"])
        self.assertIn("$(B)/f/model.fbs", source["inputs"])
        self.assertIn("$(B)/f/dep.fbs", source["inputs"])
        jar = lib.node_by_output(graph, "$(B)/f/f.jar")
        self.assertIn(source["uid"], jar["deps"])

    def test_java_consumer_does_not_enable_an_excluded_fbs_variant(self):
        files = lib.java_tree(**flatc_files())
        files.update({
            "f/ya.make": "FBS_LIBRARY()\nONLY_TAGS(CPP_FBS)\nSRCS(a.fbs64)\nEND()\n",
            "f/a.fbs64": "table A {}\n",
            "app/ya.make": "JAR_LIBRARY()\nPEERDIR(f)\nEND()\n",
        })
        graph = lib.make(files, "app", "--sandboxing")
        lib.node_by_output(graph, "$(B)/app/app.jar")
        self.assertFalse([n for n in graph["graph"] if any(o.startswith("$(B)/f/") for o in n["outputs"])])

    def test_java_fbs_peers_keep_schema_inputs_and_jar_dependencies(self):
        files = lib.java_tree(**flatc_files())
        files["contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make"] = "JAVA_CONTRIB()\nEND()\n"
        for directory, schema in (("schemas/base", "base"), ("schemas/top", "top")):
            peer = "PEERDIR(schemas/base)\n" if schema == "top" else ""
            files[directory + "/ya.make"] = (
                "FBS_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                f"ADDINCL(GLOBAL FOR flatc {directory})\nADDINCL(GLOBAL ${{ARCADIA_BUILD_ROOT}}/{directory})\nSRCS({schema}.fbs {schema}.fbs64)\n"
                + peer + "ONLY_TAGS(CPP_FBS JAVA_FBS)\nEND()\n"
            )
            if schema == "base":
                files[directory + "/ya.make"] = files[directory + "/ya.make"].replace("END()", "ADDINCL(FOR flatc private)\nEND()")
            for ext in ("fbs", "fbs64"):
                files[f"{directory}/{schema}.{ext}"] = (
                    f'include "base.{ext}";\n' if schema == "top" else ""
                ) + "table Value {}\n"
        files["private/unused.fbs"] = "table Private {}\n"
        graph = lib.make(files, "schemas/top", "--sandboxing")
        producer = lib.node_by_output(graph, "$(B)/schemas/top/top.fbs.jsrc")
        self.assertIn("$(S)/schemas/base/base.fbs", producer["inputs"])
        self.assertIn("$(S)/schemas/base", producer["cmds"][0]["cmd_args"])
        self.assertEqual(producer["requirements"]["cpu"], 1)
        self.assertNotIn("$(S)/private", producer["cmds"][0]["cmd_args"])
        base_source = lib.node_by_output(graph, "$(B)/schemas/base/base.fbs.jsrc")
        self.assertIn("$(S)/private", base_source["cmds"][0]["cmd_args"])
        top = lib.node_by_output(graph, "$(B)/schemas/top/schemas-top.jar")
        base = lib.node_by_output(graph, "$(B)/schemas/base/schemas-base.jar")
        self.assertIn(base["uid"], top["deps"])
        self.assertIn(producer["uid"], top["deps"])

    def test_cpp_consumer_keeps_unselected_java_context_command_effects(self):
        files = lib.java_tree(**flatc_files())
        files.update({
            "f/ya.make": (
                "FBS_LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nNO_LIBC()\n"
                "SRCS(a.fbs64 b.fbs use.cpp)\nONLY_TAGS(CPP_FBS JAVA_FBS)\n"
                "IF(CPP_FBS)\nCFLAGS(-DCPP_LOCAL)\nCFLAGS(GLOBAL -DCPP_GLOBAL)\nFLATC_FLAGS(--cpp-context)\nENDIF()\n"
                "IF(JAVA_FBS)\nCFLAGS(-DJAVA_LOCAL)\nCFLAGS(GLOBAL -DJAVA_GLOBAL)\nFLATC_FLAGS(--java-context)\nENDIF()\nEND()\n"
            ),
            "f/a.fbs64": "table A {}\n",
            "f/b.fbs": "table B {}\n",
            "f/use.cpp": "",
            "consumer/ya.make": "PROGRAM()\nNO_UTIL()\nNO_RUNTIME()\nNO_LIBC()\nPEERDIR(f)\nSRCS(main.cpp)\nEND()\n",
            "consumer/main.cpp": "",
        })
        graph = lib.make(files, "consumer", "--target-platform", "default-linux-x86_64", "--sandboxing")
        producer = lib.node_by_output(graph, "$(B)/f/a.fbs64.h")
        self.assertIn("--java-context", producer["cmds"][0]["cmd_args"])
        archive = lib.node_by_output(graph, "$(B)/f/libf.a")
        for output in ("a.fbs64.cpp.o", "use.cpp.o"):
            cc = lib.node_by_output(graph, "$(B)/f/" + output)
            args = cc["cmds"][0]["cmd_args"]
            self.assertIn("-DJAVA_LOCAL", args)
            self.assertIn("-DCPP_GLOBAL", args)
            self.assertNotIn("-DJAVA_GLOBAL", args)
            self.assertNotIn("-msse2", args)
            self.assertNotIn("-nostdinc++", args)
            self.assertIn(cc["uid"], archive["deps"])
            if output.startswith("a."):
                self.assertIn(producer["uid"], cc["deps"])
                self.assertIn("$(B)/f/a.fbs64.cpp", cc["inputs"])
        normal = lib.node_by_output(graph, "$(B)/f/b.fbs.cpp.o")
        self.assertIn("-DCPP_LOCAL", normal["cmds"][0]["cmd_args"])
        self.assertIn(normal["uid"], archive["deps"])
        self.assertFalse([n for n in graph["graph"] if any(o.endswith((".jsrc", ".jar")) and o.startswith("$(B)/f/") for o in n["outputs"])])

    def test_java_only_peer_does_not_select_the_cpp_fbs_context(self):
        files = lib.java_tree(**flatc_files())
        files["contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make"] = "JAVA_CONTRIB()\nEND()\n"
        for directory in ("top", "base"):
            files[directory + "/ya.make"] = (
                "FBS_LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nNO_LIBC()\nSRCS(a.fbs a.fbs64)\n"
                + ("ONLY_TAGS(JAVA_FBS)\nPEERDIR(base)\n" if directory == "top" else "")
                + "END()\n"
            )
            files[directory + "/a.fbs"] = "table A {}\n"
            files[directory + "/a.fbs64"] = "table A {}\n"
        graph = lib.make(files, "top", "--sandboxing")
        top = lib.node_by_output(graph, "$(B)/top/top.jar")
        base = lib.node_by_output(graph, "$(B)/base/base.jar")
        self.assertIn(base["uid"], top["deps"])
        self.assertFalse([n for n in graph["graph"] if any(
            o.startswith("$(B)/base/") and (o.endswith(".o") or o.endswith(".a") or o.endswith(".fbs64.h"))
            for o in n["outputs"])])
        files["base/ya.make"] = files["base/ya.make"].replace("END()", "ONLY_TAGS(CPP_FBS)\nEND()")
        excluded = lib.make(files, "top", "--sandboxing")
        lib.node_by_output(excluded, "$(B)/top/top.jar")
        self.assertFalse([n for n in excluded["graph"] if any(o.startswith("$(B)/base/") for o in n["outputs"])])

    def test_shared_output_bindings_keep_host_and_target_platforms_separate(self):
        files = lib.java_tree(**flatc_files())
        files.update({
            "f/ya.make": (
                "FBS_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(a.fbs64 b.fbs)\nONLY_TAGS(CPP_FBS JAVA_FBS)\n"
                "IF(ARCH_X86_64)\nONLY_TAGS(CPP_FBS)\nENDIF()\n"
                "IF(CPP_FBS)\nFLATC_FLAGS(--cpp-context)\nENDIF()\n"
                "IF(JAVA_FBS)\nFLATC_FLAGS(--java-context)\nENDIF()\n"
                "END()\n"
            ),
            "f/a.fbs64": "table A {}\n",
            "f/b.fbs": "table B {}\n",
            "contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make": "JAVA_CONTRIB()\nEND()\n",
        })
        lib.tool_program(files, "tools/helper", "helper")
        files["tools/helper/ya.make"] = files["tools/helper/ya.make"].replace("END()", "PEERDIR(f)\nEND()")
        lib.tool_program(files, "consumer", "consumer")
        files["consumer/ya.make"] = files["consumer/ya.make"].replace("END()", "PEERDIR(f)\nRUN_PROGRAM(tools/helper OUT from_host.cpp)\nEND()")
        graph = lib.make(files, "consumer", "--sandboxing")
        compiled = {n["platform"]: n for n in graph["graph"]
                    if any(o.startswith("$(B)/f/a.fbs64.cpp") and o.endswith(".o") for o in n["outputs"])}
        self.assertEqual(set(compiled), {"default-linux-aarch64", "default-linux-x86_64"})
        self.assertEqual(compiled["default-linux-x86_64"]["cmds"][0]["cmd_args"].count("-nostdinc++"), 1)
        self.assertEqual(compiled["default-linux-aarch64"]["cmds"][0]["cmd_args"].count("-nostdinc++"), 0)
        for platform, cc in compiled.items():
            archives = [n for n in graph["graph"] if n["platform"] == platform and "$(B)/f/libf.a" in n["outputs"]]
            self.assertEqual(len(archives), 1)
            self.assertIn(cc["uid"], archives[0]["deps"])
        producers = {n["platform"]: n for n in graph["graph"] if "$(B)/f/a.fbs64.h" in n["outputs"]}
        self.assertEqual(set(producers), set(compiled))
        self.assertIn("--cpp-context", producers["default-linux-x86_64"]["cmds"][0]["cmd_args"])
        self.assertIn("--java-context", producers["default-linux-aarch64"]["cmds"][0]["cmd_args"])

    def test_cpp_and_java_bind_shared_native_files_in_their_own_contexts(self):
        for tags in ("CPP_FBS", "CPP_FBS JAVA_FBS", "JAVA_FBS"):
            with self.subTest(tags=tags):
                files = lib.java_tree(**flatc_files())
                files.update({
                    "f/ya.make": (
                        "FBS_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                        "SRCS(a.fbs64 b.fbs use.cpp)\nPEERDIR(peer)\n"
                        "IF(CPP_FBS)\nCFLAGS(-DCPP_LOCAL)\nCXXFLAGS(-DCPP_CXX)\n"
                        "CFLAGS(GLOBAL -DCPP_GLOBAL)\nFLATC_FLAGS(--cpp-context)\nENDIF()\n"
                        "IF(JAVA_FBS)\nCFLAGS(-DJAVA_LOCAL)\nCXXFLAGS(-DJAVA_CXX)\n"
                        "NO_COMPILER_WARNINGS()\n"
                        "CFLAGS(GLOBAL -DJAVA_GLOBAL)\nFLATC_FLAGS(--java-context)\nENDIF()\n"
                        f"ONLY_TAGS({tags})\nEND()\n"
                    ),
                    "f/a.fbs64": "table A {}\n",
                    "f/b.fbs": "table B {}\n",
                    "f/use.cpp": "",
                    "peer/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCFLAGS(GLOBAL -DPEER_GLOBAL)\nSRCS(x.cpp)\nEND()\n",
                    "peer/x.cpp": "",
                    "contrib/java/com/google/flatbuffers/flatbuffers-java/24.3.25/ya.make": "JAVA_CONTRIB()\nEND()\n",
                })
                graph = lib.make(files, "f", "--sandboxing", "--target-platform", "default-linux-x86_64")
                nodes = graph["graph"]
                if "CPP_FBS" in tags:
                    producer = lib.node_by_output(graph, "$(B)/f/a.fbs64.h")
                    archive = lib.node_by_output(graph, "$(B)/f/libf.a")
                    java = "JAVA_FBS" in tags
                    local = "JAVA" if java else "CPP"
                    self.assertIn("--" + local.lower() + "-context", producer["cmds"][0]["cmd_args"])
                    for output in ("a.fbs64.cpp.o", "use.cpp.o"):
                        cc = lib.node_by_output(graph, "$(B)/f/" + output)
                        self.assertEqual(sum("$(B)/f/" + output in n["outputs"] for n in nodes), 1)
                        args = cc["cmds"][0]["cmd_args"]
                        self.assertIn("-D" + local + "_LOCAL", args)
                        self.assertIn("-D" + local + "_CXX", args)
                        self.assertIn("-DCPP_GLOBAL", args)
                        self.assertIn("-DPEER_GLOBAL", args)
                        self.assertNotIn("-DJAVA_GLOBAL", args)
                        self.assertEqual(args.count("-msse2"), 0 if java else 1)
                        self.assertEqual(args.count("-nostdinc++"), 0 if java else 1)
                        self.assertEqual(args.count("-Wno-everything"), 2 if java else 0)
                        self.assertIn(cc["uid"], archive["deps"])
                        if output.startswith("a."):
                            self.assertIn(producer["uid"], cc["deps"])
                            self.assertIn("$(B)/f/a.fbs64.cpp", cc["inputs"])
                    plain = lib.node_by_output(graph, "$(B)/f/b.fbs.cpp.o")
                    self.assertIn("-DCPP_LOCAL", plain["cmds"][0]["cmd_args"])
                    self.assertIn(plain["uid"], archive["deps"])
                else:
                    self.assertFalse([n for n in nodes if any(o.startswith("$(B)/f/") and o.endswith(".o") for o in n["outputs"])])
                    self.assertFalse([n for n in nodes if "$(B)/f/a.fbs64.h" in n["outputs"]])
                if "JAVA_FBS" in tags:
                    fj = lib.node_by_output(graph, "$(B)/f/b.fbs.jsrc")
                    jar = lib.node_by_output(graph, "$(B)/f/f.jar")
                    self.assertEqual(fj["kv"]["p"], "FJ")
                    self.assertIn(fj["uid"], jar["deps"])
                    self.assertIn("$(B)/f/b.fbs.jsrc", jar["inputs"])
                    self.assertIn(jar["uid"], graph["result"])


if __name__ == "__main__":
    unittest.main()
