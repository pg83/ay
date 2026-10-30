import base64
import json

from tst.make.lib import MakeTest, PROJECT


class AliceProtoIncludesTest(MakeTest):
    def request(self, sources, expected, ignored=(), directory="alice/proto", discovery=None):
        names = list(sources)
        self.write(directory + "/ya.make", "PROTO_LIBRARY()\nSRCS(" + " ".join(names) + ")\nEND()\n")
        for name, content in sources.items():
            self.write(directory + "/" + name, content)
        files = [f"${{ARCADIA_ROOT}}/{PROJECT}/{directory}/{name}" for name in names]
        macro = "ALICE_GENERATE_FUNCTION_PROTO_INCLUDES(" + " ".join(files + list(ignored)) + ")"
        if discovery is not None:
            macro = "ALICE_GENERATE_PROTO_INCLUDES(" + ("DIR " + discovery if discovery else "") + ")"
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nPEERDIR({PROJECT}/{directory})\n{macro}\nEND()\n")
        node = self.node(self.graph(), "proto_includes.cpp")
        commands = [command["cmd_args"] for command in node["cmds"]
                    if any("alice/functions/function_proto_converters/generator" in arg for arg in command["cmd_args"])]
        self.assertEqual(len(commands), 1, node["cmds"])
        args = commands[0]
        tool = next(index for index, arg in enumerate(args) if "alice/functions/function_proto_converters/generator" in arg)
        self.assertTrue(args[tool + 1].endswith("proto_includes.h"), args)
        payload = base64.b64decode(args[tool + 2]).decode()
        request = {(f"{directory}/{name[:-6]}.pb.h" if "alice/" in directory + "/" else "h"): messages
                   for name, messages in expected.items()}
        self.assertEqual(json.loads(payload), request)
        self.assertEqual(payload, json.dumps(request, ensure_ascii=False, sort_keys=True))

    def test_comments_and_top_level_messages(self):
        self.request({
            "comments.proto": '''// package ignored; message Commented {}
/* package wrong; message BlockCommented {} */
package e2e.answer;
/* first */ message Outer { message Nested {} } /* second */
message Second {} // message AtEnd {}''',
            "package_less.proto": "message First {}\nmessage Second {}\n",
            "unterminated.proto": "message Before {} /* unterminated message After {}",
        }, {
            "comments.proto": ["e2e::answer::Outer", "e2e::answer::Second"],
            "package_less.proto": ["First", "Second"],
            "unterminated.proto": ["Before", "After"],
        }, ignored=("not_a_proto.txt",))

    def test_proto_discovery_with_default_and_explicit_directory(self):
        for directory in ("", "alice/proto"):
            with self.subTest(directory=directory):
                self.request({"answer.proto": "package e2e; message Answer {}"},
                             {"answer.proto": ["e2e::Answer"]}, discovery=directory)

    def test_proto_without_alice_in_its_path(self):
        self.write("h", "#pragma once\n")
        self.request({"answer.proto": "package e2e; message Answer {}"},
                     {"answer.proto": ["e2e::Answer"]}, directory="proto")

    def test_generated_protos_are_rejected(self):
        self.write("source.proto", "message Answer {}\n")
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(source.proto generated.proto)\nALICE_GENERATE_FUNCTION_PROTO_INCLUDES(${BINDIR}/generated.proto)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_keyword_boundaries_and_incomplete_declarations(self):
        self.request({
            "keywords.proto": '''épackage wrong;
notpackage wrong;
packagewrong;
package  ;
émessage Ignored {}
notmessage Ignored {}
messageWithoutSpace {}
message -Invalid {}
message MissingBrace;
message Good { message Nested {} }
message EmptyName;
message''',
            "no_package.proto": "package ;\nmessage Answer {}\nmessage MissingBrace",
            "empty.proto": "// only a comment",
        }, {
            "keywords.proto": ["Good"],
            "no_package.proto": ["Answer"],
            "empty.proto": [],
        })

    def test_unicode_whitespace_and_message_names(self):
        self.request({
            "unicode.proto": "package\u2003e2e.unicode;\nmessage\vВерсия_1₂\x85{ message Nested {} }\nmessage\x1cSecond\u2003{}",
            "start.proto": "package e2e;\nmessage AtStart {}\n",
        }, {
            "unicode.proto": ["e2e::unicode::Версия_1₂", "e2e::unicode::Second"],
            "start.proto": ["e2e::AtStart"],
        })

    def test_request_json_preserves_unicode_separators_and_escapes(self):
        package = 'e2e\u2028line\u2029paragraph\\path"quoted\tspace'
        self.request({"escaped.proto": f"package {package};\nmessage Answer {{}}"}, {
            "escaped.proto": [package + "::Answer"],
        })
