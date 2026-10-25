import unittest

from tst import lib


def tree(conf):
    files = {
        "build/conf/linkers/ld.conf": conf,
        "build/ymake_conf.py": lib.ymake_conf(extra="_DEFAULT_LINKER_ID=lld\n"),
        "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nSTRIP()\nEND()\n",
        "app/main.cpp": "int main() {return 0;}\n",
        "dll/ya.make": f"DLL(test)\n{lib.BARE_MODULE}SRCS(lib.cpp)\nSTRIP()\nEND()\n",
        "dll/lib.cpp": "int value;\n",
        "dynamic/ya.make": (
            f"DYNAMIC_LIBRARY(test)\n{lib.BARE_MODULE}"
            "DYNAMIC_LIBRARY_FROM(peer)\nEXPORTS_SCRIPT(test.exports)\nEND()\n"
        ),
        "dynamic/test.exports": "{};\n",
        "peer/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
        "build/platform/local_so/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
    }
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files


def link(graph, output):
    node = lib.node_by_output(graph, output)
    return next(cmd["cmd_args"] for cmd in node["cmds"]
                if any(arg.endswith(("link_exe.py", "link_dyn_lib.py"))
                       for arg in cmd["cmd_args"]))


class LinkerConfTest(unittest.TestCase):
    def test_revision_command_parts(self):
        replacements = {
            "-lpthread -lc": "-Wl,-lpthread,-lc",
            "-Wl,-no-pie": "-no-pie",
            "-Wl,--start-group": "-Wl,-(",
            "-Wl,--end-group": "-Wl,-)",
            "-Wl,--whole-archive": "-Wl,-whole-archive",
            "-Wl,--no-whole-archive": "-Wl,-no-whole-archive",
            "-Wl,--gc-sections": "-Wl,-gc-sections",
            "-Wl,--strip-all": "-Wl,-s",
            "-rdynamic": "-Wl,--export-dynamic",
            "-shared": "-Wl,-shared",
            "-soname": "--soname",
            "--source-root": "--source-directory",
            "--build-root": "--build-directory",
        }
        for newer in (False, True):
            conf = lib.FAKE_LD_CONF
            if newer:
                for old, new in replacements.items():
                    conf = conf.replace(old, new)
            for target, output in (
                ("app", "app/app"), ("dll", "dll/libtest.so"), ("dynamic", "dynamic/libtest.so"),
            ):
                with self.subTest(newer=newer, target=target):
                    args = link(lib.make(tree(conf), target, "-DPIC_NO_PIE=yes"), f"$(B)/{output}")
                    for old, new in replacements.items():
                        if old in ("-shared", "-soname") and target == "app":
                            continue
                        if old == "-Wl,--strip-all" and target == "dynamic":
                            continue
                        value = new if newer else old
                        if old == "-soname":
                            value = f"-Wl,{value},libtest.so"
                        for word in value.split():
                            self.assertIn(word, args)
                    self.assertIn("-nodefaultlibs", args)

    def test_conditions_and_late_variable_expansion(self):
        conf = lib.FAKE_LD_CONF.replace("-Wl,-no-pie", "-no-pie")
        for flags, wanted, absent in (
            ((), ["-nodefaultlibs", "-lpthread", "-no-pie"], ["-nostdlib"]),
            (("-DMUSL=yes",), ["-nostdlib", "-no-pie"], ["-nodefaultlibs", "-lpthread"]),
            (("-DGCOV_COVERAGE=yes",), ["-lpthread"], ["-nodefaultlibs"]),
            (("-DSANITIZER_TYPE=address",), ["-lpthread"], ["-nodefaultlibs"]),
            (("-DPIE=yes",), ["-lpthread"], ["-no-pie"]),
            (("-D_LINKER_ID=bfd",), ["-lpthread"], ["-no-pie"]),
            (("-DNO_LINKER_DCE=yes",), ["-lpthread"], ["-Wl,--gc-sections"]),
            (("-DLINKER_ICF=yes",), ["-Wl,--icf=safe"], []),
            (("-DC_SYSTEM_LIBRARIES=-lcustom",), ["-lcustom"], ["-lpthread"]),
        ):
            with self.subTest(flags=flags):
                args = link(lib.make(tree(conf), "app", *flags), "$(B)/app/app")
                for value in wanted:
                    self.assertIn(value, args)
                for value in absent:
                    self.assertNotIn(value, args)

    def test_host_pic_and_target_read_same_configuration(self):
        conf = lib.FAKE_LD_CONF.replace("-Wl,-no-pie", "-no-pie")
        files = tree(conf)
        files["app/ya.make"] = files["app/ya.make"].replace(
            "END()", "ADDINCL(${ARCADIA_BUILD_ROOT}/app)\nRUN_PROGRAM(tools/gen OUT generated.h)\nEND()")
        files["app/main.cpp"] = '#include "generated.h"\n' + files["app/main.cpp"]
        lib.tool_program(files, "tools/gen", "gen")
        for flags, expected in (((), False), (("--host-platform-flag", "PIC_NO_PIE=yes"), True)):
            with self.subTest(flags=flags):
                graph = lib.make(files, "app", *flags)
                target = lib.node_by_output(graph, "$(B)/app/main.cpp.o")
                self.assertCountEqual(["$(S)/app/main.cpp", "$(B)/app/generated.h"], target["inputs"])
                producer = lib.node_by_output(graph, "$(B)/app/generated.h")
                self.assertIn(producer["uid"], target["deps"])
                self.assertIn(lib.node_by_output(graph, "$(B)/tools/gen/gen")["uid"], producer["deps"])
                self.assertIn("-no-pie", link(graph, "$(B)/app/app"))
                self.assertEqual("-no-pie" in link(graph, "$(B)/tools/gen/gen"), expected)

    def test_os_selection_and_continued_assignments(self):
        conf = lib.FAKE_LD_CONF + r'''
when ($OS_DARWIN == "yes") {
    _C_SYSTEM_LIBRARIES=\
        -lSystem \
        -lresolv
}
macro UNUSED() {
    when ($UNUSED in [ "unsupported", "expression" ]) {
        _C_SYSTEM_LIBRARIES=-lwrong
    }
}
when ($UNUSED in [ "unsupported", "expression" ]) {
    UNUSED=-ignored
}
'''
        args = link(lib.make(tree(conf), "app", "--target-platform", "default-darwin-arm64"),
                    "$(B)/app/app")
        self.assertIn("-lSystem", args)
        self.assertIn("-lresolv", args)
        self.assertIn("-Wl,-dead_strip", args)
        self.assertNotIn("-lpthread", args)
        self.assertNotIn("-lwrong", args)
        self.assertNotIn("-Wl,--start-group", args)


if __name__ == "__main__":
    unittest.main(verbosity=2)
