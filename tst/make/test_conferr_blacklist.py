from tst.make.lib import MakeTest, PROJECT


class BlacklistConfigureErrorsTest(MakeTest):
    """Configure errors of the Autocheck blacklist (devtools/ymake/blacklist_checker.cpp, blacklist.cpp)."""

    def blacklist(self, *rules):
        path = "build/rules/autocheck.blacklist"
        self.write_tree(path, (self.root / path).read_text() + "".join(rule + "\n" for rule in rules))

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\nSRCS(value.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("value.cpp", "int value() { return 42; }\n")

    def banned_library(self, path="banned"):
        self.write(f"{path}/ya.make", "LIBRARY()\nSRCS(banned.cpp)\nEND()\n")
        self.write(f"{path}/banned.cpp", "int banned() { return 42; }\n")
        self.write(f"{path}/banned.h", "#pragma once\n")

    def test_peerdir_to_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.library(f"PEERDIR({PROJECT}/banned)")
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_recurse_into_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.library()
        self.write("ya.make", (self.project / "ya.make").read_text() + "RECURSE(banned)\n")
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_recurse_for_tests_into_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.library()
        self.write("ya.make", (self.project / "ya.make").read_text() + "RECURSE_FOR_TESTS(banned)\n")
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_depends_on_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.write("ya.make", f"UNITTEST()\nSRCS(test.cpp)\nDEPENDS({PROJECT}/banned)\nEND()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")
        self.write("banned/ya.make", "PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.write("banned/main.cpp", "int main() { return 0; }\n")
        self.assertConfigureLikeYa()

    def test_srcdir_in_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.library(f"SRCDIR({PROJECT}/banned)")
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_addincl_of_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.library(f"ADDINCL(GLOBAL {PROJECT}/banned)")
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_data_from_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.write("ya.make", f"UNITTEST()\nSRCS(test.cpp)\nDATA(arcadia/{PROJECT}/banned)\nEND()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_source_from_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.library("SRCS(banned/banned.cpp)")
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_include_from_a_blacklisted_directory(self):
        self.blacklist(f"{PROJECT}/banned")
        self.library()
        self.write("value.cpp", f'#include <{PROJECT}/banned/banned.h>\nint value() {{ return 42; }}\n')
        self.banned_library()
        self.assertConfigureLikeYa()

    def test_target_in_a_blacklisted_directory(self):
        self.blacklist(PROJECT)
        self.library()
        self.assertConfigureLikeYa()

    def test_peerdir_to_an_allowed_subdirectory(self):
        self.blacklist(f"{PROJECT}/banned", f"!{PROJECT}/banned/allowed")
        self.library(f"PEERDIR({PROJECT}/banned/allowed)")
        self.banned_library("banned/allowed")
        self.assertConfigureLikeYa()

    def test_conflicting_rules(self):
        self.blacklist(f"{PROJECT}/banned", f"!{PROJECT}/banned")
        self.library(f"PEERDIR({PROJECT}/banned)")
        self.banned_library()
        self.assertConfigureLikeYa()
