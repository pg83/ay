from tst.make.lib import MakeTest


class JavaConfigureErrorsTest(MakeTest):
    """Configure errors of the Java build plugin (build/plugins/java.py)."""

    def java_library(self, *statements, jdk="17"):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION({jdk})\nJAVA_SRCS(Answer.java)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("Answer.java", "package e2e; public class Answer { public static int answer() { return 42; } }\n")

    def java_program(self, *statements, jdk="17"):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION({jdk})\nJAVA_SRCS(Main.java)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("Main.java", "package e2e; public class Main { public static void main(String[] args) {} }\n")

    def test_invalid_check_java_deps(self):
        self.java_library("CHECK_JAVA_DEPS(e2e)")
        self.assertConfigureLikeYa()

    def test_invalid_make_uberjar_in_a_program(self):
        self.java_program("SET(MAKE_UBERJAR_VALUE e2e)")
        self.assertConfigureLikeYa()

    def test_invalid_make_uberjar_in_an_annotation_processor(self):
        self.write("ya.make", "JAVA_ANNOTATION_PROCESSOR(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nSET(MAKE_UBERJAR_VALUE e2e)\nEND()\n")
        self.write("Main.java", "package e2e; public class Main {}\n")
        self.assertConfigureLikeYa()

    def test_multiple_classpath_elements_in_run_java_program(self):
        self.java_library(f"RUN_JAVA_PROGRAM(e2e.Generator CLASSPATH {self.project_name}/tool {self.project_name}/other OUT Generated.java)")
        for tool in ("tool", "other"):
            self.write(f"{tool}/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
            self.write(f"{tool}/Generator.java", "package e2e; public class Generator { public static void main(String[] args) {} }\n")
        self.assertConfigureLikeYa()

    def test_script_with_more_templates_than_outputs(self):
        self.java_program("GENERATE_SCRIPT(OUT answer.sh TEMPLATE first.tmpl second.tmpl)")
        self.write("first.tmpl", "first\n")
        self.write("second.tmpl", "second\n")
        self.assertConfigureLikeYa()

    def test_script_with_an_empty_custom_property(self):
        self.java_program("GENERATE_SCRIPT(OUT answer.sh TEMPLATE launch.tmpl CUSTOM_PROPERTY CUSTOM_PROPERTY answer 42)")
        self.write("launch.tmpl", "launch\n")
        self.assertConfigureLikeYa()

    def test_script_with_a_custom_property_without_a_value(self):
        self.java_program("GENERATE_SCRIPT(OUT answer.sh TEMPLATE launch.tmpl CUSTOM_PROPERTY answer)")
        self.write("launch.tmpl", "launch\n")
        self.assertConfigureLikeYa()

    def test_unknown_jdk_version(self):
        self.java_library(jdk="12")
        self.assertConfigureLikeYa()

    def test_runnable_jar_on_a_new_jdk_without_with_jdk(self):
        self.java_program(jdk="21")
        self.assertConfigureLikeYa()

    def test_java_resource_tar_without_an_extract_root(self):
        self.java_library("JAVA_RESOURCE_TAR(data.tar)")
        self.write("data.tar", "data\n")
        self.assertConfigureLikeYa()

    def test_javac_flags_with_full_processing(self):
        self.java_library("JAVAC_FLAGS(-proc:full)")
        self.assertConfigureLikeYa()

    def test_javac_flags_with_processing_only(self):
        self.java_library("JAVAC_FLAGS(-proc:only)")
        self.assertConfigureLikeYa()

    def test_kotlin_abi_jar_without_kotlin(self):
        self.java_library("ENABLE_KOTLIN_ABI_JAR()")
        self.assertConfigureLikeYa()

    def test_kotlin_abi_jar_with_a_non_numeric_kotlin_version(self):
        self.java_library("WITH_KOTLIN()", "SET(_KOTLIN_VERSION e2e)", "ENABLE_KOTLIN_ABI_JAR()")
        self.assertConfigureLikeYa()

    def test_kotlin_abi_jar_with_an_old_kotlin(self):
        self.java_library("WITH_KOTLIN()", "SET(_KOTLIN_VERSION 2.0.0)", "ENABLE_KOTLIN_ABI_JAR()")
        self.assertConfigureLikeYa()

    def test_relative_external_dependency(self):
        self.java_program("JAVA_EXTERNAL_DEPENDENCIES(../outside.jar)")
        self.assertConfigureLikeYa()

    def test_absolute_external_dependency(self):
        self.java_program("JAVA_EXTERNAL_DEPENDENCIES(/outside.jar)")
        self.assertConfigureLikeYa()
