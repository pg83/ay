# Upstream over-emit: enum parser node carries C++ closures it does not read

## Summary

Upstream ymake attaches C/C++ include closures to `kv.p=EN` enum-serialization
generator nodes. In the plain `GENERATE_ENUM_SERIALIZATION` variant this
includes both the input header closure and the closure of the future
`*_serialized.cpp` output. In the `WITH_HEADER` variant it includes the input
header closure.

The `enum_parser` action reads neither closure. It opens exactly the header
path passed as positional argument 1, calls `ReadAll()`, parses that one byte
buffer, and writes C++ text. It does not invoke a preprocessor and does not
open the header's `#include` directives. The generated C++ includes are inputs
of the later compiler action.

## Evidence

`tools/enum_parser/parse_enum/parse_enum.cpp` implements
`TEnumParser::TEnumParser(const TString& fileName)` as:

1. `new TFileInput(fileName)`;
2. `contents = in->ReadAll()`;
3. `Parse(contents.data(), contents.size())`.

`tools/enum_parser/enum_parser/main.cpp` constructs that parser from
`freeArgs[0]`, then `WriteHeader()` emits `#include` lines into the generated
output. Those emitted headers are not opened by the generator.

## Status

Confirmed and normalized as of 2026-07-11. `ay` gives the EN node only the
tool and the direct input header. The generated source remains registered with
its parsed/induced includes, so the ordinary `emitCC` path owns and resolves
the generated C++ closure exactly once.

`dev dump normalize --ref-graph` filters EN inputs positionally from the
command protocol: argument 0 is the tool and argument 1 is the sole input
header. No path-extension classification is used.
