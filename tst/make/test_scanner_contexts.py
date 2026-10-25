import os
from unittest.mock import patch

from tst.make.lib import MakeTest, PROJECT


class ScannerContextsTest(MakeTest):
    def test_proto_chunk_boundaries(self):
        self.write("ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto base.proto)\nEND()\n")
        self.write("base.proto", 'syntax = "proto2";\npackage e2e;\nmessage Base { optional int32 value = 1; }\n')
        for size in (1048550, 1048575, 2097200):
            with self.subTest(size=size):
                self.write("answer.proto", '// ' + 'x' * size + f'\r\nsyntax = "proto2";\r\npackage e2e;\r\nimport "{PROJECT}/base.proto";\r\nmessage Answer {{ optional Base value = 1; }}')
                self.assertIn(f"$(S)/{PROJECT}/base.proto", self.node(self.graph(), "answer.pb.cc")["inputs"])

    def test_long_terminal_proto_comment(self):
        self.write("ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2";\nmessage Answer {}\n//' + 'x' * 2200000)
        self.node(self.graph(), "answer.pb.cc")

    def test_interning_and_hash_collisions(self):
        self.write("ya.make", f"PROGRAM(answer)\nADDINCL({PROJECT}/include)\nSRCS(main.cpp second.cpp)\nRESOURCE(value.txt /value)\nEND()\n")
        self.write("main.cpp", '#include "value.h"\nint main() { return VALUE; }\n')
        self.write("second.cpp", '#include "value.h"\nint second() { return VALUE; }\n')
        self.write("include/value.h", "#define VALUE 42\n")
        self.write("value.txt", "42\n")
        for chaos in ("intern-shuffle=0", "hash-source-under-slot=0 hash-bucket-h1=0 hash-bucket-h2=0", "table-hint=2 bump-chunk-bytes=512"):
            with self.subTest(chaos=chaos), patch.dict(os.environ, {"AY_CHAOS": chaos}):
                self.node(self.graph(), "answer")

    def test_keep_going_with_bad_peer(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/broken)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("broken/ya.make", "LIBRARY()\nRUN_PROGRAM()\nEND()\n")
        with patch.dict(os.environ, {"AY_DEBUG_OWNERSHIP": "1"}):
            result = self.make("-k")
        self.assertIn("RUN_PROGRAM", result.stderr)
