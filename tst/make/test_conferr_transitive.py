from tst.make.lib import MakeTest


class TransitiveConfigureErrorsTest(MakeTest):
    """Configure errors of transitive requirement checks (devtools/ymake/transitive_requirements_check.cpp):
    CHECK_DEPENDENT_DIRS, REQUIRES, PROVIDES, LICENSE restrictions and FEATURE_VERSIONS."""

    def program(self, *statements):
        self.write("ya.make", "PROGRAM()\nSRCS(main.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("main.cpp", "int main() { return 0; }\n")

    def library(self, path, *statements):
        self.write(f"{path}/ya.make", "LIBRARY()\nSRCS(value.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write(f"{path}/value.cpp", "int value() { return 42; }\n")

    def peer(self, path):
        return f"PEERDIR({self.project_name}/{path})"

    def test_dependent_dirs_with_an_unknown_type(self):
        self.program(f"CHECK_DEPENDENT_DIRS(E2E {self.project_name}/lib)")
        self.assertConfigureLikeYa()

    def test_dependent_dirs_with_different_types(self):
        self.program(f"CHECK_DEPENDENT_DIRS(DENY {self.project_name}/lib)", f"CHECK_DEPENDENT_DIRS(ALLOW_ONLY {self.project_name}/other)")
        self.assertConfigureLikeYa()

    def test_dependent_dirs_with_an_exception_in_allow_only(self):
        self.program(f"CHECK_DEPENDENT_DIRS(ALLOW_ONLY EXCEPT {self.project_name}/lib)")
        self.assertConfigureLikeYa()

    def test_dependent_dirs_denying_a_peer(self):
        self.program(self.peer("lib"), f"CHECK_DEPENDENT_DIRS(DENY {self.project_name}/lib)")
        self.library("lib")
        self.assertConfigureLikeYa()

    def test_dependent_dirs_denying_a_transitive_peer(self):
        self.program(self.peer("lib"), f"CHECK_DEPENDENT_DIRS(DENY PEERDIRS {self.project_name}/deep)")
        self.library("lib", self.peer("deep"))
        self.library("deep")
        self.assertConfigureLikeYa()

    def test_dependent_dirs_allowing_only_another_directory(self):
        self.program(self.peer("lib"), f"CHECK_DEPENDENT_DIRS(ALLOW_ONLY PEERDIRS {self.project_name}/other build contrib library util)")
        self.library("lib")
        self.assertConfigureLikeYa()

    def test_dependent_dirs_denying_a_tool(self):
        self.program(f"RUN_PROGRAM({self.project_name}/tool STDOUT out.cpp)", f"CHECK_DEPENDENT_DIRS(DENY {self.project_name}/tool)")
        self.write("tool/ya.make", "PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.assertConfigureLikeYa()

    def test_dependent_dirs_denying_a_peer_of_a_tool(self):
        self.program(f"RUN_PROGRAM({self.project_name}/tool STDOUT out.cpp)", f"CHECK_DEPENDENT_DIRS(DENY {self.project_name}/lib)")
        self.write("tool/ya.make", f"PROGRAM()\nSRCS(main.cpp)\n{self.peer('lib')}\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.library("lib")
        self.assertConfigureLikeYa()

    def test_requires_a_missing_directory(self):
        self.program(f"REQUIRES({self.project_name}/missing)")
        self.assertConfigureLikeYa()

    def test_requires_a_directory_without_a_peerdir(self):
        self.program(f"REQUIRES({self.project_name}/lib)")
        self.library("lib")
        self.assertConfigureLikeYa()

    def test_provides_by_two_peers(self):
        self.program(self.peer("one"), self.peer("two"))
        self.library("one", "PROVIDES(e2e)")
        self.library("two", "PROVIDES(e2e)")
        self.assertConfigureLikeYa()

    def test_provides_by_the_module_and_its_peer(self):
        self.program(self.peer("lib"), "PROVIDES(e2e)")
        self.library("lib", "PROVIDES(e2e)")
        self.assertConfigureLikeYa()

    def test_license_restriction_with_an_unknown_type(self):
        self.program("LICENSE_RESTRICTION(E2E SERVICE)")
        self.assertConfigureLikeYa()

    def test_license_restriction_with_different_types(self):
        self.program("LICENSE_RESTRICTION(DENY FORBIDDEN)", "LICENSE_RESTRICTION(ALLOW_ONLY SERVICE)")
        self.assertConfigureLikeYa()

    def test_license_restriction_with_an_unknown_property(self):
        self.program("LICENSE_RESTRICTION(DENY E2E)")
        self.assertConfigureLikeYa()

    def test_license_restriction_denying_a_peer_license(self):
        self.program(self.peer("lib"), "LICENSE_RESTRICTION(DENY FORBIDDEN)")
        self.library("lib", "LICENSE(AGPL-3.0-only)")
        self.assertConfigureLikeYa()

    def test_modulewise_license_restriction(self):
        self.library(".", "LICENSE(AGPL-3.0-only)", "MODULEWISE_LICENSE_RESTRICTION(DENY FORBIDDEN)")
        self.assertConfigureLikeYa()

    def test_license_with_an_invalid_expression(self):
        self.library(".", "LICENSE(MIT AND)")
        self.assertConfigureLikeYa()

    def test_license_with_an_unknown_name(self):
        self.library(".", "LICENSE(MIT WITH)")
        self.assertConfigureLikeYa()

    def test_explicit_license_prefix_without_a_license(self):
        self.library(".", f"SET(EXPLICIT_LICENSE_PREFIXES {self.project_name})")
        self.assertConfigureLikeYa()

    def test_feature_version_conflicts_without_versions(self):
        self.program("SET_APPEND(CHECK_FEATURE_VERSION_CONFLICTS yes)")
        self.assertConfigureLikeYa()

    def test_feature_versions_without_a_name(self):
        self.program("SET_APPEND(CHECK_FEATURE_VERSION_CONFLICTS yes)", "SET_APPEND(FEATURE_VERSIONS =1)")
        self.assertConfigureLikeYa()

    def test_feature_versions_conflicting_with_a_peer(self):
        self.program(self.peer("lib"), "SET_APPEND(CHECK_FEATURE_VERSION_CONFLICTS yes)", "SET_APPEND(FEATURE_VERSIONS e2e=1)")
        self.library("lib", "SET_APPEND(FEATURE_VERSIONS e2e=2)")
        self.assertConfigureLikeYa()

    def test_go_test_depending_on_the_tested_module_through_a_peer(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(value.go)\nEND()\nRECURSE_FOR_TESTS(ut)\n")
        self.write("value.go", "package value\n\nfunc Value() int { return 42 }\n")
        self.write("ut/ya.make", f"GO_TEST_FOR({self.project_name})\nGO_TEST_SRCS(value_test.go)\n{self.peer('other')}\nEND()\n")
        self.write("value_test.go", "package value\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {}\n")
        self.write("other/ya.make", f"GO_LIBRARY()\nSRCS(other.go)\n{self.peer('')}\nEND()\n".replace(f"{self.project_name}/)", f"{self.project_name})"))
        self.write("other/other.go", "package other\n")
        self.assertConfigureLikeYa(targets=(f"{self.project_name}/ut",))
