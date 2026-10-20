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

    def test_conditions_apply_after_unconditional_assignments(self):
        for op, selected in (("=", {"first"}), ("+=", {"first", "second"})):
            with self.subTest(operator=op):
                files = self.files(f'''when ($OS_LINUX == "yes") {{
    SYSINCL{op}build/custom/first.yml
}}
SYSINCL=build/custom/second.yml
''')
                graph = lib.make(files, "m")
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertEqual({"$(S)/m/a.cpp"} | {f"$(S)/m/{name}.h" for name in selected}, set(inputs))

    def test_conditional_assignments_observe_prior_actions(self):
        set_second = '''when ($OS_LINUX == "yes") {
    SYSINCL=build/custom/second.yml
}
'''
        append_third = '''when ($SYSINCL == "build/custom/second.yml") {
    SYSINCL+=build/custom/third.yml
}
'''
        for imported in (False, True):
            with self.subTest(imported=imported):
                config = "SYSINCL=build/custom/first.yml\n"
                config += '@import "${CONF_ROOT}/conf/set_sysincl.conf"\n' if imported else set_second
                files = self.files(config + append_third)
                files["build/conf/set_sysincl.conf"] = set_second
                files["build/custom/third.yml"] = "- includes:\n  - selected.h: m/third.h\n"
                files["m/third.h"] = ""
                graph = lib.make(files, "m")
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/second.h", "$(S)/m/third.h"}, set(inputs))

    def test_imports_and_global_flags(self):
        files = self.files("SYSINCL=build/custom/first.yml\n")
        files["build/ymake_conf.py"] = lib.ymake_conf(extra='@import "${CONF_ROOT}/conf/extra.conf"\n')
        files["build/conf/extra.conf"] = '@import "${CONF_ROOT}/conf/nested.conf"\n'
        files["build/conf/nested.conf"] = '''when ($WITH_EXTRA == "yes") {
    SYSINCL += build/sysincl/extra.yml
}
'''
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

    def test_referenced_platform_variable_selects_yaml(self):
        files = self.files("SYSINCL=${EXTRA_SYSINCL}\n")
        graph = lib.make(files, "m", "-DEXTRA_SYSINCL=build/custom/second.yml", "--host-platform-flag=EXTRA_SYSINCL=build/custom/first.yml")
        inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
        self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/second.h"}, set(inputs))

    def test_imported_scalar_dependencies_are_retained(self):
        variants = (
            "EXTRA_SYSINCL=build/custom/first.yml\nSYSINCL=$EXTRA_SYSINCL\n",
            '''@import "${CONF_ROOT}/conf/aliases.conf"
SYSINCL=${EXTRA_SYSINCL}
''',
            '''@import "${CONF_ROOT}/conf/aliases.conf"
SYSINCL=build/custom/second.yml
when ($USE_CUSTOM_SYSINCL == "yes") {
    SYSINCL=$EXTRA_SYSINCL
}
''',
        )
        for config in variants:
            with self.subTest(config=config):
                files = self.files(config)
                files["build/conf/aliases.conf"] = '''EXTRA_SYSINCL=${CUSTOM_SYSINCL}
CUSTOM_SYSINCL=build/custom/second.yml
USE_CUSTOM_SYSINCL=no
when ($OS_LINUX == "yes") {
    CUSTOM_SYSINCL=build/custom/first.yml
    USE_CUSTOM_SYSINCL=yes
}
'''
                graph = lib.make(files, "m")
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/first.h"}, set(inputs))

    def test_unset_and_deep_scalar_expansion(self):
        config = "SYSINCL=$YAML_0 ${UNSET_SYSINCL}\n"
        config += "".join(f"YAML_{i}=${{YAML_{i+1}}}\n" for i in range(20))
        for variant in ("SYSINCL=build/custom/first.yml ${UNSET_SYSINCL}\n", config + "YAML_20=build/custom/first.yml\n"):
            with self.subTest(config=variant):
                graph = lib.make(self.files(variant), "m")
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/first.h"}, set(inputs))

    def test_define_and_exclude_scalar_assignments(self):
        for config in (
            "EXTRA_SYSINCL:=build/custom/notfirst.yml\n"
            "EXTRA_SYSINCL:=build/custom/second.yml\n"
            "EXTRA_SYSINCL-=not\nSYSINCL=$EXTRA_SYSINCL\n",
            '''SYSINCL=build/custom/first.yml
when ($OS_LINUX == "yes") {
    SYSINCL-=build/custom/first.yml
}
''',
        ):
            with self.subTest(config=config):
                graph = lib.make(self.files(config), "m")
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/first.h"}, set(inputs))

    def test_select_expands_to_live_conditional_alternatives(self):
        files = self.files('''SYSINCL=
select ($HARDWARE_TYPE) {
    "x86_64" | "aarch64" ? {
        SYSINCL=build/custom/first.yml
    }
    default ? {
        SYSINCL=build/custom/second.yml
    }
}
''')
        for flags, selected in (((), "first"), (("-DHARDWARE_TYPE=other",), "second")):
            with self.subTest(flags=flags):
                graph = lib.make(files, "m", *flags)
                inputs = lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
                self.assertEqual({"$(S)/m/a.cpp", f"$(S)/m/{selected}.h"}, set(inputs))

    def test_printed_assignments_override_imported_defaults(self):
        files = self.files("EXTRA_SYSINCL=build/custom/first.yml\nSYSINCL=$EXTRA_SYSINCL\n")
        graph = lib.make(files, "m", "-DEXTRA_SYSINCL=build/custom/second.yml")
        self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/second.h"}, set(lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]))
        files["build/ymake_conf.py"] = lib.ymake_conf(extra='''when ($OS_LINUX == "yes") {
    EXTRA_SYSINCL=build/custom/second.yml
}
''')
        graph = lib.make(files, "m")
        self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/second.h"}, set(lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]))

    def test_appending_to_empty_variable_does_not_add_leading_space(self):
        graph = lib.make(self.files('''SYSINCL=
SYSINCL+=build/custom/first.yml
when ($SYSINCL == "build/custom/first.yml") {
    SYSINCL=build/custom/second.yml
}
'''), "m")
        self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/second.h"}, set(lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]))

    def test_select_rechecks_the_variable_after_an_action(self):
        graph = lib.make(self.files('''SYSINCL=build/custom/first.yml
CHOICE=first
select ($CHOICE) {
    "first" ? {
        CHOICE=second
    }
    "second" ? {
        SYSINCL=build/custom/second.yml
    }
    default ? {
        SYSINCL=build/sysincl/unused.yml
    }
}
'''), "m")
        self.assertEqual({"$(S)/m/a.cpp", "$(S)/m/second.h"}, set(lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]))

    def test_missing_configuration_files_are_reported(self):
        for config, missing in ((
            '@import "${CONF_ROOT}/conf/missing.conf"\n', "build/conf/missing.conf",
        ), ("SYSINCL=build/sysincl/missing.yml\n", "build/sysincl/missing.yml")):
            with self.subTest(missing=missing):
                files = self.files(config)
                files["m/a.cpp"] = ""
                result = lib.make_process(files, "m")
                self.assertEqual(1, result.returncode)
                self.assertIn(missing, result.stderr)

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
