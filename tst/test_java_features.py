"""Java compiler, packaging and generator contracts beyond the basic jar."""
import base64
import unittest

import lib


def tree(body, module="JAVA_LIBRARY", **extra):
    files = lib.java_tree(**{
        "app/ya.make": f"{module}()\n{body}\nEND()\n",
        "app/src/Main.kt": "class Main\n",
        "app/src/Main.java": "class Main {}\n",
        "app/src/Skip.java": "class Skip {}\n",
        "app/data.txt": "resource\n",
        "build/platform/java/kotlin/ya.make": lib.resource_lib("KOTLIN_COMPILER", 10),
        "build/platform/java/scriptgen/ya.make": lib.resource_lib("SCRIPTGEN", 11),
        "build/platform/java/uberjar/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
        "build/platform/java/uberjar/uberjar17/ya.make": lib.resource_lib("UBERJAR17", 12),
        "build/platform/java/uberjar/uberjar26/ya.make": lib.resource_lib("UBERJAR26", 13),
    })
    for artifact in ("kotlin-stdlib-jdk8", "kotlin-annotation-processing"):
        root = f"contrib/java/org/jetbrains/kotlin/{artifact}"
        files[f"{root}/2.3.10/ya.make"] = "JAVA_CONTRIB()\nJAR_RESOURCE(14)\nEND()\n"
        files[f"{root}/ya.make"] = f"JAVA_CONTRIB_PROXY()\nPEERDIR({root}/2.3.10)\nEND()\n"
    lib.tool_program(files, "contrib/tools/ijar", "ijar")
    files.update(extra)
    return files


def command(node, script):
    return next(c["cmd_args"] for c in node["cmds"] if any(a.endswith(script) for a in c["cmd_args"]))


