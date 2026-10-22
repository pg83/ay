import unittest

import lib
from test_rodata_contexts import fixture as context_fixture


X86_64 = ("--target-platform", "default-linux-x86_64")
LIBRARY = "LIBRARY()\n" + lib.BARE_MODULE


def fixture(body):
    files = {
        "capsule/ya.make": LIBRARY + body + "END()\n",
        "capsule/FIRST_RECORD": "first\n",
        "capsule/SECOND_RECORD": "second\n",
        "build/scripts/rodata2asm.py": "",
    }
    lib.tool_program(files, "tools/archiver", "archiver")
    lib.tool_program(files, "contrib/tools/yasm", "yasm")
    return files


class ArchiveAsmContractTest(unittest.TestCase):
    def assert_archive(self, graph, directory, name, members, uncompressed=False, object_suffix=""):
        output = f"$(B)/{directory}/{name}.rodata"
        archive = lib.node_by_output(graph, output)
        tool = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
        self.assertEqual(archive["kv"], {"p": "AR", "pc": "light-cyan"})
        self.assertEqual(archive["outputs"], [output])
        self.assertEqual(archive["cmds"][0]["cmd_args"], [
            tool["outputs"][0], "-q", *(["-p"] if uncompressed else []),
            *(member + ":" for member in members), "-o", output,
        ])
        self.assertEqual(archive["inputs"], list(dict.fromkeys(members)) + tool["outputs"])
        self.assertIn(tool["uid"], archive["deps"])
        rodata = lib.node_by_output(graph, output + object_suffix + ".o")
        self.assertEqual(rodata["outputs"], [output + ".asm", output + object_suffix + ".o"])
        self.assertEqual(rodata["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/rodata2asm.py", "--elf", name, output, output + ".asm",
        ])
        self.assertIn(output, rodata["inputs"])
        self.assertIn(archive["uid"], rodata["deps"])
        return archive, rodata

    def test_arbitrary_uppercase_names_and_members_keep_lowering_and_order(self):
        for name, first, second in (
            ("ORBIT_LEDGER", "FIRST_RECORD", "SECOND_RECORD"),
            ("FROST_INDEX", "COPPER_PAGE", "SILVER_PAGE"),
        ):
            with self.subTest(name=name):
                files = fixture(f"ARCHIVE_ASM({first} NAME {name} DONTCOMPRESS {second} {first})\n")
                files[f"capsule/{first}"] = "first\n"
                files[f"capsule/{second}"] = "second\n"
                graph = lib.make(files, "capsule", *X86_64)
                _, rodata = self.assert_archive(graph, "capsule", name, [
                    f"$(S)/capsule/{first}", f"$(S)/capsule/{second}", f"$(S)/capsule/{first}",
                ], uncompressed=True)
                library = lib.node_by_output(graph, "$(B)/capsule/libcapsule.a")
                self.assertIn(rodata["uid"], library["deps"])
                self.assertTrue(set(rodata["outputs"]).issubset(library["inputs"]))

    def test_variable_expansions_supply_name_files_and_declared_options(self):
        graph = lib.make(fixture(
            "SET(ARCHIVE_LABEL EXPANDED_LEDGER)\n"
            "SET(ARCHIVE_MEMBERS SECOND_RECORD FIRST_RECORD SECOND_RECORD)\n"
            "SET(ARCHIVE_SWITCH DONTCOMPRESS)\n"
            "ARCHIVE_ASM(${ARCHIVE_SWITCH} ${ARCHIVE_MEMBERS} NAME ${ARCHIVE_LABEL})\n"
        ), "capsule", *X86_64)
        self.assert_archive(graph, "capsule", "EXPANDED_LEDGER", [
            "$(S)/capsule/SECOND_RECORD", "$(S)/capsule/FIRST_RECORD", "$(S)/capsule/SECOND_RECORD",
        ], uncompressed=True)

    def test_default_name_and_empty_variadic_inputs_follow_the_signature(self):
        for args, name, members, uncompressed in (
            ("", "", [], False),
            ("DONTCOMPRESS", "", [], True),
            ("FIRST_RECORD", "", ["$(S)/capsule/FIRST_RECORD"], False),
            ("NAME EMPTY_LEDGER", "EMPTY_LEDGER", [], False),
            ("NAME EMPTY_LEDGER DONTCOMPRESS", "EMPTY_LEDGER", [], True),
        ):
            with self.subTest(args=args):
                graph = lib.make(fixture(f"ARCHIVE_ASM({args})\n"), "capsule", *X86_64)
                archive = lib.node_by_output(graph, f"$(B)/capsule/{name}.rodata")
                self.assertEqual(archive["cmds"][0]["cmd_args"][1:], [
                    "-q", *(["-p"] if uncompressed else []),
                    *(member + ":" for member in members), "-o", f"$(B)/capsule/{name}.rodata",
                ])

    def test_generated_members_use_registered_producer_over_source_shadow(self):
        files = fixture(
            "COPY(GENERATED_RECORD FROM ${CURDIR}/incoming)\n"
            "ARCHIVE_ASM(NAME PRODUCER_LEDGER GENERATED_RECORD FIRST_RECORD GENERATED_RECORD)\n"
        )
        files["capsule/incoming/GENERATED_RECORD"] = "generated\n"
        files["capsule/GENERATED_RECORD"] = "source shadow\n"
        graph = lib.make(files, "capsule", *X86_64)
        archive, _ = self.assert_archive(graph, "capsule", "PRODUCER_LEDGER", [
            "$(B)/capsule/GENERATED_RECORD", "$(S)/capsule/FIRST_RECORD", "$(B)/capsule/GENERATED_RECORD",
        ])
        copy = lib.node_by_output(graph, "$(B)/capsule/GENERATED_RECORD")
        self.assertIn(copy["uid"], archive["deps"])
        self.assertNotIn("$(S)/capsule/GENERATED_RECORD", archive["inputs"])
        self.assertEqual(archive["deps"].count(copy["uid"]), 1)

    def test_shared_lexical_call_expands_in_distinct_module_contexts(self):
        files = fixture("")
        files["shared/archive.inc"] = "ARCHIVE_ASM(NAME ${ARCHIVE_LABEL} ${ARCHIVE_MEMBERS})\n"
        for directory, name, member in (("amber", "AMBER_LEDGER", "AMBER_RECORD"),
                                        ("cobalt", "COBALT_LEDGER", "COBALT_RECORD")):
            files[f"{directory}/ya.make"] = (
                LIBRARY + f"SET(ARCHIVE_LABEL {name})\nSET(ARCHIVE_MEMBERS {member})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/archive.inc)\nEND()\n"
            )
            files[f"{directory}/{member}"] = directory
        for targets in (("amber", "cobalt"), ("cobalt", "amber")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, *X86_64)
                for directory in targets:
                    self.assert_archive(graph, directory, directory.upper() + "_LEDGER", [
                        f"$(S)/{directory}/{directory.upper()}_RECORD",
                    ])

    def test_multimodule_suffixes_and_producer_contexts_remain_distinct(self):
        files = context_fixture()
        files.update({
            "capsule/ya.make": (
                f"YQL_UDF(renamed)\n{lib.BARE_MODULE}UDF_NO_PROBE()\n"
                "SET(ARCHIVE_LABEL ABI_LEDGER)\nSET(ARCHIVE_MEMBERS ABI_RECORD)\n"
                "ARCHIVE_ASM(NAME ${ARCHIVE_LABEL} ${ARCHIVE_MEMBERS})\nEND()\n"
            ),
            "capsule/ABI_RECORD": "abi data\n",
        })
        graph = lib.make(files, "capsule", *X86_64)
        for suffix, filename in ((".udfs", "librenamed.a"), (".udfs.abi_current", "yql-abi-current-renamed.a")):
            with self.subTest(suffix=suffix):
                _, rodata = self.assert_archive(graph, "capsule", "ABI_LEDGER" + suffix, [
                    "$(S)/capsule/ABI_RECORD",
                ], object_suffix=suffix)
                library = lib.node_by_output(graph, "$(B)/capsule/" + filename)
                self.assertIn(rodata["uid"], library["deps"])
                self.assertTrue(set(rodata["outputs"]).issubset(library["inputs"]))

    def test_archive_values_do_not_exempt_later_macro_validation(self):
        for value in ("ORBIT_LEDGER", "FIRST_RECORD", "SECOND_RECORD"):
            with self.subTest(value=value):
                result = lib.make_process(fixture(
                    "ARCHIVE_ASM(NAME ORBIT_LEDGER FIRST_RECORD SECOND_RECORD)\n"
                    f"ALLOCATOR({value})\n"
                ), "capsule", *X86_64)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f'macro ALLOCATOR received service-keyword "{value}"', result.stderr)

    def test_declared_scalar_and_flag_errors_survive_arbitrary_values(self):
        for args, error in (
            ("NAME", "missing value after NAME"),
            ("NAME DONTCOMPRESS FIRST_RECORD", "missing value after NAME"),
            ("NAME NAME ORBIT_LEDGER FIRST_RECORD", "missing value after NAME"),
            ("NAME ORBIT_LEDGER FIRST_RECORD NAME FROST_INDEX", "Keyword 'NAME' appears more than once and is not an array."),
            ("DONTCOMPRESS NAME ORBIT_LEDGER FIRST_RECORD DONTCOMPRESS", "Keyword 'DONTCOMPRESS' appears more than once and is not an array."),
        ):
            with self.subTest(args=args):
                result = lib.make_process(fixture(f"ARCHIVE_ASM({args})\n"), "capsule", *X86_64)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
