import unittest

from tst import lib


DEFAULT_CONFIG = '''
DEFAULT_ALLOCATOR=SYSTEM
when ($MUSL == "yes") {
    DEFAULT_ALLOCATOR=TCMALLOC_TC
}
when ($OS_ANDROID == "yes" || $OS_WINDOWS == "yes" || $ARCH_TYPE_32 == "yes") {
    DEFAULT_ALLOCATOR=J
}
when ($OS_LINUX == "yes") {
    when ($GCC) {
    }
    elsewhen ($ARCH_X86_64) {
        DEFAULT_ALLOCATOR=TCMALLOC_TC
    }
}
when ($SANITIZER_TYPE) {
    when ($SANITIZER_TYPE != "no") {
        DEFAULT_ALLOCATOR=SYSTEM
    }
}
when ($ARCH_XTENSA == "yes") {
    DEFAULT_ALLOCATOR=FAKE
}
when ($OS_EMSCRIPTEN == "yes") {
    DEFAULT_ALLOCATOR=FAKE
}
'''


def program(body="", prelude="", name="maple"):
    return (
        prelude + f"PROGRAM({name})\nDISABLE(COMMON_LINK_SETTINGS)\nNO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\n" + body
        + 'IF(ALLOCATOR == "TCMALLOC_TC")\nPEERDIR(glade/stats/cobalt)\nCFLAGS(-DSEEN_COBALT)\n'
        'ELSE()\nPEERDIR(glade/stats/linen)\nCFLAGS(-DSEEN_LINEN)\nENDIF()\n'
        'ALLOCATOR(FAKE)\nSRCS(main.cpp)\nEND()\n'
    )


def workspace(body="", prelude="", config=DEFAULT_CONFIG):
    files = {
        "build/ymake.core.conf": lib.CONF_FILES["build/ymake.core.conf"] + config,
        "glade/app/ya.make": program(body, prelude),
        "glade/app/main.cpp": "int main(){return 0;}\n",
    }
    for color in ("cobalt", "linen"):
        root = "glade/stats/" + color
        files[root + "/ya.make"] = "LIBRARY()\nNO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\nSRCS(local.cpp)\nGLOBAL_SRCS(shared.cpp)\nEND()\n"
        files[root + "/local.cpp"] = "int " + color + ";\n"
        files[root + "/shared.cpp"] = "int global_" + color + ";\n"
    return files


def selected_graph(files, *args, platform="default-linux-x86_64", targets=("glade/app",)):
    return lib.make(files, *targets, "--target-platform", platform, *args)


