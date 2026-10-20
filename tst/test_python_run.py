import unittest

import lib


LIBRARY_HEAD = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def cmd(node):
    return node["cmds"][0]


class RunPython3Test(unittest.TestCase):
    def test_arguments_expand_quoted_configuration_values(self):
        files = {
            "build/ymake_conf.py": lib.ymake_conf(extra='SCRIPT_ARG="two words"\n'),
            "lib/ya.make": LIBRARY_HEAD + (
                "RUN_PYTHON3(gen.py $SCRIPT_ARG OUT first.o)\nEND()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib", "--sandboxing")
        run = lib.node_by_output(graph, "$(B)/lib/first.o")
        self.assertEqual(cmd(run)["cmd_args"][2:], ["two words"])

    def test_object_outputs_become_archive_members(self):
        files = {
            "lib/ya.make": LIBRARY_HEAD + "RUN_PYTHON3(gen.py OUT first.o second.o OUT_NOAUTO unused.o)\nEND()\n",
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/first.o")
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"][:2], ["$(B)/lib/first.o", "$(B)/lib/second.o"])
        self.assertIn(generated["uid"], archive["deps"])
        self.assertEqual(archive["deps"].count(generated["uid"]), 1)
        self.assertNotIn("$(B)/lib/unused.o", archive["inputs"])

    def test_outputs_follow_command_discovery_order(self):
        files = {
            "lib/ya.make": LIBRARY_HEAD + (
                "RUN_PYTHON3(gen.py third.o first.o "
                "OUT first.o second.o third.o)\nEND()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/first.o")
        expected = ["$(B)/lib/" + name for name in ("third.o", "first.o", "second.o")]
        self.assertEqual(run["outputs"], expected)
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["cmds"][0]["cmd_args"][-3:], expected)

    def test_compiler_and_resource_globals_in_arguments(self):
        files = {
            "lib/ya.make": LIBRARY_HEAD + (
                "DECLARE_EXTERNAL_RESOURCE(SDK sbr:123)\n"
                "RUN_PYTHON3(gen.py $CXX_COMPILER --sdk $SDK_RESOURCE_GLOBAL "
                "OUT first.o)\nEND()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/first.o")
        self.assertEqual(cmd(run)["cmd_args"][2:], [
            "$(B)/resources/CLANG20/bin/clang++", "--sdk", "$(B)/resources/SDK",
        ])
        lib.node_by_output(graph, "$(B)/resources/SDK")

    def test_sections_shape_command_env_and_outputs(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "RUN_PYTHON3(\n"
                "  gen.py ${ARCADIA_ROOT} --flag in.txt out=in.txt mode=fast gen.h -o\n"
                "  IN in.txt\n"
                "  OUT gen.h gen.cpp data.bin\n"
                "  OUT_NOAUTO extra.h\n"
                "  STDOUT log.cpp\n"
                "  ENV A=1 B\n"
                "  CWD ${ARCADIA_BUILD_ROOT}\n"
                "  OUTPUT_INCLUDES lib/dep.h ${ARCADIA_ROOT}/lib/dep2.h\n"
                ")\n"
                "SRCS(use.cpp)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
            "lib/in.txt": "",
            "lib/dep.h": "",
            "lib/dep2.h": "",
            "lib/use.cpp": '#include "lib/gen.h"\n#include "lib/extra.h"\nint u(){return 0;}\n',
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/gen.cpp")
        self.assertEqual(run["kv"]["p"], "PY")
        self.assertEqual(run["outputs"], [
            "$(B)/lib/log.cpp", "$(B)/lib/gen.h", "$(B)/lib/gen.cpp",
            "$(B)/lib/data.bin", "$(B)/lib/extra.h",
        ])
        self.assertEqual(cmd(run)["cmd_args"][1:], [
            "$(S)/lib/gen.py", "$(S)", "--flag", "$(S)/lib/in.txt",
            "out=$(S)/lib/in.txt", "mode=fast", "$(B)/lib/gen.h", "-o",
        ])
        self.assertEqual(cmd(run)["cwd"], "$(B)")
        self.assertEqual(cmd(run)["stdout"], "$(B)/lib/log.cpp")
        self.assertEqual(cmd(run)["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)", "A": "1", "B": ""})
        self.assertEqual(run["env"], cmd(run)["env"])
        for expected in ('$(S)/lib/gen.py', '$(S)/lib/in.txt',):
            self.assertIn(expected, run["inputs"])
        self.assertNotIn('$(S)/lib/dep.h', run['inputs'])
        self.assertNotIn('$(S)/lib/dep2.h', run['inputs'])

        for output in ("gen.cpp", "log.cpp"):
            compiled = lib.node_by_output(graph, f"$(B)/lib/{output}.o")
            self.assertIn(f"$(B)/lib/{output}", compiled["inputs"])
            self.assertIn("$(S)/lib/dep2.h", compiled["inputs"])
            self.assertIn(run["uid"], compiled["deps"])
        use = lib.node_by_output(graph, "$(B)/lib/use.cpp.o")
        for expected in ('$(B)/lib/gen.h', '$(B)/lib/extra.h', '$(S)/lib/dep.h',):
            self.assertIn(expected, use["inputs"])
        self.assertNotIn('$(S)/lib/in.txt', use['inputs'])
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"][:3], [
            "$(B)/lib/use.cpp.o", "$(B)/lib/gen.cpp.o", "$(B)/lib/log.cpp.o",
        ])

    def test_generated_script_and_stdout_noauto(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "COPY_FILE(script.txt script.py)\n"
                "RUN_PYTHON3(${BINDIR}/script.py STDOUT_NOAUTO quiet.cpp OUT data.cpp data.bin)\n"
                "END()\n"
            ),
            "lib/script.txt": "",
        }
        graph = lib.make(files, "lib")
        copy = lib.node_by_output(graph, "$(B)/lib/script.py")
        run = lib.node_by_output(graph, "$(B)/lib/data.cpp")
        self.assertEqual(run["outputs"], [
            "$(B)/lib/quiet.cpp", "$(B)/lib/data.cpp", "$(B)/lib/data.bin",
        ])
        self.assertEqual(cmd(run)["cmd_args"][1:], ["$(B)/lib/script.py"])
        self.assertEqual(cmd(run)["stdout"], "$(B)/lib/quiet.cpp")
        self.assertIn("$(B)/lib/script.py", run["inputs"])
        self.assertIn(copy["uid"], run["deps"])
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"][0], "$(B)/lib/data.cpp.o")
        self.assertNotIn("$(B)/lib/quiet.cpp.o", archive["inputs"])

    def test_split_outputs_include_generator_induced_sources(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "BASE_CODEGEN(tool proto)\n"
                "RUN_PYTHON3(split.py IN ${BINDIR}/proto.cpp OUT_NOAUTO shard0.cpp shard1.cpp shard.h)\n"
                "SRCS(shard0.cpp shard1.cpp)\n"
                "END()\n"
            ),
            "lib/split.py": "",
            "lib/proto.in": "",
            "lib/induced.h": "",
            "tool/ya.make": (
                "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "INDUCED_DEPS(cpp ${ARCADIA_ROOT}/lib/induced.h)\n"
                "SRCS(main.cpp)\n"
                "END()\n"
            ),
            "tool/main.cpp": "int main(){return 0;}\n",
        }
        graph = lib.make(files, "lib")
        codegen = lib.node_by_output(graph, "$(B)/lib/proto.cpp")
        run = lib.node_by_output(graph, "$(B)/lib/shard0.cpp")
        self.assertEqual(run["outputs"], [
            "$(B)/lib/shard0.cpp", "$(B)/lib/shard1.cpp", "$(B)/lib/shard.h",
        ])
        self.assertIn("$(B)/lib/proto.cpp", run["inputs"])
        self.assertNotIn("$(S)/lib/proto.in", run["inputs"])
        self.assertIn(codegen["uid"], run["deps"])
        first = lib.node_by_output(graph, "$(B)/lib/shard0.cpp.o")
        self.assertEqual(lib.bucketed(first["inputs"], 1), ['$(B)/lib/shard0.cpp', '$(S)/lib/induced.h'])
        second = lib.node_by_output(graph, "$(B)/lib/shard1.cpp.o")
        self.assertEqual(lib.bucketed(second["inputs"], 1), [
            '$(B)/lib/shard1.cpp',
            '$(B)/lib/shard0.cpp',
            '$(S)/lib/induced.h',
        ])

    def test_script_text_naming_own_output_is_not_an_include(self):
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "DECLARE_IN_DIRS(CONF *.toml DIRS conf)\n"
                "RUN_PYTHON3(\n"
                "  gen.py --header gen.h --cpp gen.cpp \\${CONF_FILES}\n"
                "  IN ${CONF_FILES}\n"
                "  OUT gen.h gen.cpp\n"
                "  OUTPUT_INCLUDES lib/dep.h\n"
                ")\n"
                "SRCS(gen.h)\n"
                "END()\n"
            ),
            "lib/gen.py": 'CPP = """\n#include "gen.h"\n"""\n',
            "lib/conf/a.toml": "",
            "lib/conf/b.toml": "",
            "lib/dep.h": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/gen.h")
        self.assertEqual(run["outputs"], ["$(B)/lib/gen.h", "$(B)/lib/gen.cpp"])
        self.assertEqual(cmd(run)["cmd_args"][1:], [
            "$(S)/lib/gen.py", "--header", "$(B)/lib/gen.h", "--cpp", "$(B)/lib/gen.cpp",
            "$(S)/lib/conf/a.toml", "$(S)/lib/conf/b.toml",
        ])
        self.assertEqual(run["deps"], [])
        self.assertEqual(sorted(set(run["inputs"])), [
            '$(S)/lib/conf/a.toml',
            '$(S)/lib/conf/b.toml',
            '$(S)/lib/gen.py',
        ])
        compiled = lib.node_by_output(graph, "$(B)/lib/gen.cpp.o")
        self.assertEqual(compiled["deps"], [run["uid"]])

    def test_inputs_with_empty_parser_extensions_are_not_scanned(self):
        extensions = ("fml", "fml2", "fml3", "pln", "info", "a", "lua", "sh", "bin", "py", "pyi", "pysrc")
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "RUN_PYTHON3(gen.py IN "
                + " ".join(f"in.{ext}" for ext in extensions)
                + " OUT gen.cpp)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
            "lib/dep.h": "",
        }
        for ext in extensions:
            files[f"lib/in.{ext}"] = '#include "lib/dep.h"\n'
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/gen.cpp")
        compiled = lib.node_by_output(graph, "$(B)/lib/gen.cpp.o")
        for ext in extensions:
            self.assertIn(f"$(S)/lib/in.{ext}", run["inputs"])
        self.assertNotIn("$(S)/lib/dep.h", run["inputs"])
        self.assertNotIn("$(S)/lib/dep.h", compiled["inputs"])

    def test_arcadia_root_left_by_expansion_limit_maps_to_source_root(self):
        # Each expansion pass consumes one "O1}" suffix, so the literal
        # ${ARCADIA_ROOT} only appears after the last of the eight passes.
        files = {
            "lib/ya.make": (
                LIBRARY_HEAD
                + "SET(O1 \\${)\n"
                "RUN_PYTHON3(gen.py ${O1}O1}O1}O1}O1}O1}O1}O1}ARCADIA_ROOT} OUT x.cpp)\n"
                "END()\n"
            ),
            "lib/gen.py": "",
        }
        graph = lib.make(files, "lib")
        run = lib.node_by_output(graph, "$(B)/lib/x.cpp")
        self.assertEqual(cmd(run)["cmd_args"][1:], ["$(S)/lib/gen.py", "$(S)"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
