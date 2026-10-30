from tst.make.lib import MakeTest, PROJECT


class RosVariantsTest(MakeTest):
    def test_single_letter_types_and_numeric_enum_fields(self):
        names = ("A", "O", "O0")
        declarations = ["enum Mode0 0", "enum 0Mode 0", "Mode0 local", "Optional<0Mode> optional_local"]
        for index, name in enumerate(names):
            for whitespace in ("\t", "\f", " "):
                declarations.append(f"{name}{whitespace}field{index}")
                declarations.append(f"{name}[]{whitespace}0field{index}")
            self.write(name + ".msg", "int32 value\n")
        declarations.append("Optional<int32> \t@")
        files = " ".join(name + ".msg" for name in names)
        self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(Answer.msg {files})\nEND()\n")
        self.write("Answer.msg", "\n".join(declarations) + "\n")
        graph = self.graph()
        generated = next(node for node in graph["graph"] if f"$(B)/{PROJECT}/Answer.h" in node["outputs"])
        messages = {path for path in generated["inputs"] if path.startswith(f"$(S)/{PROJECT}/") and path.endswith(".msg")}
        self.assertEqual(messages, {f"$(S)/{PROJECT}/{name}.msg" for name in (*names, "Answer")})

    def test_local_enum_names_values_and_internal_whitespace(self):
        declarations = []
        for index, name in enumerate(("0Mode", "_Mode", "AMode", "aMode")):
            for whitespace in ("\t\t\f\r ", "\f\f\t ", "  \t\f\r "):
                declarations.append(f"enum{whitespace}{name}{whitespace}{index}")
                declarations.append(f"{name}{whitespace}field{index}")
        declarations.extend(("enum Invalid @", "enum\t\t?", "enum Broken VALUE extra"))
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(Answer.msg)\nEND()\n")
        self.write("Answer.msg", "\n".join(declarations) + "\nint32 answer\n")
        graph = self.graph()
        generated = next(node for node in graph["graph"] if f"$(B)/{PROJECT}/Answer.h" in node["outputs"])
        messages = {path for path in generated["inputs"] if path.startswith(f"$(S)/{PROJECT}/") and path.endswith(".msg")}
        self.assertEqual(messages, {f"$(S)/{PROJECT}/Answer.msg"})

    def test_optional_type_prefixes_and_field_whitespace(self):
        names = ("OX", "OpX", "OptX", "OptiX", "OptioX", "OptionX", "OptionaX", "OptionalX")
        declarations = []
        for index, name in enumerate(names):
            whitespace = "\t\t\f\r " if index % 2 else "\f\f\t "
            declarations.extend((f"{name}{whitespace}value{index}",
                                 f"Optional<{name}[]>[23]{whitespace}_value{index}"))
            self.write(name + ".msg", "int32 value\n")
        declarations.extend(("Optional<int32>\t\t\f\r 0value", "Optional<int32>[2]\f\f\t _value0"))
        files = " ".join(name + ".msg" for name in names)
        self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(Answer.msg Unused.msg {files})\nEND()\n")
        self.write("Answer.msg", "\n".join(declarations) + "\n")
        self.write("Unused.msg", "int32 value\n")
        graph = self.graph()
        generated = next(node for node in graph["graph"] if f"$(B)/{PROJECT}/Answer.h" in node["outputs"])
        for name in names:
            self.assertIn(f"$(S)/{PROJECT}/{name}.msg", generated["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/Unused.msg", generated["inputs"])

    def test_optional_array_fields_and_local_enums(self):
        fields = (
            "Dep0 value", "Dep1[] values", "Dep2[12] values",
            "Optional<Dep3> value", "Optional<Dep4[]> values", "Optional<Dep5[12]> values",
            "Optional<Dep6>[] values", "Optional<Dep7>[12] values", "Optional<Dep8[12]>[34] values",
        )
        names = [f"Dep{index}.msg" for index in range(len(fields))]
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(Answer.msg Mode.msg " + " ".join(names) + ")\nEND()\n")
        self.write("Answer.msg", "enum Mode VALUE\nMode mode\n" + "\n".join(fields) + "\n")
        self.write("Mode.msg", "int32 value\n")
        for name in names:
            self.write(name, "int32 value\n")
        graph = self.graph()
        generated = next(node for node in graph["graph"] if any(path.endswith("/Answer.h") for path in node["outputs"]))
        for name in names:
            with self.subTest(dependency=name):
                self.assertIn(f"$(S)/{PROJECT}/{name}", generated["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/Mode.msg", generated["inputs"])

    def test_truncated_optional_fields_and_enum_declarations(self):
        samples = ("Optional<int32[12]>[34] field", "enum Mode VALUE", "int32[123] value")
        fragments = [text[:end] for text in samples for end in range(len(text) + 1)]
        fragments.extend(("Optional<", "Optional<>", "Optional<int32[>", "Optional<int32>[] missing extra", "enum Mode VALUE extra", "enum Mode-Value", "int32[abc] field"))
        fragments.extend(f"int32{space}field" for space in ("\t", "\r", "\f"))
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(Answer.msg)\nEND()\n")
        self.write("Answer.msg", "\n".join(fragments) + "\nint32 answer # trailing comment")
        graph = self.graph()
        generated = next(node for node in graph["graph"] if any(path.endswith("/Answer.h") for path in node["outputs"]))
        self.assertIn(f"$(S)/{PROJECT}/Answer.msg", generated["inputs"])
        self.assertFalse(any(path.endswith(".msg") and path != f"$(S)/{PROJECT}/Answer.msg" for path in generated["inputs"]))

    def test_languages_and_services(self):
        self.write("Answer.msg", "int32 value\n")
        self.write("Get.srv", "int32 question\n---\nint32 answer\n")
        for tag in ("CPP_ROS", "CPP_ROS PY3_CPP_ROS", "CPP_ROS PY3_CPP_ROS PY3_ROS", "GO_ROS"):
            with self.subTest(tag=tag):
                self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS({tag})\nROS_SRCS(Answer.msg Get.srv)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/Answer.msg" in node["inputs"] for node in graph["graph"]))

    def test_zero_copy(self):
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(ZERO_COPY Answer.msg)\nEND()\n")
        self.write("Answer.msg", "int32 value\n")
        graph = self.graph()
        self.assertTrue(any(path.endswith("Answer_zero_copy.h") for node in graph["graph"] for path in node["outputs"]))

    def test_partial(self):
        self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_DIR({PROJECT}/messages)\nROS_PARTIAL(e2e_part)\nROS_SRCS(messages/Answer.msg)\nEND()\n")
        self.write("messages/Answer.msg", "int32 value\n")
        self.graph()

    def test_pure_python(self):
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(PY3_ROS)\nENABLE(ROS_PURE_PY)\nROS_SRCS(Answer.msg Get.srv)\nEND()\n")
        self.write("Answer.msg", "int32 value\n")
        self.write("Get.srv", "int32 question\n---\nint32 answer\n")
        graph = self.graph()
        self.assertTrue(any(path.endswith("msg/__init__.py") for node in graph["graph"] for path in node["outputs"]))
