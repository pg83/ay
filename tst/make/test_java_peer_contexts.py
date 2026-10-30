import json

from tst.make.lib import MakeTest, PROJECT


class JavaPeerContextsTest(MakeTest):
    def test_duplicate_java_module_end_is_a_configuration_error(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("END", result.stderr)
        self.node(self.graph("-k"), "answer.jar")

    def test_junit_default_docker_images_ignore_quoted_empty_entries(self):
        image = f"docker://registry.example.com/e2e@sha256:{'a' * 64}"
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + f'\nDOCKER_IMAGES_VALUE = "" \'\' e2e={image}\n')
        self.write("ya.make", "JUNIT5(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        graph = self.graph("-t")
        commands = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"] if any(arg.endswith("/fetch_from_docker_repo.py") for arg in command["cmd_args"])]
        self.assertEqual(len(commands), 1)
        command = commands[0]
        self.assertEqual(command[command.index("--tag") + 1], "e2e")
        self.assertEqual(command[command.index("--link") + 1], image)

    def test_dollar_literals_in_exclusion_variables_do_not_remove_peers(self):
        self.program("peer")
        self.write("ya.make", (self.project / "ya.make").read_text().replace("END()", 'SET(EXCLUSIONS Outer${"$"}Inner)\nEXCLUDE(${EXCLUSIONS})\nEND()'))
        self.write("peer/ya.make", "JAVA_LIBRARY(peer)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
        self.write("peer/Value.java", "public class Value {}\n")
        self.assertIn(f"$(B)/{PROJECT}/peer/peer.jar", self.node(self.graph(), "answer.run.cp.jar")["inputs"])

    def test_java_set_append_requires_a_variable_name(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nSET_APPEND()\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_junit_legacy_docker_image_values_ignore_empty_entries(self):
        image = f"docker://registry.example.com/e2e@sha256:{'a' * 64}"
        self.write("ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nSET(DOCKER_IMAGES_VALUE \"\" '' e2e={image})\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        graph = self.graph("-t")
        commands = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"] if any(arg.endswith("/fetch_from_docker_repo.py") for arg in command["cmd_args"])]
        self.assertEqual(len(commands), 1)
        command = commands[0]
        self.assertEqual(command[command.index("--tag") + 1], "e2e")
        self.assertEqual(command[command.index("--link") + 1], image)

    def test_release_java_without_a_sources_jar_default(self):
        config = self.root / "build/conf/java.conf"
        defaults = 'SOURCES_JAR=no\nwhen ($BUILD_TYPE == "DEBUG" && $NO_DEBUGINFO != "yes") {\n    SOURCES_JAR=yes\n}\n'
        contents = config.read_text()
        self.assertEqual(contents.count(defaults), 1)
        self.write_tree("build/conf/java.conf", contents.replace(defaults, ""))
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        archive = self.node(self.graph("--build=release"), "answer.jar")
        self.assertNotIn(f"$(B)/{PROJECT}/answer-sources.jar", archive["outputs"])

    def test_keep_going_reports_missing_includes_in_java_modules(self):
        self.write("Answer.java", "public class Answer {}\n")
        self.write("healthy/ya.make", "JAVA_LIBRARY(healthy)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
        self.write("healthy/Value.java", "public class Value {}\n")
        for declaration in ("JAVA_LIBRARY", "JAVA_PROGRAM", "JUNIT5"):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"{declaration}(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nINCLUDE(missing.inc)\nEND()\nRECURSE(healthy)\n")
                result = self.make("-k", "-t")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("missing.inc", result.stderr)
                self.assertTrue(any(path.endswith(f"/{PROJECT}/healthy/healthy.jar") for node in json.loads(result.stdout)["graph"] for path in node["outputs"]))

    def test_keep_going_reports_failed_java_compilation_submodules(self):
        self.write("Answer.java", "public class Answer {}\n")
        for declaration, tag in (("JAVA_PROGRAM", "JAR_COMPILATION"), ("JUNIT5", "JUNIT5_JAR_COMPILATION")):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"{declaration}(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nIF(MODULE_TAG == {tag})\nINCLUDE(missing.inc)\nENDIF()\nEND()\n")
                result = self.make("-k", "-t")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("missing.inc", result.stderr)

    def test_files_copies_java_sources_into_the_build_directory(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nFILES(Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        graph = self.graph()
        self.assertIn(f"$(S)/{PROJECT}/Answer.java", self.node(graph, "Answer.java")["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/Answer.java", self.node(graph, "answer.jar")["inputs"])

    def test_files_rejects_a_duplicate_generated_java_source(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_PYTHON3(generate.py OUT Generated.java)\nFILES(${BINDIR}/Generated.java)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nPath("Generated.java").write_text("public class Generated {}\\n")\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Generated.java", result.stderr)

    def test_addincl_java_peer_keeps_its_archive_dependency(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR(ADDINCL {PROJECT}/peer)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("peer/ya.make", "JAVA_LIBRARY(peer)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
        self.write("peer/Value.java", "public class Value {}\n")
        self.assertIn(f"$(B)/{PROJECT}/peer/peer.jar", self.node(self.graph(), "answer.run.cp.jar")["inputs"])

    def test_unused_unterminated_java_variable_does_not_change_sources(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nSET(BROKEN prefix${UNTERMINATED)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.assertIn(f"$(S)/{PROJECT}/Answer.java", self.node(self.graph(), "answer.jar")["inputs"])

    def test_contrib_proxy_requires_a_managed_version(self):
        dependency = "contrib/java/org/apache/commons/commons-lang3"
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({dependency})\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(dependency, result.stderr)

    def test_managed_contrib_proxy_rejects_a_missing_version(self):
        dependency = "contrib/java/org/apache/commons/commons-lang3"
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({dependency})\nDEPENDENCY_MANAGEMENT({dependency}/__ay_missing__)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"{dependency}/__ay_missing__", result.stderr)

    def test_managed_contrib_proxy_rejects_an_excluded_java_replacement(self):
        dependency = "contrib/java/org/apache/commons/commons-lang3"
        replacement = f"{dependency}/__ay_excluded__"
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({dependency})\nDEPENDENCY_MANAGEMENT({replacement})\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write_tree(replacement + "/ya.make", "FBS_LIBRARY()\nEXCLUDE_TAGS(JAVA_FBS)\nSRCS(answer.fbs)\nEND()\n")
        self.write_tree(replacement + "/answer.fbs", "namespace e2e;\ntable Answer { value:int; }\nroot_type Answer;\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(replacement, result.stderr)

    def test_java_peerdir_policy_rejects_restricted_native_libraries(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR(contrib/restricted/boost/hana)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("peerdir policy", result.stderr)
        graph = self.graph("-k")
        self.node(graph, "answer.jar")
        self.assertFalse(any("/contrib/restricted/boost/" in path for node in graph["graph"] for path in node["outputs"]))

    def test_exclusions_expand_mixed_empty_and_scalar_variables(self):
        self.program("parent")
        makefile = (self.project / "ya.make").read_text().replace("END()", f"SET(EMPTY)\nSET(FIRST {PROJECT}/drop_b)\nSET(SHORTHAND {PROJECT}/drop_c)\nSET(ROOT {PROJECT})\nSET(TAIL /drop_a)\nEXCLUDE(${{EMPTY}})\nEXCLUDE({PROJECT}/drop_a ${{FIRST}} ${{EMPTY}})\nEXCLUDE($SHORTHAND ${{ROOT}}${{TAIL}})\nEND()")
        self.write("ya.make", makefile)
        self.write("parent/ya.make", "JAVA_LIBRARY(parent)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nPEERDIR(" + " ".join(f"{PROJECT}/{name}" for name in ("kept", "drop_a", "drop_b", "drop_c")) + ")\nEND()\n")
        self.write("parent/Value.java", "public class Value {}\n")
        for name in ("kept", "drop_a", "drop_b", "drop_c"):
            self.write(f"{name}/ya.make", f"JAVA_LIBRARY({name})\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
            self.write(f"{name}/Value.java", "public class Value {}\n")
        inputs = self.node(self.graph(), "answer.run.cp.jar")["inputs"]
        for name in ("parent", "kept", "drop_a", "drop_b", "drop_c"):
            self.assertEqual(f"$(B)/{PROJECT}/{name}/{name}.jar" in inputs, name in ("parent", "kept"), name)

    def test_empty_java_compiler_variable_name_is_a_syntax_error(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nJAVAC_FLAGS(${})\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_empty_exclusion_variable_does_not_remove_dependencies(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/peer)\nEXCLUDE(${{}})\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("peer/ya.make", "JAVA_LIBRARY(peer)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
        self.write("peer/Value.java", "public class Value {}\n")
        self.assertIn(f"$(B)/{PROJECT}/peer/peer.jar", self.node(self.graph(), "answer.run.cp.jar")["inputs"])

    def test_explicit_archive_name_accepts_a_prefix(self):
        self.write("ya.make", "JAVA_LIBRARY(answer PREFIX e2e-)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.node(self.graph(), "e2e-answer.jar")

    def test_default_test_archive_names_shorten_long_directory_prefixes(self):
        cases = (("p" * 50 + "/first", "first.jar"), ("q" * 110 + "/second", "second.jar"))
        self.write("ya.make", "RECURSE(" + " ".join(directory for directory, _ in cases) + ")\n")
        for directory, _ in cases:
            self.write(directory + "/ya.make", "JUNIT5()\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
            self.write(directory + "/Value.java", "public class Value {}\n")
        graph = self.graph("-t")
        for directory, filename in cases:
            self.node(graph, directory + "/" + filename)

    def test_unknown_jdk_version_is_a_configuration_error(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(999)\nUSE_ERROR_PRONE()\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.assertNotEqual(self.make().returncode, 0)

    def program(self, peer):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/{peer})\nEND()\n")
        self.write("Main.java", "public class Main {}\n")

    def test_missing_java_peer_directory_is_a_configuration_error(self):
        self.program("missing")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"{PROJECT}/missing", result.stderr)

    def test_proto_rejects_incompatible_java_test_peers(self):
        self.program("schema")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nPEERDIR({PROJECT}/peer)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
        self.write("peer/Value.java", "public class Value {}\n")
        for declaration in ("JUNIT5", "JAVA_TEST_LIBRARY"):
            with self.subTest(declaration=declaration):
                self.write("peer/ya.make", f"{declaration}(peer)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
                result = self.make()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f"{PROJECT}/peer", result.stderr)

    def test_excluded_java_schema_variants_cannot_be_used_as_peers(self):
        self.program("schema")
        for declaration, tag, source, content in (
            ("PROTO_LIBRARY", "JAVA_PROTO", "answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n'),
            ("FBS_LIBRARY", "JAVA_FBS", "answer.fbs", "namespace e2e;\ntable Answer { value:int; }\nroot_type Answer;\n"),
        ):
            with self.subTest(declaration=declaration):
                self.write("schema/ya.make", f"{declaration}()\nEXCLUDE_TAGS({tag})\nSRCS({source})\nEND()\n")
                self.write("schema/" + source, content)
                self.assertNotEqual(self.make().returncode, 0)

    def test_default_kotlin_layout_keeps_sources_and_resources(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nDEFAULT_JAVA_SRCS_LAYOUT()\nEND()\n")
        self.write("src/main/java/Answer.kt", "class Answer(val value: Int = 42)\n")
        self.write("src/main/resources/value.txt", "42\n")
        node = self.node(self.graph(), "answer.jar")
        self.assertIn(f"$(S)/{PROJECT}/src/main/java/Answer.kt", node["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/src/main/resources/value.txt", node["inputs"])

    def test_auto_service_adds_its_annotation_library(self):
        dependency = "contrib/java/com/google/auto/service/auto-service-annotations/1.0.1"
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nAUTO_SERVICE(1.0.1)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        inputs = self.node(self.graph(), "answer.jar")["inputs"]
        self.assertIn(f"$(B)/{dependency}/auto-service-annotations-1.0.1.jar", inputs)

    def test_disabling_default_scriptgen_keeps_custom_scripts(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nENABLE(DISABLE_SCRIPTGEN)\nJAVA_SRCS(Main.java)\nGENERATE_SCRIPT(OUT custom.sh TEMPLATE launch.tmpl)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("launch.tmpl", "#!/bin/sh\necho custom\n")
        graph = self.graph()
        self.node(graph, "answer.jar")
        self.node(graph, "custom.sh")
        self.assertFalse(any(f"$(B)/{PROJECT}/run.sh" in node["outputs"] for node in graph["graph"]))


class LongRootJavaNamesTest(MakeTest):
    project_name = "ay_e2e_" + "x" * 110

    def test_long_root_name_stays_unique_without_directory_separators(self):
        self.write("ya.make", "JUNIT5()\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
        self.write("Value.java", "public class Value {}\n")
        self.node(self.graph("-t"), self.project_name + ".jar")