class AllocatorConditionContextTest(unittest.TestCase):
    def check(self, files, expected, *args, platform="default-linux-x86_64"):
        graph = selected_graph(files, *args, platform=platform)
        selected = [n for n in graph["graph"] if any("$(B)/glade/stats/" in o for o in n["outputs"])]
        self.assertTrue(selected)
        self.assertTrue(all("/" + expected + "/" in o for n in selected for o in n["outputs"]))
        compiled = lib.node_by_output(graph, "$(B)/glade/app/main.cpp.o")
        self.assertIn("-DSEEN_" + expected.upper(), compiled["cmds"][0]["cmd_args"])
        link = lib.node_by_output(graph, "$(B)/glade/app/maple")
        for archive in (n for n in selected if n["outputs"][0].endswith(".a")):
            self.assertIn(archive["uid"], link["deps"])
            self.assertIn(archive["outputs"][0], link["inputs"])
        self.assertIn(link["uid"], graph["result"])
        return graph

    def test_program_default_is_visible_to_if_and_preserves_platform_context(self):
        self.check(workspace(), "cobalt")
        self.check(workspace(), "linen", platform="default-linux-aarch64")
        self.check(workspace(), "cobalt", "-DUA_PUBLIC=yes")
        files = workspace()
        files["glade/app/ya.make"] = files["glade/app/ya.make"].replace("PROGRAM(maple)", "UNITTEST_WITH_CUSTOM_ENTRY_POINT(maple)")
        self.check(files, "cobalt")
        files = workspace()
        for path in list(files):
            if path.startswith("glade/stats/linen/"):
                del files[path]
        self.check(files, "cobalt")

    def test_explicit_allocator_and_existing_binding_are_not_overwritten(self):
        for body, prelude, flags, color in (
            ("ALLOCATOR(TCMALLOC_TC)\n", "", (), "cobalt"),
            ("ALLOCATOR(SYSTEM)\n", "", (), "linen"),
            ("ALLOCATOR(FAKE)\n", "", (), "linen"),
            ("", "SET(ALLOCATOR FAKE)\n", (), "linen"),
            ("", "SET(DEFAULT_ALLOCATOR FAKE)\n", (), "linen"),
            ("", "SET(ALLOCATOR)\n", (), "linen"),
            ("SET(DEFAULT_ALLOCATOR FAKE)\n", "", (), "cobalt"),
            ("", "", ("-DALLOCATOR=FAKE",), "linen"),
            ("", "", ("-DDEFAULT_ALLOCATOR=FAKE",), "cobalt"),
        ):
            with self.subTest(body=body, prelude=prelude, flags=flags):
                self.check(workspace(body, prelude), color, *flags)

    def test_default_value_follows_source_config_instead_of_a_platform_table(self):
        config = DEFAULT_CONFIG.replace("DEFAULT_ALLOCATOR=TCMALLOC_TC", "DEFAULT_ALLOCATOR=FAKE")
        self.check(workspace(config=config), "linen")
        config = DEFAULT_CONFIG.replace("DEFAULT_ALLOCATOR=TCMALLOC_TC", "DEFAULT_ALLOCATOR=$RENAMED_CHOICE")
        self.check(workspace(config="RENAMED_CHOICE=FAKE\n" + config), "linen")

    def test_module_bindings_do_not_leak_between_roots_in_either_order(self):
        files = workspace()
        files["glade/other/ya.make"] = program("ALLOCATOR(FAKE)\n", name="birch")
        files["glade/other/main.cpp"] = "int main(){return 0;}\n"
        for targets in (("glade/app", "glade/other"), ("glade/other", "glade/app")):
            graph = selected_graph(files, targets=targets)
            for path, color in (("glade/app", "COBALT"), ("glade/other", "LINEN")):
                node = next(n for n in graph["graph"] if n["kv"].get("p") == "CC" and "$(S)/" + path + "/main.cpp" in n["inputs"])
                self.assertIn("-DSEEN_" + color, node["cmds"][0]["cmd_args"])

    def test_ordinary_library_does_not_receive_program_default_binding(self):
        files = workspace()
        files["glade/app/ya.make"] = (
            "LIBRARY(maple)\nNO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\n"
            'IF(DEFINED ALLOCATOR)\nCFLAGS(-DWRONG)\nELSE()\nCFLAGS(-DUNBOUND)\nENDIF()\n'
            "SRCS(main.cpp)\nEND()\n"
        )
        graph = selected_graph(files)
        node = lib.node_by_output(graph, "$(B)/glade/app/main.cpp.o")
        self.assertIn("-DUNBOUND", node["cmds"][0]["cmd_args"])
        self.assertNotIn("-DWRONG", node["cmds"][0]["cmd_args"])

    def test_host_tool_and_target_keep_their_own_allocator_context(self):
        files = workspace("ADDINCL(${ARCADIA_BUILD_ROOT}/glade/app)\nRUN_PROGRAM(glade/tool OUT_NOAUTO gen.h)\n")
        files["glade/app/main.cpp"] = '#include "gen.h"\nint main(){return 0;}\n'
        files["glade/tool/ya.make"] = program(name="cedar")
        files["glade/tool/main.cpp"] = "int main(){return 0;}\n"
        graph = selected_graph(files, platform="default-linux-aarch64")
        for path, color in (("glade/app", "LINEN"), ("glade/tool", "COBALT")):
            node = next(n for n in graph["graph"] if n["kv"].get("p") == "CC" and "$(S)/" + path + "/main.cpp" in n["inputs"])
            self.assertIn("-DSEEN_" + color, node["cmds"][0]["cmd_args"])
        generated = lib.node_by_output(graph, "$(B)/glade/app/gen.h")
        tool = lib.node_by_output(graph, "$(B)/glade/tool/cedar")
        owner = lib.node_by_output(graph, "$(B)/glade/app/main.cpp.o")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(generated["uid"], owner["deps"])


if __name__ == "__main__":
    unittest.main()
