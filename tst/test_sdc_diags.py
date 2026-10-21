import unittest

import lib


ROOT = "sdg/sdc/ros/monitorings/diag_updater/etc/Descriptions"
GENERATED = "$(B)/sdg/sdc/ros/monitorings/diag_updater/gen_diags"
GENERATOR = "sdg/sdc/ros/monitorings/diag_updater/utils/diags_split_generator"
SEVERITY = "sdg/sdc/ros/misc/protocol/DiagSeverity.h"


def files():
    result = {}
    lib.tool_program(result, GENERATOR, "diags_split_generator")
    lib.tool_program(result, "tools/severity", "severity")
    for name in ("group", "diagnostic"):
        result[f"sdg/sdc/ros/monitorings/diag_updater/include/diagnostics/{name}.h"] = "#pragma once\n"
    return result


class SdcDiagsTest(unittest.TestCase):
    def test_v3_group_headers_keep_parent_producers_and_manifest_inputs(self):
        sources = files()
        groups = ["Hardware", "Hardware/Hostname", "Hardware/Hostname/Device"]
        for i, name in enumerate(groups):
            module = f"{ROOT}/{name}"
            parent = f"PEERDIR({ROOT}/{groups[i - 1]})\n" if i else ""
            severity = f"RUN_PROGRAM(tools/severity OUT_NOAUTO ${{ARCADIA_BUILD_ROOT}}/{SEVERITY})\n" if not i else ""
            sources[f"{module}/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}\n{parent}{severity}SDC_DIAGS_SPLIT_GENERATOR_V3({module})\nEND()\n"
            sources[f"{module}/_manifest_.yaml"] = "name: group\n"
        deepest = f"{GENERATED}/diagnostics/v3/{groups[-1]}/_group.h"
        sources["app/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}\nPEERDIR({ROOT}/{groups[-1]})\nSRCS(main.cpp)\nEND()\n"
        sources["app/main.cpp"] = f'#include "{deepest.removeprefix("$(B)/")}"\nint main() {{ return 0; }}\n'
        graph = lib.make(sources, "app")
        for i, name in enumerate(groups):
            module = f"{ROOT}/{name}"
            output = f"{GENERATED}/diagnostics/v3/{name}/_group.h"
            node = lib.node_by_output(graph, output)
            expected = [f"$(B)/{GENERATOR}/diags_split_generator", "--group-manifest", f"$(S)/{module}/_manifest_.yaml",
                        "--cpp", output, "--cpp-namespace", "yandex::sdc::diagnostics::v3::" + name.replace("/", "::")]
            if i:
                expected += ["--group-include-file", f"diagnostics/v3/{groups[i - 1]}/_group.h"]
            self.assertEqual(node["cmds"][0]["cmd_args"], expected)
            self.assertIn(f"$(S)/{module}/_manifest_.yaml", node["inputs"])
        compile_node = lib.node_by_output(graph, "$(B)/app/main.cpp.o")
        for name in groups:
            self.assertIn(f"{GENERATED}/diagnostics/v3/{name}/_group.h", compile_node["inputs"])
        self.assertIn(f"$(B)/{SEVERITY}", compile_node["inputs"])

    def test_v3_group_directory_resolves_module_relative_and_rooted_paths(self):
        module = f"{ROOT}/Hardware"
        child = f"{module}/Hostname"
        output = f"{GENERATED}/diagnostics/v3/Hardware/Hostname/_group.h"
        for argument in ("Hostname", "${CURDIR}/Hostname", "${ARCADIA_ROOT}/" + child, child):
            with self.subTest(argument=argument):
                sources = files()
                sources[f"{module}/ya.make"] = (
                    f"LIBRARY()\n{lib.BARE_MODULE}\n"
                    f"RUN_PROGRAM(tools/severity OUT_NOAUTO ${{ARCADIA_BUILD_ROOT}}/{SEVERITY})\n"
                    f"SDC_DIAGS_SPLIT_GENERATOR_V3({module})\n"
                    f"SDC_DIAGS_SPLIT_GENERATOR_V3({argument})\nEND()\n"
                )
                sources[f"{module}/_manifest_.yaml"] = "name: hardware\n"
                sources[f"{child}/_manifest_.yaml"] = "name: hostname\n"
                sources["app/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}\nPEERDIR({module})\nSRCS(main.cpp)\nEND()\n"
                sources["app/main.cpp"] = f'#include "{output.removeprefix("$(B)/")}"\nint main() {{ return 0; }}\n'
                graph = lib.make(sources, "app")
                generated = lib.node_by_output(graph, output)
                self.assertIn(f"$(S)/{child}/_manifest_.yaml", generated["inputs"])

    def test_v3_leaf_resolves_curdir_and_places_group_option_before_file(self):
        sources = files()
        module = f"{ROOT}/Hardware"
        output = f"{GENERATED}/diagnostics/v3/Hardware/Temperature.h"
        sources[f"{module}/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}\n"
            f"RUN_PROGRAM(tools/severity OUT_NOAUTO ${{ARCADIA_BUILD_ROOT}}/{SEVERITY})\n"
            f"SDC_DIAGS_SPLIT_GENERATOR_V3({module})\n"
            "SDC_DIAGS_SPLIT_GENERATOR_V3(${CURDIR}/Temperature.yaml)\nEND()\n"
        )
        sources[f"{module}/Temperature.yaml"] = "name: temperature\n"
        sources[f"{module}/_manifest_.yaml"] = "name: group\n"
        sources["app/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}\nPEERDIR({module})\nSRCS(main.cpp)\nEND()\n"
        sources["app/main.cpp"] = f'#include "{output.removeprefix("$(B)/")}"\nint main() {{ return 0; }}\n'
        graph = lib.make(sources, "app")
        node = lib.node_by_output(graph, output)
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            f"$(B)/{GENERATOR}/diags_split_generator", "--group-include-file", "diagnostics/v3/Hardware/_group.h",
            "--diag-file", f"$(S)/{module}/Temperature.yaml", "--cpp", output, "--cpp-namespace",
            "yandex::sdc::diagnostics::v3::Hardware::Temperature",
        ])
        self.assertIn(f"$(S)/{module}/Temperature.yaml", node["inputs"])


if __name__ == "__main__":
    unittest.main()
