# Enum serialization: preserve upstream declared include closures

## Status

The reference-only compatibility filter is retired. Both graph normalization
modes preserve declared EN inputs and dependency edges. `--ref-graph` remains
accepted for command-line compatibility. The strict parity policy follows the
upstream graph algorithm, even when the generator executable reads fewer files.

The previous policy, introduced on 2026-07-11, reduced reference EN inputs to
the tool and positional header, then pruned some producer edges. That hid real
declared graph differences and must not be used to assess source fidelity.

## Declared graph algorithm

`build/ymake.core.conf:4596-4617` gives both enum variants the input header as
an `output_include` with `from_input`. The plain variant additionally declares
`util/generic/serialized_enum.h` as an output include; `WITH_HEADER` does not.

`devtools/ymake/model/action_graph.cpp:508-544` creates BuildFrom edges for
action inputs, retaining generated inputs as nonparsed files. Its output-include
handling at 427-455 and 634 attaches parsed includes to outputs, including both
outputs of `WITH_HEADER`. `model_producer/action_path_resolver.cpp:156-177`
resolves `from_input` immediately, while ordinary output includes are resolved
through the include processor. `include_processors/cpp_processor.cpp:70-86`
selects `h`/`h+cpp` induced includes for headers and `cpp`/`h+cpp` for C++.

`devtools/ymake/json_visitor.cpp:472-473,632-712` propagates included-input
sets along include edges and into action input sets along BuildFrom edges.
Generated inputs retain their actual producer and its primary output through
OutTogether relationships. `export_json.cpp:228-253,270-303` exports these
sets and real producer dependencies. EN therefore inherits the input header's
include closure, including nested includes and generated-header metadata.
The generated C++ output's own include closure belongs to compilation.

The earlier version of this note claimed that plain EN also inherited its own
future C++ output's runtime include closure. A sandboxed upstream probe on
2026-10-22, using the pinned codegen case flags and environment, disproved that
claim: both EN variants inherited only their input header closure. None of the
six examples in ROOT's raw 707-EN audit included `serialized_enum.h` in EN
inputs either. The plain generated C++ compilation did include that runtime
header and its nested include; the `WITH_HEADER` compilation did not.

The same tiny upstream probe established these generated-input cases for both
variants:

| Header producer | Additional declared EN inputs |
| --- | --- |
| `COPY_FILE` | None from the copied source's includes |
| `COPY_FILE_WITH_CONTEXT` | Source template and its nested include closure |
| `RUN_PROGRAM` with C++ primary output and secondary header | Producer primary output and header-domain induced include closure, excluding cpp-only induced includes |

A consumer of an EN secondary header also retained the EN primary C++ output
and header closure. Python binary CLI fixtures cover these rules with renamed
source trees, alongside the existing independent Alice enum generator cases.

## Executable reads

`tools/enum_parser/parse_enum/parse_enum.cpp` constructs `TFileInput(fileName)`,
reads that file, and parses the byte buffer. `tools/enum_parser/enum_parser/main.cpp`
passes positional argument 1 to the parser and writes generated C++ includes.
This observation explains why conservative declared inputs may exceed actual
reads; it does not authorize removing them from either graph.
