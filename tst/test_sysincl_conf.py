import unittest

import lib


class SysinclConfTest(unittest.TestCase):
    def files(self, config):
        return {
            "m/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.cpp)\nEND()\n",
            "m/a.cpp": "#include <selected.h>\n",
            "build/conf/sysincl.conf": config,
            "build/ymake.core.conf": '''@import "${CONF_ROOT}/conf/sysincl.conf"
when ($NORUNTIME != "yes") {
    SYSINCL+=build/sysincl/core.yml
}
macro UNUSED() {
    SYSINCL+=build/sysincl/unused.yml
}
''',
            "build/sysincl/core.yml": "- includes:\n  - core.h\n",
            "build/sysincl/unused.yml": "this is not sysincl YAML\n",
            "build/sysincl/misc.yml": "this is not sysincl YAML\n",
            "build/custom/first.yml": "- includes:\n  - selected.h: m/first.h\n",
            "build/custom/second.yml": "- includes:\n  - selected.h: m/second.h\n",
            "m/first.h": "#include <core.h>\n",
            "m/second.h": "#include <core.h>\n",
        }

    def test_source_tree_controls_the_list(self):
        variants = (
            "SYSINCL = \\\n    build/custom/first.yml \\\n\n",
            '''SYSINCL=build/sysincl/misc.yml
SYSINCL=
when ($OS_LINUX == "yes" && !$MUSL) {
    when ($ARCH_AARCH64 == "yes") {
        SYSINCL += build/custom/second.yml
    } otherwise {
        SYSINCL += build/custom/first.yml
    }
} elsewhen ($MUSL == "yes") {
    SYSINCL+=build/custom/first.yml
} otherwise {
    SYSINCL+=build/sysincl/unused.yml
}
''',
        )
        for config, selected, flags in (
            (variants[0], "first", ()),
            (variants[1], "second", ()),
            (variants[1], "first", ("--musl",)),
            (variants[1], "first", ("--target-platform", "default-linux-x86_64")),
        ):
            with self.subTest(selected=selected, flags=flags):
                graph = lib.make(self.files(config), "m", *flags)
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertEqual({"$(S)/m/a.cpp", f"$(S)/m/{selected}.h"}, set(inputs))

    def test_imports_and_global_flags(self):
        files = self.files("SYSINCL=build/custom/first.yml\n")
        files["build/ymake_conf.py"] = lib.ymake_conf(extra='@import "${CONF_ROOT}/extra.conf"\n')
        files["build/extra.conf"] = '''when ($WITH_EXTRA == "yes") {
    @import "${CONF_ROOT}/nested.conf"
}
'''
        files["build/nested.conf"] = "SYSINCL += build/sysincl/extra.yml\n"
        files["build/sysincl/extra.yml"] = "- includes:\n  - extra.h\n"
        files["m/first.h"] += "#include <extra.h>\n"
        graph = lib.make(files, "m", "-DWITH_EXTRA=yes")
        inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
        self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/first.h"}, set(inputs))
        result = lib.make_process(files, "m", "-DWITH_EXTRA=no")
        self.assertEqual(1, result.returncode)
        self.assertIn("unresolved include <extra.h>", result.stderr)
        result = lib.make_process(files, "m", "-DWITH_EXTRA=yes", "-DNORUNTIME=yes")
        self.assertEqual(1, result.returncode)
        self.assertIn("unresolved include <core.h>", result.stderr)
        self.assertNotIn("unresolved include <extra.h>", result.stderr)

    def test_host_and_target_evaluate_separately(self):
        files = self.files('''SYSINCL=
when ($ARCH_AARCH64 == "yes") {
    SYSINCL+=build/custom/second.yml
} otherwise {
    SYSINCL+=build/custom/first.yml
}
''')
        lib.tool_program(files, "tools/gen", "gen")
        files["tools/gen/main.cpp"] = "#include <selected.h>\n"
        files["m/ya.make"] = files["m/ya.make"].replace(
            "END()", "RUN_PROGRAM(tools/gen OUT generated.h)\nEND()")
        graph = lib.make(files, "m")
        for source, suffix, selected in (("m/a.cpp", ".o", "second"), ("tools/gen/main.cpp", ".pic.o", "first")):
            inputs = lib.node_by_output(graph, f"$(B)/{source}{suffix}")["inputs"]
            self.assertEqual({f"$(S)/{source}", f"$(S)/m/{selected}.h"}, set(inputs))


if __name__ == "__main__":
    unittest.main(verbosity=2)
