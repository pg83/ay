import unittest

import lib


def aliases(length):
    return "".join(f"STEP{i}=$STEP{i + 1}\n" for i in range(length)) + f"STEP{length}=finished\n"


def parity_files(body, extra=""):
    return lib.java_tree(**{
        "shared/facade.inc": body,
        "ochre/native/ya.make": (
            f"LIBRARY()\n{lib.BARE_MODULE}INCLUDE(${{ARCADIA_ROOT}}/shared/facade.inc)\n"
            "IF (PHASE_MATCH)\nSRCS(selected.cpp)\nENDIF()\nEND()\n"
        ),
        "ochre/native/selected.cpp": "int selected;\n",
        "ochre/jvm/ya.make": (
            "JAVA_CONTRIB()\nINCLUDE(${ARCADIA_ROOT}/shared/facade.inc)\n"
            "IF (PHASE_MATCH)\nLOCAL_JAR(selected.jar)\nENDIF()\nEND()\n"
        ),
        "ochre/jvm/selected.jar": "selected fixture jar",
        "build/ymake_conf.py": lib.ymake_conf(extra=extra),
    })


def matching(name, expected):
    expected = expected.replace('"', '\\"')
    return f'IF ({name} == "{expected}")\nENABLE(PHASE_MATCH)\nENDIF()\n'


class DeclarativeMacroPhaseTest(unittest.TestCase):
    def parity(self, body, extra=""):
        graph = lib.make(parity_files(body, extra), "ochre/native", "ochre/jvm")
        lib.node_by_output(graph, "$(B)/ochre/native/selected.cpp.o")
        self.assertTrue(any("$(S)/ochre/jvm/selected.jar" in node["inputs"] for node in graph["graph"]))
        return graph

    def test_long_config_aliases_expand_once_at_the_shared_body_stage(self):
        for length in (12, 30, 124):
            with self.subTest(length=length):
                self.parity("WINDOWS_MANIFEST($STEP0)\n" + matching("WINDOWS_MANIFEST", "finished"),
                            aliases(length))

    def test_bound_parameter_shadows_the_outer_variable(self):
        self.parity("WINDOWS_MANIFEST($Manifest)\n" + matching("WINDOWS_MANIFEST", "$Manifest"),
                    "Manifest=outer\n")

    def test_signature_splitting_precedes_deep_body_expansion(self):
        self.parity("WINDOWS_MANIFEST($CHOICE)\n" + matching("WINDOWS_MANIFEST", "first second"),
                    "CHOICE=first second\n")
        self.parity("WINDOWS_MANIFEST(${ALIAS})\n" + matching("WINDOWS_MANIFEST", "first second"),
                    "ALIAS=$CHOICE\nCHOICE=first second\n")
        for target in ("ochre/native", "ochre/jvm"):
            with self.subTest(target=target):
                result = lib.make_process(parity_files("WINDOWS_MANIFEST(${CHOICE})\n",
                                                       "CHOICE=first second\n"), target)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("WINDOWS_MANIFEST expects one argument", result.stderr)

    def test_unset_raw_and_escaped_references_keep_the_upstream_phase(self):
        for argument, expected in (("$UNBOUND", "$UNBOUND"),
                                   (r"\\\${CHOICE}", r"\${CHOICE}"),
                                   (r"\\\$CHOICE", "$CHOICE"),
                                   ("$RAW_CHOICE", '${"$CHOICE"}'),
                                   ("${MINT_RESOURCE_GLOBAL}/bin/tool", "${MINT_RESOURCE_GLOBAL}/bin/tool")):
            with self.subTest(argument=argument):
                self.parity(f"WINDOWS_MANIFEST({argument})\n" + matching("WINDOWS_MANIFEST", expected),
                            'CHOICE=finished\nRAW_CHOICE=${"$CHOICE"}\n')

    def test_roots_remain_in_the_correct_namespace_and_module_phase(self):
        for argument, expected in (("${ARCADIA_ROOT}/payload", "${ARCADIA_ROOT}/payload"),
                                   ("${ARCADIA_BUILD_ROOT}/payload", "${ARCADIA_BUILD_ROOT}/payload"),
                                   ("${BINDIR}/payload", r"\${BINDIR}/payload"),
                                   ("${CURDIR}/payload", r"\${CURDIR}/payload"),
                                   ("$BINDIR/payload", "$BINDIR/payload")):
            with self.subTest(argument=argument):
                self.parity(f"WINDOWS_MANIFEST({argument})\n" + matching("WINDOWS_MANIFEST", expected))

    def test_direct_set_and_default_remain_one_pass(self):
        body = ("SET(DIRECT ${STEP0})\nDEFAULT(DEFAULTED ${STEP0})\n"
                'IF (DIRECT == "\\${STEP1}" AND DEFAULTED == "\\${STEP1}")\n'
                "WINDOWS_MANIFEST($STEP0)\n" + matching("WINDOWS_MANIFEST", "finished") + "ENDIF()\n")
        self.parity(body, "STEP0=${STEP1}\nSTEP1=finished\n")

    def test_existing_scalar_and_constant_facades_share_the_stage(self):
        self.parity("VALIDATE_DATA_RESTART($STEP0)\n" + matching("SBR_UID_EXT", "finished"), aliases(12))
        self.parity("SET_CUDA_NODE_RAM_REQUIEREMENT($STEP0)\n" +
                    matching("_CU_NODE_MAX_RAM_REQUIREMENT", "ram:finished"), aliases(12))
        self.parity("NO_COW()\nNO_NEED_CHECK()\n"
                    'IF (USE_COW == "no" AND UNUSED_MACRO == "yes")\nENABLE(PHASE_MATCH)\nENDIF()\n')
        self.parity("WINDOWS_LONG_PATH_MANIFEST()\n" +
                    matching("WINDOWS_MANIFEST", "build/long-path.manifest"))

    def test_macro_body_split_collapses_spaces_before_source_set(self):
        self.parity('WINDOWS_MANIFEST("first   second")\n' + matching("WINDOWS_MANIFEST", "first second"))

    def test_upstream_substitution_limit_includes_parameter_binding(self):
        for target in ("ochre/native", "ochre/jvm"):
            for configuration in (aliases(125), "STEP0=$STEP0/again\n"):
                with self.subTest(target=target, configuration=configuration[:20]):
                    result = lib.make_process(parity_files("WINDOWS_MANIFEST($STEP0)\n", configuration), target)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("Macro subst exceeded the limit 128", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
