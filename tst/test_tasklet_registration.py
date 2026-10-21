import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
RUNTIME_HEADERS = [
    "tasklet/v1/runtime/lib/" + name
    for name in ("cpp_wrapper.h", "go_wrapper.h", "py_wrapper.h", "js_wrapper.h", "registry.h")
]


def fixture(body):
    files = {
        "mod/ya.make": "LIBRARY()\n" + BARE + body + "END()\n",
        "build/scripts/gen_tasklet_reg.py": "",
        "common/api.hpp": '#include "detail.hpp"\n',
        "common/detail.hpp": "",
        "contrib/libs/python/ya.make": "LIBRARY()\n" + BARE + "END()\n",
    }
    for directory, marker in (("tasklet/v1/domain", "DOMAIN"), ("sandbox/bin", "BIN"),
                              ("sandbox/taskbox/worker", "WORKER"), ("tasklet/v1/runtime/js", "JS")):
        files[directory + "/ya.make"] = "LIBRARY()\n" + BARE + "CFLAGS(GLOBAL -D" + marker + ")\nEND()\n"
    for header in RUNTIME_HEADERS:
        files[header] = ""
    return files


class TaskletRegistrationTest(unittest.TestCase):
    def test_python_library_registration_is_compiled_into_its_global_archive(self):
        files = fixture("TASKLET_REG(Task py sample.module:Factory common/api.hpp)\n")
        files["mod/ya.make"] = files["mod/ya.make"].replace("LIBRARY()", "PY3_LIBRARY()", 1)
        graph = lib.make(files, "mod")
        generated = lib.node_by_output(graph, "$(B)/mod/Task.task.cpp")
        compiled = lib.node_by_output(graph, "$(B)/mod/Task.task.cpp.o")
        archive = lib.node_by_output(graph, "$(B)/mod/libpy3mod.global.a")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(compiled["uid"], archive["deps"])

    def test_registration_emits_global_cpp_with_declared_runtime_includes(self):
        for language, extended in (("cpp", False), ("py", False), ("go", False), ("js", False),
                                   ("java", True), ("js", True)):
            with self.subTest(language=language, extended=extended):
                macro = "TASKLET_REG_EXT" if extended else "TASKLET_REG"
                wrapper = " CustomWrapper" if extended else ""
                graph = lib.make(fixture(f"{macro}(Task {language} module.Impl{wrapper} common/api.hpp)\n"), "mod")
                output = "$(B)/mod/Task.task.cpp"
                producer = lib.node_by_output(graph, output)
                self.assertEqual(producer["kv"], {"p": "TT", "pc": "yellow"})
                expected = ["", "$(S)/build/scripts/gen_tasklet_reg.py", "Task", "-l", language, "-i", "module.Impl"]
                if extended:
                    expected += ["-w", "CustomWrapper"]
                self.assertEqual(producer["cmds"][0]["cmd_args"], expected + [output, "common/api.hpp"])
                self.assertEqual(producer["inputs"], ["$(S)/build/scripts/gen_tasklet_reg.py"])
                compiled = lib.node_by_output(graph, output + ".o")
                self.assertEqual(set(compiled["inputs"]), {
                    output, "$(S)/common/api.hpp", "$(S)/common/detail.hpp",
                    *("$(S)/" + header for header in RUNTIME_HEADERS),
                })
                self.assertIn(producer["uid"], compiled["deps"])
                args = compiled["cmds"][0]["cmd_args"]
                markers = ["-DDOMAIN", "-DBIN", "-DWORKER"]
                if language == "js" and not extended:
                    markers += ["-DJS"]
                self.assertEqual([a for a in args if a in ("-DDOMAIN", "-DBIN", "-DWORKER", "-DJS")], markers)
                archive = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
                self.assertIn(compiled["uid"], archive["deps"])
                self.assertIn(output + ".o", archive["inputs"])

    def test_arguments_expand_and_repeated_includes_keep_command_multiplicity(self):
        graph = lib.make(fixture(
            "SET(REG_NAME Task)\nSET(REG_LANG py)\nSET(REG_IMPL sample.module:Factory)\n"
            "TASKLET_REG(${REG_NAME} ${REG_LANG} ${REG_IMPL} common/api.hpp common/api.hpp)\n"), "mod")
        producer = lib.node_by_output(graph, "$(B)/mod/Task.task.cpp")
        self.assertEqual(producer["cmds"][0]["cmd_args"][-2:], ["common/api.hpp", "common/api.hpp"])
        self.assertIn("sample.module:Factory", producer["cmds"][0]["cmd_args"])
        compiled = lib.node_by_output(graph, "$(B)/mod/Task.task.cpp.o")
        self.assertEqual(compiled["inputs"].count("$(S)/common/api.hpp"), 1)

    def test_non_js_and_extended_js_do_not_add_js_runtime(self):
        for statement in ("TASKLET_REG(Task py sample.Impl)", "TASKLET_REG_EXT(Task js sample.Impl Wrapper)"):
            with self.subTest(statement=statement):
                files = fixture(statement + "\n")
                del files["tasklet/v1/runtime/js/ya.make"]
                graph = lib.make(files, "mod")
                lib.node_by_output(graph, "$(B)/mod/Task.task.cpp.o")

    def test_registration_requires_its_positional_arguments(self):
        for statement in ("TASKLET_REG()", "TASKLET_REG(Task)", "TASKLET_REG(Task py)",
                          "TASKLET_REG_EXT(Task py Impl)"):
            with self.subTest(statement=statement):
                result = lib.make_process(fixture(statement + "\n"), "mod")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("expects", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
