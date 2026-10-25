import unittest

from tst import lib


PERL_INC = """IF (PERL_INCLUDE)
    CFLAGS(GLOBAL $PERL_INCLUDE)
ENDIF()
CFLAGS(GLOBAL -I$PERL_ARCHLIB/CORE)
IF (PERL_LIBS)
    LDFLAGS(-L${PERL_LIBS})
ENDIF()
LDFLAGS(-lperl)
CFLAGS(GLOBAL -DUSE_PERL)
"""


def sdk_tree(body):
    return {
        "sdk/ya.make": "RESOURCES_LIBRARY()\n" + body + "\nINCLUDE(perl.inc)\nEND()\n",
        "sdk/perl.inc": PERL_INC,
        "app/ya.make": f"PROGRAM(app)\n{lib.BARE_MODULE}PEERDIR(sdk)\nSRCS(a.cpp)\nEND()\n",
        "app/a.cpp": "",
    }


class PerlSdkTest(unittest.TestCase):
    def test_system_sdk_layout_and_global_flags(self):
        for sdk, version, prefix, arch in (
            ("ubuntu-14", "5.18", "perl5.18", ""),
            ("ubuntu-16", "5.22", "perl5.22", "/x86_64-linux-gnu"),
            ("ubuntu-24", "5.38", "usr", "/x86_64-linux-gnu"),
        ):
            resource = "SYSTEM_PERL_" + version.replace(".", "")
            for set_sdk in (True, False):
                with self.subTest(sdk=sdk, set_sdk=set_sdk):
                    body = f"DECLARE_EXTERNAL_RESOURCE({resource} sbr:1)\n"
                    if set_sdk:
                        body += f"SET(PERL_SDK {sdk})\n"
                    files = sdk_tree(body)
                    graph = lib.make(files, "app", "-DPERL_SDK=" + ("ubuntu-12" if set_sdk else sdk))
                    compiled = lib.node_by_output(graph, "$(B)/app/a.cpp.o")["cmds"][0]["cmd_args"]
                    root = f"$(B)/resources/{resource}/{prefix}"
                    archlib = f"{root}/lib{arch}/perl/{version}"
                    self.assertIn("-I" + archlib, compiled)
                    self.assertIn("-I" + archlib + "/CORE", compiled)
                    self.assertIn("-DUSE_PERL", compiled)
                    self.assertNotIn(f"-I{root}/share/perl/{version}", compiled)
                    linked = lib.node_by_output(graph, "$(B)/app/app")
                    flags = next(c["cmd_args"] for c in linked["cmds"] if "$(S)/build/scripts/link_exe.py" in c["cmd_args"])
                    self.assertIn(f"-L{root}/lib{arch}", flags)
                    self.assertIn("-lperl", flags)

    def test_local_sdk_uses_local_paths_with_system_perl_disabled(self):
        for configure in ("flags", "set", "disable"):
            with self.subTest(configure=configure):
                body = ""
                flags = [
                    "-DUSE_LOCAL_TOOLS=yes", "-DLOCAL_PERL_ARCHLIB=/local/arch",
                    "-DLOCAL_PERL_PRIVLIB=/local/share", "-DLOCAL_PERL_LIBS=/local/lib",
                ]
                if configure == "flags":
                    flags.append("-DUSE_PERL_SYSTEM=no")
                elif configure == "set":
                    body = "SET(USE_PERL_SYSTEM no)\n"
                else:
                    body = "DISABLE(USE_PERL_SYSTEM)\n"
                graph = lib.make(sdk_tree(body), "app", *flags)
                compiled = lib.node_by_output(graph, "$(B)/app/a.cpp.o")["cmds"][0]["cmd_args"]
                self.assertIn("-I/local/arch/CORE", compiled)
                self.assertNotIn("-I/local/share", compiled)
                linked = lib.node_by_output(graph, "$(B)/app/app")
                flags = next(c["cmd_args"] for c in linked["cmds"] if "$(S)/build/scripts/link_exe.py" in c["cmd_args"])
                self.assertIn("-L/local/lib", flags)
                self.assertIn("-lperl", flags)

    def test_default_and_enable_recalculate_the_sdk_values(self):
        for body, flags in (
            ("DEFAULT(PERL_SDK ubuntu-16)\n", []),
            ("ENABLE(USE_PERL_SYSTEM)\n", ["-DPERL_SDK=ubuntu-16", "-DUSE_PERL_SYSTEM=no"]),
        ):
            with self.subTest(body=body):
                files = sdk_tree(body + "DECLARE_EXTERNAL_RESOURCE(SYSTEM_PERL_522 sbr:1)\n")
                graph = lib.make(files, "app", *flags)
                args = lib.node_by_output(graph, "$(B)/app/a.cpp.o")["cmds"][0]["cmd_args"]
                self.assertIn("-I$(B)/resources/SYSTEM_PERL_522/perl5.22/lib/x86_64-linux-gnu/perl/5.22/CORE", args)

    def test_recalculation_restores_module_assignments_or_the_global_configuration(self):
        for assignment, expected in (
            ("", "-L$(B)/resources/SYSTEM_PERL_522/perl9.99/lib/x86_64-linux-gnu"),
            ("SET(PERL_LIBS /module/lib)\n", "-L/module/lib"),
        ):
            with self.subTest(assignment=assignment):
                files = sdk_tree("DECLARE_EXTERNAL_RESOURCE(SYSTEM_PERL_522 sbr:1)\n" + assignment + "DISABLE(USE_PERL_SYSTEM)\n")
                graph = lib.make(files, "app", "-DUSE_LOCAL_TOOLS=yes", "-DPERL_SDK=ubuntu-16",
                                 "-DLOCAL_PERL_ARCHLIB=/local/arch", "-DLOCAL_PERL_VERSION=9.99", "-DPERL_LIBS=/original/lib")
                linked = lib.node_by_output(graph, "$(B)/app/app")
                flags = next(c["cmd_args"] for c in linked["cmds"] if "$(S)/build/scripts/link_exe.py" in c["cmd_args"])
                self.assertIn(expected, flags)
                self.assertNotIn("-L/original/lib", flags)

    def test_sdk_change_recalculates_only_its_dependent_assignments(self):
        files = sdk_tree(
            "SET(PERL_SDK ubuntu-16)\nSET(PERL_ARCHLIB /custom/arch)\n"
            "SET(PERL_SDK ubuntu-24)\nDECLARE_EXTERNAL_RESOURCE(SYSTEM_PERL_538 sbr:1)\n")
        graph = lib.make(files, "app")
        args = lib.node_by_output(graph, "$(B)/app/a.cpp.o")["cmds"][0]["cmd_args"]
        self.assertIn("-I/custom/arch", args)
        self.assertIn("-I/custom/arch/CORE", args)

    def test_an_applied_default_survives_recalculation(self):
        files = sdk_tree("DEFAULT(PERL_LIBS /default/lib)\nENABLE(USE_LOCAL_TOOLS)\n")
        graph = lib.make(files, "app", "-DUSE_PERL_SYSTEM=no", "-DLOCAL_PERL_ARCHLIB=/local/arch")
        linked = lib.node_by_output(graph, "$(B)/app/app")
        flags = next(c["cmd_args"] for c in linked["cmds"] if "$(S)/build/scripts/link_exe.py" in c["cmd_args"])
        self.assertIn("-L/default/lib", flags)

    def test_sdk_conditions_expand_scalar_aliases(self):
        files = sdk_tree("DECLARE_EXTERNAL_RESOURCE(SYSTEM_PERL_522 sbr:1)\n")
        graph = lib.make(files, "app", "-DPERL_SDK=$SDK_ALIAS", "-DSDK_ALIAS=ubuntu-16",
                         "-DUSE_PERL_SYSTEM=$SYSTEM_ALIAS", "-DSYSTEM_ALIAS=yes")
        args = lib.node_by_output(graph, "$(B)/app/a.cpp.o")["cmds"][0]["cmd_args"]
        self.assertIn("-I$(B)/resources/SYSTEM_PERL_522/perl5.22/lib/x86_64-linux-gnu/perl/5.22/CORE", args)


if __name__ == "__main__":
    unittest.main()
