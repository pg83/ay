from tst.make.lib import MakeTest

GUAVA = "contrib/java/com/google/guava/guava"
GSON = "contrib/java/com/google/code/gson/gson"


class JavaDependencyManagementConfigureErrorsTest(MakeTest):
    """Configure errors of Java dependency management (devtools/ymake/dependency_management.cpp)."""

    def java_library(self, *statements):
        self.write("ya.make", "JAVA_LIBRARY()\nJDK_VERSION(17)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("src/main/java/ru/e2e/Value.java", "package ru.e2e;\npublic class Value {}\n")

    def test_dependency_management_outside_of_manageable_paths(self):
        self.java_library(f"DEPENDENCY_MANAGEMENT({self.project_name}/e2e)")
        self.assertConfigureLikeYa()

    def test_unknown_java_dependencies_configuration(self):
        self.java_library("JAVA_DEPENDENCIES_CONFIGURATION(E2E)")
        self.assertConfigureLikeYa()

    def test_peerdir_to_a_direct_version(self):
        self.java_library("JAVA_DEPENDENCIES_CONFIGURATION(FORBID_DIRECT_PEERDIRS)", f"PEERDIR({GUAVA}/33.7.1-jre)")
        self.assertConfigureLikeYa()

    def test_peerdir_to_a_default_version(self):
        self.java_library(f"PEERDIR({GUAVA})")
        self.assertConfigureLikeYa()

    def test_dependency_management_to_a_missing_version(self):
        self.java_library(f"DEPENDENCY_MANAGEMENT({GUAVA}/0.0.0-e2e)", f"PEERDIR({GUAVA})")
        self.assertConfigureLikeYa()

    def test_dependency_version_without_dependency_management(self):
        self.java_library("JAVA_DEPENDENCIES_CONFIGURATION(REQUIRE_DM)", f"PEERDIR({GUAVA}/33.7.1-jre)")
        self.assertConfigureLikeYa()

    def test_auto_resolved_version_conflict(self):
        self.java_library("JAVA_DEPENDENCIES_CONFIGURATION(FORBID_CONFLICT)", f"PEERDIR({self.project_name}/one {self.project_name}/two)")
        self.write("one/ya.make", f"JAVA_LIBRARY()\nJDK_VERSION(17)\nPEERDIR({GUAVA}/33.7.1-jre)\nEND()\n")
        self.write("two/ya.make", f"JAVA_LIBRARY()\nJDK_VERSION(17)\nPEERDIR({GUAVA}/32.1.3-jre)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_dependency_management_version_conflict(self):
        self.java_library("JAVA_DEPENDENCIES_CONFIGURATION(FORBID_CONFLICT_DM)", f"DEPENDENCY_MANAGEMENT({GUAVA}/33.7.1-jre)", f"PEERDIR({self.project_name}/one)")
        self.write("one/ya.make", f"JAVA_LIBRARY()\nJDK_VERSION(17)\nPEERDIR({GUAVA}/32.1.3-jre)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_dependency_management_older_than_a_peer(self):
        self.java_library("JAVA_DEPENDENCIES_CONFIGURATION(FORBID_CONFLICT_DM_RECENT)", f"DEPENDENCY_MANAGEMENT({GUAVA}/32.1.3-jre)", f"PEERDIR({self.project_name}/one)")
        self.write("one/ya.make", f"JAVA_LIBRARY()\nJDK_VERSION(17)\nPEERDIR({GUAVA}/33.7.1-jre)\nEND()\n")
        self.assertConfigureLikeYa()
