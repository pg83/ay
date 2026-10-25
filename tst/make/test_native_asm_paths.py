import unittest

from tst import lib
from tst.make.test_go_package import go_module, go_world


MODULE = "birch/parcel"
X86 = ("--target-platform", "default-linux-x86_64")


def assembly_world(body, source="nested/leaf.S"):
    return {
        f"{MODULE}/ya.make": f"LIBRARY(parcel)\n{lib.BARE_MODULE}CFLAGS(-DMODULE_ASM)\n" + body + "END()\n",
        f"{MODULE}/{source}": '#include "common.h"\n',
        f"{MODULE}/nested/common.h": "// actual include\n",
    }


def assembler(graph, output):
    node = lib.node_by_output(graph, "$(B)/" + output)
    assert node["kv"] == {"p": "AS", "pc": "light-green"}, node["kv"]
    return node


class NativeAssemblerPathsTest(unittest.TestCase):
    def test_src_and_srcs_crossed_extensions_and_flag_presence(self):
        for extension in ("S", "s"):
            for form, flagged in (("SRC", False), ("SRC", True), ("SRCS", False), ("SRCS", True)):
                with self.subTest(extension=extension, form=form, flagged=flagged):
                    source = f"nested/leaf.{extension}"
                    # SRCS has no FLAGS argument; SFLAGS is the module flag control.
                    setup = "SET_APPEND(SFLAGS -mavx)\n" if form == "SRCS" and flagged else ""
                    tail = " -mavx" if form == "SRC" and flagged else ""
                    files = assembly_world(setup + f"{form}({source}{tail})\n", source)
                    graph = lib.make(files, MODULE, *X86)
                    relative = source if form == "SRC" else "_/" + source
                    output = f"{MODULE}/{relative}.o"
                    node = assembler(graph, output)
                    command = node["cmds"][0]["cmd_args"]
                    self.assertEqual(command[command.index("-c"):command.index("-c") + 4], [
                        "-c", "-o", "$(B)/" + output, f"$(S)/{MODULE}/{source}",
                    ])
                    self.assertEqual("-mavx" in command, flagged)
                    self.assertIn("-DMODULE_ASM", command)
                    self.assertEqual(node["inputs"], [f"$(S)/{MODULE}/{source}", f"$(S)/{MODULE}/nested/common.h"])
                    archive = lib.node_by_output(graph, f"$(B)/{MODULE}/libparcel.a")
                    self.assertIn(node["uid"], archive["deps"])
                    self.assertIn("$(B)/" + output, archive["inputs"])

    def test_src_original_token_controls_outputs_independently_of_resolved_input(self):
        cases = (
            ("leaf.S", "birch/pool/leaf.S", f"{MODULE}/leaf.S.o", "SRCDIR(birch/pool)\n"),
            ("../pool/leaf.S", "birch/pool/leaf.S", "birch/pool/leaf.S.o", ""),
            ("${ARCADIA_ROOT}/birch/pool/leaf.S", "birch/pool/leaf.S", "birch/pool/leaf.S.o", ""),
            (f"${{ARCADIA_ROOT}}/{MODULE}/nested/leaf.S", f"{MODULE}/nested/leaf.S", f"{MODULE}/nested/leaf.S.o", ""),
            (f"{MODULE}/nested/leaf.S", f"{MODULE}/nested/leaf.S", f"{MODULE}/{MODULE}/nested/leaf.S.o", ""),
            ("nested/../branch/leaf.S", f"{MODULE}/branch/leaf.S", f"{MODULE}/branch/leaf.S.o", ""),
        )
        for token, source, output, setup in cases:
            with self.subTest(token=token):
                files = assembly_world(setup + f"SRC({token} -mavx)\n")
                files[source] = ""
                graph = lib.make(files, MODULE, *X86)
                node = assembler(graph, output)
                self.assertEqual(node["inputs"], ["$(S)/" + source])
                command = node["cmds"][0]["cmd_args"]
                self.assertIn("-mavx", command)
                self.assertEqual(command[command.index("-o") + 1:command.index("-o") + 3], ["$(B)/" + output, "$(S)/" + source])

    def test_srcs_keeps_resolved_directory_transform_for_external_and_explicit_sources(self):
        cases = (
            ("leaf.S", "birch/pool/leaf.S", "__/pool/leaf.S.o", "SRCDIR(birch/pool)\n"),
            ("../pool/leaf.S", "birch/pool/leaf.S", "__/pool/leaf.S.o", ""),
            ("${ARCADIA_ROOT}/birch/pool/leaf.S", "birch/pool/leaf.S", "__/pool/leaf.S.o", ""),
            (f"${{ARCADIA_ROOT}}/{MODULE}/nested/leaf.S", f"{MODULE}/nested/leaf.S", "_/nested/leaf.S.o", ""),
            (f"{MODULE}/nested/leaf.S", f"{MODULE}/nested/leaf.S", "_/nested/leaf.S.o", ""),
            ("nested/../branch/leaf.S", f"{MODULE}/branch/leaf.S", "_/branch/leaf.S.o", ""),
        )
        for token, source, relative, setup in cases:
            with self.subTest(token=token):
                files = assembly_world(setup + f"SRCS({token})\n")
                files[source] = ""
                node = assembler(lib.make(files, MODULE, *X86), f"{MODULE}/{relative}")
                self.assertEqual(node["inputs"], ["$(S)/" + source])

    def test_generated_assembly_preserves_real_producer_and_main_output_edges(self):
        for form in ("SRC", "SRCS"):
            for explicit in (False, True):
                with self.subTest(form=form, explicit=explicit):
                    main = "birch/created/main.txt" if explicit else f"{MODULE}/main.txt"
                    source = "birch/created/leaf.S" if explicit else f"{MODULE}/nested/created.S"
                    first = "${ARCADIA_BUILD_ROOT}/" + main if explicit else "main.txt"
                    second = "${ARCADIA_BUILD_ROOT}/" + source if explicit else "nested/created.S"
                    output = (source + ".o" if explicit else f"{MODULE}/nested/created.S.o") if form == "SRC" else (
                        f"{MODULE}/__/created/leaf.S.o" if explicit else f"{MODULE}/_/nested/created.S.o")
                    files = assembly_world(
                        f"RUN_PROGRAM(generators/seed OUT_NOAUTO {first} {second} OUTPUT_INCLUDES {MODULE}/local.h)\n"
                        f"{form}({second}{' -mavx' if form == 'SRC' else ''})\n"
                    )
                    files[f"{MODULE}/local.h"] = ""
                    lib.tool_program(files, "generators/seed", "seed")
                    graph = lib.make(files, MODULE, *X86)
                    node = assembler(graph, output)
                    producer = lib.node_by_output(graph, "$(B)/" + main)
                    self.assertEqual(producer["outputs"], ["$(B)/" + main, "$(B)/" + source])
                    self.assertIn(producer["uid"], node["deps"])
                    self.assertEqual(node["inputs"], ["$(B)/" + source, "$(B)/" + main, f"$(S)/{MODULE}/local.h"])
                    self.assertNotIn("$(S)/" + source, node["inputs"])
                    archive = lib.node_by_output(graph, f"$(B)/{MODULE}/libparcel.a")
                    self.assertIn("$(B)/" + output, archive["inputs"])
                    self.assertIn(node["uid"], archive["deps"])

    def test_host_gnu_assembly_uses_o_while_yasm_keeps_pic_suffix(self):
        files = {
            "caller/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(${{ARCADIA_BUILD_ROOT}}/caller)\nSRCS(main.cpp)\nRUN_PROGRAM(tools/packer OUT_NOAUTO packed.bin)\nEND()\n",
            "caller/main.cpp": '#include "packed.bin"\nint consumer;\n',
            "tools/packer/ya.make": f"PROGRAM(packer)\n{lib.BARE_MODULE}SRC(nested/driver.S -mavx)\nSRCS(nested/ordinary.s nested/encoded.asm)\nEND()\n",
            "tools/packer/nested/driver.S": "",
            "tools/packer/nested/ordinary.s": "",
            "tools/packer/nested/encoded.asm": "",
        }
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "caller", *X86)
        src = assembler(graph, "tools/packer/nested/driver.S.o")
        srcs = assembler(graph, "tools/packer/_/nested/ordinary.s.o")
        yasm = lib.node_by_output(graph, "$(B)/tools/packer/_/nested/encoded.pic.o")
        tool = lib.node_by_output(graph, "$(B)/tools/packer/packer")
        consumer = lib.node_by_output(graph, "$(B)/caller/main.cpp.o")
        self.assertCountEqual(["$(S)/caller/main.cpp", "$(B)/caller/packed.bin"], consumer["inputs"])
        producer = lib.node_by_output(graph, "$(B)/caller/packed.bin")
        self.assertIn(producer["uid"], consumer["deps"])
        self.assertIn(tool["uid"], producer["deps"])
        for node in (src, srcs, yasm):
            self.assertIn(node["outputs"][0], tool["inputs"])
            self.assertIn(node["uid"], tool["deps"])
        self.assertEqual(yasm["cmds"][0]["cmd_args"][-1], "$(S)/tools/packer/nested/encoded.asm")

    def test_go_assembly_output_stem_and_go_assembler_are_preserved(self):
        files = go_world()
        go_module(files, "birch/goparcel", srcs="goparcel.go nested/native.S nested/go.s")
        files["birch/goparcel/nested/native.S"] = ""
        files["birch/goparcel/nested/go.s"] = ""
        graph = lib.make(files, "birch/goparcel", *X86)
        native = assembler(graph, "birch/goparcel/nested/native.S.o")
        go_asm = lib.node_by_output(graph, "$(B)/birch/goparcel/gen.symabis")
        self.assertIn("$(S)/birch/goparcel/nested/native.S", native["inputs"])
        self.assertNotEqual(go_asm["kv"]["p"], "AS")
        package = lib.node_by_output(graph, "$(B)/birch/goparcel/goparcel.a")
        for node in (native, go_asm):
            self.assertIn(node["uid"], package["deps"])
            self.assertIn(node["outputs"][0], package["inputs"])
        self.assertIn("$(S)/birch/goparcel/nested/go.s", package["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
