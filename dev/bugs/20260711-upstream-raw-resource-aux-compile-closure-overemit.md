# Upstream over-emit: raw resource producer carries the aux C++ compile closure

## Summary

Upstream ymake attaches the full C/C++ include closure of a generated
`*_raw.auxcpp` file to the preceding `rescompiler` producer node (`kv.p=PR`).
`rescompiler` only reads the resource payloads named in its command. The
headers are inputs of the later C++ compilation, not of resource generation.

This is the raw-resource counterpart of the Cython generator over-emit: a
generator owns the closure of the translation unit it produces. `ay` now
registers the generated aux source and lets the ordinary `emitCC` path resolve
that closure once. `dev dump normalize --ref-graph` removes the upstream
compile-only inputs from raw-aux PR nodes before comparison.

## Evidence

In the `devtools/ya/bin` reference graph, the PR node producing

`$(B)/contrib/libs/protobuf/builtin_proto/protos_from_protobuf/3620e98489806137a441f8f93b_raw.auxcpp`

has 903 normalized inputs. The command has 12 actual inputs: eleven generated
Python payloads and `tools/rescompiler/rescompiler`. Everything else belongs
to producer dependency closures or to the aux translation unit's compile
closure.

Typical excess inputs are libcxx `__algorithm/*`, glibc/musl headers,
`library/cpp/resource/*.h`, and `util/generic/*.h`. None occurs in the
`rescompiler` command. They do occur in the later CC node for the generated
aux source.

Before the refactor this also caused two scanner walks per raw aux source:
`registerPyProtoAuxSource`/the old `pyProtoAuxInputClosure` pre-scan followed
by the ordinary `emitCC` scan during source-queue draining.

## Status

Confirmed and normalized as of 2026-07-11. The production graph keeps the
compile closure only on the CC node; the raw PR node keeps only the resource
payloads and tool named by its command. The reference-only over-declaration is
filtered narrowly for `kv.p=PR` nodes whose output ends in `_raw.auxcpp`.
The filter derives inputs from the command protocol positionally: argument 0
is the tool, argument 1 is the output, and every following pair is
`payload-or-"-", key-or-inline-value`. It does not classify paths by extension.
