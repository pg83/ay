import unittest

from tst import lib
from tst.make.test_python_srcs import python_base


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
    def test_uppercase_values_preserve_the_registration_producer_contract(self):
        # ymake.core.conf:5417-5436 has positional values and a variadic include
        # list, with no service keywords in either registration macro.
        for extended in (False, True):
            for expanded in (False, True):
                with self.subTest(extended=extended, expanded=expanded):
                    macro = "TASKLET_REG_EXT" if extended else "TASKLET_REG"
                    values = ["RENAMED_REGISTRATION", "java" if extended else "cpp", "RENAMED_IMPLEMENTATION"]
                    if extended:
                        values.append("RENAMED_WRAPPER")
                    values += ["RENAMED_HEADER", "RENAMED_HEADER"]
                    body = ""
                    args = values
                    if expanded:
                        body = "".join(f"SET(ARG_{i} {value})\n" for i, value in enumerate(values))
                        args = [f"${{ARG_{i}}}" for i in range(len(values))]
                    files = fixture(body + f"{macro}({' '.join(args)})\n")
                    files["RENAMED_HEADER"] = '#include "common/detail.hpp"\n'
                    graph = lib.make(files, "mod")
                    output = "$(B)/mod/RENAMED_REGISTRATION.task.cpp"
                    producer = lib.node_by_output(graph, output)
                    command = ["", "$(S)/build/scripts/gen_tasklet_reg.py", values[0], "-l", values[1], "-i", values[2]]
                    if extended:
                        command += ["-w", values[3]]
                    self.assertEqual(producer["cmds"][0]["cmd_args"], command + [output, *values[-2:]])
                    self.assertEqual(producer["outputs"], [output])
                    self.assertEqual(producer["inputs"], ["$(S)/build/scripts/gen_tasklet_reg.py"])
                    self.assertEqual(producer["kv"], {"p": "TT", "pc": "yellow"})
                    compiled = lib.node_by_output(graph, output + ".o")
                    self.assertEqual(set(compiled["inputs"]), {
                        output, "$(S)/RENAMED_HEADER", "$(S)/common/detail.hpp",
                        *("$(S)/" + header for header in RUNTIME_HEADERS),
                    })
                    self.assertIn(producer["uid"], compiled["deps"])
                    archive = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
                    self.assertIn(output + ".o", archive["inputs"])
                    self.assertIn(compiled["uid"], archive["deps"])

    def test_uppercase_generated_include_uses_its_registered_producer(self):
        files = fixture(
            "RUN_PYTHON3(build/scripts/render_contract.py OUT_NOAUTO RENAMED_CONTRACT "
            "OUTPUT_INCLUDES common/detail.hpp)\n"
            "TASKLET_REG(RENAMED_GENERATED cpp RENAMED_FACTORY mod/RENAMED_CONTRACT)\n")
        files["build/scripts/render_contract.py"] = ""
        graph = lib.make(files, "mod")
        header = lib.node_by_output(graph, "$(B)/mod/RENAMED_CONTRACT")
        producer = lib.node_by_output(graph, "$(B)/mod/RENAMED_GENERATED.task.cpp")
        compiled = lib.node_by_output(graph, "$(B)/mod/RENAMED_GENERATED.task.cpp.o")
        self.assertEqual(producer["inputs"], ["$(S)/build/scripts/gen_tasklet_reg.py"])
        self.assertEqual(producer["cmds"][0]["cmd_args"][-1], "mod/RENAMED_CONTRACT")
        self.assertIn(header["uid"], compiled["deps"])
        self.assertIn(producer["uid"], compiled["deps"])
        self.assertIn("$(B)/mod/RENAMED_CONTRACT", compiled["inputs"])
        self.assertIn("$(S)/common/detail.hpp", compiled["inputs"])

    def test_registration_values_do_not_disable_other_macro_validation(self):
        for macro, wrapper in (("TASKLET_REG", ""), ("TASKLET_REG_EXT", " RenamedWrapper")):
            with self.subTest(macro=macro):
                result = lib.make_process(fixture(
                    f"{macro}(RENAMED_UNKNOWN_OPTION cpp RenamedFactory{wrapper})\n"
                    "ALLOCATOR(RENAMED_UNKNOWN_OPTION)\n"), "mod")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('macro ALLOCATOR received service-keyword "RENAMED_UNKNOWN_OPTION"', result.stderr)

    def test_python_library_registration_is_compiled_into_its_global_archive(self):
        files = fixture("TASKLET_REG(RENAMED_PY_TASK py sample.module:Factory common/api.hpp)\n")
        files["mod/ya.make"] = files["mod/ya.make"].replace("LIBRARY()", "PY3_LIBRARY()", 1)
        graph = lib.make(files, "mod")
        generated = lib.node_by_output(graph, "$(B)/mod/RENAMED_PY_TASK.task.cpp")
        compiled = lib.node_by_output(graph, "$(B)/mod/RENAMED_PY_TASK.task.cpp.o")
        archive = lib.node_by_output(graph, "$(B)/mod/libpy3mod.global.a")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(compiled["uid"], archive["deps"])

    def test_python_resources_and_registration_keep_statement_order(self):
        # ymake TASKLET_REG expands to SRCS(GLOBAL ...); both statement orders
        # must retain their position relative to Python embedding objects.
        for task_first in (False, True, None):
            with self.subTest(task_first=task_first):
                statements = ["PY_SRCS(__init__.py)\n",
                              "TASKLET_REG(Task py sample.module:Factory common/api.hpp)\n"]
                if task_first:
                    statements.reverse()
                if task_first is None:
                    statements.append("PY_SRCS(extra.py)\n")
                files = python_base()
                files.update(fixture("".join(statements)))
                files["mod/ya.make"] = files["mod/ya.make"].replace("LIBRARY()", "PY3_LIBRARY()", 1)
                files["mod/__init__.py"] = ""
                files["mod/extra.py"] = ""
                graph = lib.make(files, "mod")
                producer = lib.node_by_output(graph, "$(B)/mod/Task.task.cpp")
                compiled = lib.node_by_output(graph, "$(B)/mod/Task.task.cpp.o")
                archive = lib.node_by_output(graph, "$(B)/mod/libpy3mod.global.a")
                self.assertIn(producer["uid"], compiled["deps"])
                self.assertIn(compiled["uid"], archive["deps"])
                objects = [arg for arg in archive["cmds"][0]["cmd_args"] if arg.endswith(".o")]
                self.assertEqual(len(objects), 5 if task_first is None else 3)
                self.assertEqual(objects.index("$(B)/mod/Task.task.cpp.o"), 0 if task_first else 2)
                if task_first is None:
                    self.assertIn("$(S)/mod/__init__.py", lib.node_by_output(graph, objects[1])["inputs"])
                    self.assertIn("$(S)/mod/extra.py", lib.node_by_output(graph, objects[4])["inputs"])
                for output in objects:
                    node = lib.node_by_output(graph, output)
                    self.assertIn(output, archive["inputs"])
                    self.assertIn(node["uid"], archive["deps"])

    def test_registrations_keep_their_order_around_python_resources(self):
        for before in (False, True):
            with self.subTest(before=before):
                registrations = ("TASKLET_REG(First py sample.module:First)\n"
                                 "TASKLET_REG(Second py sample.module:Second)\n")
                python = "PY_SRCS(__init__.py)\n"
                files = python_base()
                files.update(fixture(registrations + python if before else python + registrations))
                files["mod/ya.make"] = files["mod/ya.make"].replace("LIBRARY()", "PY3_LIBRARY()", 1)
                files["mod/__init__.py"] = ""
                graph = lib.make(files, "mod")
                archive = lib.node_by_output(graph, "$(B)/mod/libpy3mod.global.a")
                objects = [arg for arg in archive["cmds"][0]["cmd_args"] if arg.endswith(".o")]
                self.assertEqual(objects[:2] if before else objects[-2:],
                                 ["$(B)/mod/First.task.cpp.o", "$(B)/mod/Second.task.cpp.o"])

    def test_resource_between_python_groups_keeps_its_ready_position(self):
        files = python_base()
        files.update(fixture("PY_SRCS(__init__.py)\nRESOURCE(- marker=value)\n"
                             "TASKLET_REG(Task py sample.module:Factory)\nPY_SRCS(extra.py)\n"))
        files["mod/ya.make"] = files["mod/ya.make"].replace("LIBRARY()", "PY3_LIBRARY()", 1)
        files["mod/__init__.py"] = files["mod/extra.py"] = ""
        graph = lib.make(files, "mod")
        archive = lib.node_by_output(graph, "$(B)/mod/libpy3mod.global.a")
        objects = [arg for arg in archive["cmds"][0]["cmd_args"] if arg.endswith(".o")]
        self.assertEqual(len(objects), 6)
        self.assertEqual(objects[3], "$(B)/mod/Task.task.cpp.o")
        self.assertIn("marker=value", lib.node_by_output(graph, objects[0])["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/mod/__init__.py", lib.node_by_output(graph, objects[2])["inputs"])
        self.assertIn("$(S)/mod/extra.py", lib.node_by_output(graph, objects[5])["inputs"])

    def test_registration_without_python_sources_keeps_resource_archive_order(self):
        files = python_base()
        files.update(fixture("TASKLET_REG(Task py sample.module:Factory)\nRESOURCE(- marker=value)\n"))
        files["mod/ya.make"] = files["mod/ya.make"].replace("LIBRARY()", "PY3_LIBRARY()", 1)
        graph = lib.make(files, "mod")
        archive = lib.node_by_output(graph, "$(B)/mod/libpy3mod.global.a")
        objects = [arg for arg in archive["cmds"][0]["cmd_args"] if arg.endswith(".o")]
        self.assertEqual(len(objects), 2)
        self.assertEqual(objects[1], "$(B)/mod/Task.task.cpp.o")
        self.assertIn("marker=value", lib.node_by_output(graph, objects[0])["cmds"][0]["cmd_args"])
        for output in objects:
            self.assertIn(output, archive["inputs"])
            self.assertIn(lib.node_by_output(graph, output)["uid"], archive["deps"])

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
