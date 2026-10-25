import unittest

from tst import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def fixture(declaration="UDF_BASE()", body=""):
    return {
        "grove/addon/ya.make": (
            f"{declaration}\n{BARE}{body}"
            "COPY_FILE(seed.cpp generated.cpp)\n"
            "SRCS(body.cpp generated.cpp GLOBAL global.cpp)\n"
            "PEERDIR(grove/dep)\nEND()\n"
        ),
        "grove/addon/body.cpp": '#include "seed.h"\nextern "C" int probe(){return VALUE;}\n',
        "grove/addon/seed.h": "#define VALUE 7\n",
        "grove/addon/seed.cpp": "int generated_value;\n",
        "grove/addon/global.cpp": "int global_value;\n",
        "grove/addon/api.exports": "C probe\n",
        "grove/dep/ya.make": f"LIBRARY(dep)\n{BARE}CFLAGS(GLOBAL -DDEPENDENCY_MARKER)\nSRCS(dep.cpp)\nEND()\n",
        "grove/dep/dep.cpp": "int dependency;\n",
    }


def link_args(node):
    return next(cmd["cmd_args"] for cmd in node["cmds"] if "--target" in cmd["cmd_args"])


class UdfBaseTest(unittest.TestCase):
    def test_default_dll_preserves_generated_global_peer_and_header_producers(self):
        graph = lib.make(fixture(), "grove/addon", "--add-modules-to-results")
        link = lib.node_by_output(graph, "$(B)/grove/addon/libgrove-addon.so")
        args = link_args(link)
        self.assertIn(link["uid"], graph["result"])
        self.assertLess(args.index("-Wl,-z,notext"), args.index("-Wl,-Bsymbolic"))
        self.assertFalse(any("udf_probe" in output for node in graph["graph"] for output in node["outputs"]))
        for source in ("body.cpp", "generated.cpp", "global.cpp"):
            compiled = lib.node_by_output(graph, f"$(B)/grove/addon/{source}.pic.o")
            self.assertIn(compiled["uid"], link["deps"])
            self.assertIn("-fPIC", compiled["cmds"][0]["cmd_args"])
            self.assertNotIn("-DBUILD_UDF", compiled["cmds"][0]["cmd_args"])
        compiled = lib.node_by_output(graph, "$(B)/grove/addon/body.cpp.pic.o")
        self.assertIn("$(S)/grove/addon/seed.h", compiled["inputs"])
        self.assertIn("-DDEPENDENCY_MARKER", compiled["cmds"][0]["cmd_args"])
        copied = lib.node_by_output(graph, "$(B)/grove/addon/generated.cpp")
        generated = lib.node_by_output(graph, "$(B)/grove/addon/generated.cpp.pic.o")
        self.assertIn("$(S)/grove/addon/seed.cpp", copied["inputs"])
        self.assertIn(copied["uid"], generated["deps"])
        peer = lib.node_by_output(graph, "$(B)/grove/dep/libdep.a")
        self.assertIn(peer["uid"], link["deps"])
        self.assertIn("$(B)/grove/dep/libdep.a", link["inputs"])

    def test_named_version_prefix_and_exports_use_dll_contract(self):
        graph = lib.make(fixture("UDF_BASE(renamed 4 7 PREFIX ext_)", "EXPORTS_SCRIPT(api.exports)\n"), "grove/addon")
        link = lib.node_by_output(graph, "$(B)/grove/addon/ext_renamed.so")
        self.assertEqual(link["outputs"], ["$(B)/grove/addon/ext_renamed.so", "$(B)/grove/addon/ext_renamed.so.4"])
        self.assertIn("-Wl,-soname,ext_renamed.so.4", link_args(link))
        self.assertIn("$(S)/grove/addon/api.exports", link["inputs"])

    def test_inherited_scalars_are_visible_before_body_statements(self):
        body = "".join(
            f'IF ({name} == "{value}")\nCFLAGS(-D{name}_MATCH)\nENDIF()\n'
            for name, value in (("MODULE_TYPE", "DLL"), ("MODULE_TAG", "DLL"), ("MODULE_LANG", "CPP"), ("ALLOCATOR", "FAKE"), ("DYNAMIC_LINK", "yes"))
        )
        files = fixture("UDF_BASE(renamed)", body)
        files["grove/addon/ya.make"] = "SET(ALLOCATOR SYSTEM)\n" + files["grove/addon/ya.make"]
        graph = lib.make(files, "grove/addon")
        compiled = lib.node_by_output(graph, "$(B)/grove/addon/body.cpp.pic.o")
        for name in ("MODULE_TYPE", "MODULE_TAG", "MODULE_LANG", "ALLOCATOR", "DYNAMIC_LINK"):
            self.assertIn(f"-D{name}_MATCH", compiled["cmds"][0]["cmd_args"])

    def test_conditional_symbolic_uses_final_source_scalars(self):
        for body in ("SET(DARWIN yes)\n", "SET(MSVC yes)\n"):
            with self.subTest(body=body):
                graph = lib.make(fixture("UDF_BASE(renamed)", body), "grove/addon")
                link = lib.node_by_output(graph, "$(B)/grove/addon/librenamed.so")
                self.assertNotIn("-Wl,-Bsymbolic", link_args(link))

    def test_ordinary_dll_keeps_its_link_flags(self):
        graph = lib.make(fixture("DLL(renamed)"), "grove/addon")
        link = lib.node_by_output(graph, "$(B)/grove/addon/librenamed.so")
        self.assertNotIn("-Wl,-Bsymbolic", link_args(link))

    def test_exports_file_variable_is_inherited(self):
        files = fixture("UDF_BASE(renamed)")
        files["grove/addon/ya.make"] = "SET(EXPORTS_FILE api.exports)\n" + files["grove/addon/ya.make"]
        graph = lib.make(files, "grove/addon")
        link = lib.node_by_output(graph, "$(B)/grove/addon/librenamed.so")
        self.assertIn("$(S)/grove/addon/api.exports", link["inputs"])

    def test_dll_unit_keywords_before_name_and_default_name(self):
        for module in ("UDF_BASE", "DLL"):
            for arguments, output in (("PREFIX ext_ renamed 4", "ext_renamed.so"), ("PREFIX ext_", "ext_grove-addon.so"), ("renamed PREFIX", "librenamed.so")):
                with self.subTest(module=module, arguments=arguments):
                    graph = lib.make(fixture(f"{module}({arguments})"), "grove/addon")
                    link = lib.node_by_output(graph, f"$(B)/grove/addon/{output}")
                    self.assertEqual("-Wl,-Bsymbolic" in link_args(link), module == "UDF_BASE")

    def test_prefix_evaluation_does_not_expand_literal_basename(self):
        files = fixture("UDF_BASE(PREFIX $PREFIX $NAME)")
        files["grove/addon/ya.make"] = "SET(PREFIX bound_)\nSET(NAME expanded)\n" + files["grove/addon/ya.make"]
        graph = lib.make(files, "grove/addon")
        lib.node_by_output(graph, "$(B)/grove/addon/bound_$NAME.so")

    def test_body_allocator_overrides_inherited_fake(self):
        files = fixture("UDF_BASE(renamed)", 'ALLOCATOR(SYSTEM)\nIF (ALLOCATOR == "SYSTEM")\nCFLAGS(-DSYSTEM_AFTER_HEADER)\nENDIF()\n')
        files["library/cpp/malloc/system/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        graph = lib.make(files, "grove/addon")
        compiled = lib.node_by_output(graph, "$(B)/grove/addon/body.cpp.pic.o")
        self.assertIn("-DSYSTEM_AFTER_HEADER", compiled["cmds"][0]["cmd_args"])

    def test_runtime_dependency_uses_only_shared_final_output(self):
        files = fixture("UDF_BASE(renamed)")
        files["tests/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(grove/addon)\nEND()\n"
        graph = lib.make(files, "tests", "-t", "--strip-packages-from-results")
        link = lib.node_by_output(graph, "$(B)/grove/addon/librenamed.so")
        suite = lib.node_by_output(graph, "$(B)/tests/test-results/exectest/meta.json")
        self.assertIn(link["uid"], suite["deps"])
        self.assertIn(link["uid"], graph["result"])

    def test_deprecated_header_exports_is_rejected(self):
        result = lib.make_process(fixture("UDF_BASE(renamed EXPORTS api.exports)"), "grove/addon")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("EXPORTS keyword in module header is deprecated", result.stderr)
        self.assertIn("EXPORTS_SCRIPT", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