class JavaFeaturesTest(unittest.TestCase):
    def test_kotlin_plugins_kapt_and_abi(self):
        files = tree("""
WITH_KOTLIN()
WITH_KAPT(GENERATE_KOTLIN)
KAPT_OPTS(plugin:test:enabled=true)
KAPT_ANNOTATION_PROCESSOR(example.Processor)
KAPT_ANNOTATION_PROCESSOR_CLASSPATH(processor)
KAPT_ANNOTATION_PROCESSOR_OPTIONS(key=value)
KOTLINC_FLAGS(-Werror)
WITH_KOTLINC_ALLOPEN(annotation=example.Open)
WITH_KOTLINC_LOMBOK(config=lombok.config)
WITH_KOTLINC_NOARG(annotation=example.NoArg)
WITH_KOTLINC_SERIALIZATION()
WITH_KOTLINC_DETEKT(config=detekt.yml)
ENABLE_KOTLIN_ABI_JAR()
JAVA_SRCS(SRCDIR src **/*.kt **/*.java EXCLUDE src/Skip.java)
""", **{"processor/ya.make": "JAVA_CONTRIB()\nJAR_RESOURCE(20)\nEND()\n"})
        files["consumer/ya.make"] = "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(app)\nEND()\n"
        graph = lib.make(files, "consumer")
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        kotlin = next(c["cmd_args"] for c in jar["cmds"]
                      if "--kotlin" in c["cmd_args"] and any(a.endswith("run_javac.py") for a in c["cmd_args"]) and not any(a.endswith("with_kapt_args.py") for a in c["cmd_args"]))
        self.assertIn("-Werror", kotlin)
        for plugin in ("allopen", "lombok", "noarg", "serialization", "jvm-abi-gen"):
            self.assertTrue(any(f"kotlin-{plugin}-plugin.jar" in a for a in kotlin), kotlin)
        self.assertIn("plugin:org.jetbrains.kotlin.allopen:annotation=example.Open", kotlin)
        self.assertIn("plugin:detekt-compiler-plugin:config=detekt.yml", kotlin)
        self.assertIn("$(B)/app/app-stripped.jar", jar["outputs"])
        self.assertNotIn("$(S)/app/src/Skip.java", jar["inputs"])
        kapt = command(jar, "with_kapt_args.py")
        self.assertIn("$(B)/processor/processor.jar", kapt)
        self.assertIn("key=value", kapt)
        self.assertIn("kapt.kotlin.generated=$(B)/app/kapt_gen_src", kapt)
        self.assertIn("plugin:org.jetbrains.kotlin.kapt3:processors=example.Processor", kapt)
        processor = lib.node_by_output(graph, "$(B)/processor/processor.jar")
        self.assertIn(processor["uid"], jar["deps"])
        ijar = lib.node_by_output(graph, "$(B)/app/app-interface.jar")
        self.assertIn("--metadata_source", ijar["cmds"][0]["cmd_args"])
        self.assertIn("$(B)/app/app-stripped.jar", ijar["cmds"][0]["cmd_args"])

    def test_sources_jar_generated_sources_and_package_prefix(self):
        files = tree("""
WITH_KOTLIN()
ENABLE(SOURCES_JAR)
JAVA_SRCS(RESOURCES SRCDIR src PACKAGE_PREFIX example.api FILES src/Main.java)
JAVA_SRCS(SKIP_CHECK_SRCDIR SRCDIR ${ARCADIA_BUILD_ROOT}/app/gen **/*.java EXCLUDE **/Skip.java)
JAVA_EXTERNAL_DEPENDENCIES(data.txt ${ARCADIA_ROOT}/external.txt)
ANNOTATION_PROCESSOR(example.First example.Second)
""", **{"external.txt": "external\n"})
        jar = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.jar")
        prep = command(jar, "prepare_jar_build.py")
        self.assertIn("--with-sources-jar", prep)
        self.assertIn("--all-resources", prep)
        self.assertIn("--exclude-patterns", prep)
        self.assertIn("--resolve-kotlin", prep)
        self.assertIn("$(B)/app/app-sources.jar", jar["outputs"])
        self.assertTrue(any("$(B)/app/src/example/api" in c["cmd_args"] for c in jar["cmds"]))
        self.assertTrue(any("example.First,example.Second" in c["cmd_args"] for c in jar["cmds"]))
        self.assertIn("$(S)/app/data.txt", jar["inputs"])
        self.assertIn("$(S)/external.txt", jar["inputs"])

    def test_error_prone_jdk_versions_and_preview(self):
        for jdk, version in ((8, "2.7.1"), (11, "2.7.1"), (17, "2.10.0"),
                             (21, "2.30.0"), (22, "2.30.0"), (23, "2.30.0"),
                             (24, "2.47.0"), (25, "2.47.0"), (26, "2.47.0")):
            with self.subTest(jdk=jdk):
                peer = "build/platform/java/error_prone" + (f"/{version}" if jdk != 8 else "")
                resource = "ERROR_PRONE_" + version.replace(".", "_") if jdk != 8 else "ERROR_PRONE"
                files = tree(f"JDK_VERSION({jdk})\nUSE_ERROR_PRONE()\nENABLE_PREVIEW()\nJAVAC_FLAGS(-Werror)",
                             module="JAVA_LIBRARY_NON_SPLIT", **{
                                 f"build/platform/java/jdk/jdk{jdk}/ya.make": lib.resource_lib(f"JDK{jdk}", 30),
                                 f"{peer}/ya.make": lib.resource_lib(resource, 31),
                             })
                jar = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.jar")
                args = command(jar, "run_javac.py")
                self.assertIn(f"$(B)/resources/{resource}/error_prone.jar", args)
                self.assertIn("--enable-preview", args)
                self.assertEqual(args[args.index("--release") + 1], str(jdk))
                self.assertIn("-Werror", args)
                self.assertEqual("--should-stop=ifError=FLOW" in args, jdk >= 24)
                self.assertEqual("-Xep:InlineMeInliner:OFF" in args, jdk in (8, 11))

    def test_uberjar_scripts_and_bundled_jdk(self):
        files = tree("""
UBERJAR()
JDK_VERSION(17)
JAR_MAIN_CLASS(example.Main)
ENABLE_PREVIEW()
JAVA_SRCS(SRCDIR src *.java)
PEERDIR(dep native)
ADD_DLLS_TO_JAR()
WITH_JDK()
GENERATE_SCRIPT(OUT launch.sh second.sh TEMPLATE launch.tmpl CUSTOM_PROPERTY greeting hello world)
UBERJAR_HIDING_PREFIX(shaded)
UBERJAR_HIDE_EXCLUDE_PATTERN(public.**)
UBERJAR_HIDE_INCLUDE_PATTERN(internal.**)
UBERJAR_PATH_EXCLUDE_PREFIX(META-INF/old)
UBERJAR_MANIFEST_TRANSFORMER_MAIN(example.Main)
UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Implementation-Title Example App)
UBERJAR_APPENDING_TRANSFORMER(reference.conf)
UBERJAR_SERVICES_RESOURCE_TRANSFORMER()
""", module="JAVA_PROGRAM", **{
            "app/launch.tmpl": "template\n",
            "dep/ya.make": "JAVA_CONTRIB()\nJAR_RESOURCE(21)\nEND()\n",
            "native/ya.make": f"DLL()\n{lib.BARE_MODULE}SRCS(a.cpp)\nEND()\n",
            "native/a.cpp": "int value;\n",
            "build/platform/java/jdk/jdk17/ya.make": lib.resource_lib("JDK17", 3).replace(
                "END()", "DECLARE_EXTERNAL_RESOURCE(WITH_JDK17 sbr:22)\nEND()"),
        })
        graph = lib.make(files, "app")
        jar = lib.node_by_output(graph, "$(B)/app/app.jar")
        shade = command(jar, "ru.yandex.devtools.emigrante.Main")
        for value in ("--shade-prefix", "shaded", "--shade-exclude", "public.**", "--shade-include", "internal.**",
                      "--uber-exclude", "META-INF/old", "--manifest-main", "example.Main", "--manifest-attribute",
                      "Implementation-Title:Example App", "--append-transformer", "reference.conf", "--service-transformer"):
            self.assertIn(value, shade)
        run = lib.node_by_output(graph, "$(B)/app/app.run.cp.jar")
        for output in ("$(B)/app/launch.sh", "$(B)/app/second.sh", "$(B)/app/jdk.tar", "$(B)/app/app.tar"):
            self.assertIn(output, run["outputs"])
        script = command(run, "/scriptgen")
        self.assertIn("IS_UBERJAR=yes", script)
        self.assertIn("MAIN_CLASS=example.Main", script)
        self.assertIn(base64.b64encode(b"greeting=hello world").decode(), script)
        self.assertIn("$(B)/resources/WITH_JDK17", command(run, "tar_directory.py"))
        self.assertTrue(any("$(B)/native/libnative.so" in c["cmd_args"] for c in run["cmds"]))

    def test_local_sources_and_empty_contrib(self):
        files = tree("LOCAL_JAR(local.jar)\nLOCAL_SOURCES_JAR(src.jar)", module="JAVA_CONTRIB", **{
            "app/local.jar": "jar", "app/src.jar": "sources",
        })
        jar = lib.node_by_output(lib.make(files, "app"), "$(B)/app/app.jar")
        self.assertIn("$(S)/app/src.jar", jar["inputs"])
        self.assertTrue(any("$(S)/app/src.jar" in c["cmd_args"] for c in jar["cmds"]))
        empty = lib.node_by_output(lib.make(tree("", module="JAVA_CONTRIB"), "app"), "$(B)/app/app.jar")
        self.assertTrue(any("cfvm" in c["cmd_args"] and "$(B)/app/app.jar" in c["cmd_args"] for c in empty["cmds"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
