"""Autocheck graph-generation protocol: the ya make invocation and the configure output Autocheck consumes.

Autocheck generates graphs on Distbuild with one node per partition. The node
runs ya make, saves the context and the graph, and exits 0 on configure errors.
The command, environment and output names follow that node as built by
devtools/autocheck/ggaas/builders/meta_graph2 for the pre_commit profile with
the platform of the p12 partition (dev/config.json, p12_full). The production
node built by devtools/ya/build/remote/remote_graph_generator.py passes the same
options through --raw-params-file and writes the same outputs.

From the context Autocheck consumes configure_errors and make_files:
contexts_difference_cpp subtracts the base revision and marks changed make
files "OK", and the build stage reports a configure entry per path.
"""

import json
import struct
from compression import zstd


ARGUMENTS = (
    "--dist", "-j", "0", "--no-src-links", "--build", "release", "--host-build-type", "release",
    "--ignore-configure-errors", "yes", "--ignore-nodes-exit-code", "--no-content-uids", "--no-prefetch",
    "--no-yt-store", "--skip-test-console-report", "--warning-mode", "dirloops;ChkPeers;allbadrecurses", "-x", "s",
    "-k", "--add-modules-to-results", "--strip-packages-from-results", "--output-only-tests",
    "--backup-test-results", "--report-skipped-suites", "--use-links-in-report", "--sandboxing", "--compress-ymake-output",
    "-DAUTOCHECK=yes", "-DUSE_PREBUILT_TOOLS=no",
    "--host-platform-flag", "AUTOCHECK=yes", "--host-platform-flag", "USE_PREBUILT_TOOLS=no",
    "--target-platform", "default-linux-x86_64", "--target-platform-build-type", "relwithdebinfo", "--target-platform-tests",
    "--target-platform-flag", "AUTOCHECK=yes", "--target-platform-flag", "DEBUGINFO_LINES_ONLY=yes",
    "--target-platform-flag", "ADD_PEERDIRS_GEN_TESTS=yes",
    "--test-size", "small", "--test-size", "medium", "--tests-retries", "1",
)

# The node also sets YA_CACHE_DIR and TMPDIR under its build root; the tests
# keep the shared ya cache so that tools are not downloaded for every run.
ENVIRONMENT = {
    "AUTOCHECK": "1",
    "YA_NO_YMAKE_RETRY": "1",
    "YA_NO_GEN_RENAMED_RESULTS": "1",
    "YA_STRIP_IDLE_BUILD_RESULTS": "0",
    "YA_TEST_DISABLE_FLAKE8_MIGRATIONS": "0",
    "YA_DONT_USE_TOKENS_IN_GRAPH": "1",
    "YA_USE_NEW_DISTBUILD_CLIENT": "1",
    "YA_CACHE_NAMESPACE": "AC",
    "YA_CACHE_TESTS": "1",
    "YA_ENABLE_TEST_NODES_SELF_UID": "1",
    "YA_TEST_REMOVE_RESULT_NODE": "1",
    "YA_SET_UPLOAD_NODES_TO_REMOTE_STORE": "1",
    "YA_ADD_CHANGED_OK_CONFIGURES": "1",
    "YA_MERGE_SPLIT_TESTS": "0",
    "YA_NO_LOGS": "1",
    "YA_NO_RESPAWN": "1",
}

OUTPUTS = {
    "--profile": "context.profile.json",
    "--stages": "context.stages.json",
    "--save-context-to": "context.json.uc",
    "--save-graph-to": "graph.json.uc",
    "--log-file": "generating.log.uc",
    "--evlog-file": "generating.evlog.zst",
    "--html-display": "output.html",
}

MAX_COMPRESSED_LEN = 100000000


def command(binary, targets, build_dir, output_dir, source_root):
    outputs = [item for flag, name in OUTPUTS.items() for item in (flag, str(output_dir / name))]
    return [
        binary, "--no-report", "-v", "make", *ARGUMENTS, *targets, "--build-dir", str(build_dir),
        *outputs, "--evlog-node-stat", "--source-root", str(source_root), "--dist-priority", "0",
    ]


def read_uc(path):
    """Decode a library/cpp/ucompress stream the way NUCompress::TDecodedInput does."""
    data = path.read_bytes()
    records = []
    pos = 0
    while pos < len(data):
        if pos + 4 > len(data):
            raise ValueError(f"{path}: broken stream: cannot read block length")
        (length,) = struct.unpack_from("<I", data, pos)
        pos += 4
        if length > MAX_COMPRESSED_LEN or pos + length > len(data):
            raise ValueError(f"{path}: broken stream")
        records.append(data[pos:pos + length])
        pos += length
    if not records:
        raise ValueError(f"{path}: empty stream")
    codec = json.loads(records[0]).get("codec")
    if not codec:
        raise ValueError(f"{path}: header does not have codec info")
    if not codec.startswith(("zstd_", "zstd08_")):
        raise ValueError(f"{path}: codec {codec} is not supported by the test reader")
    blocks = []
    for block in records[1:]:
        if len(block) < 8:
            raise ValueError(f"{path}: too small input")
        (size,) = struct.unpack_from("<Q", block)
        if size:
            decoded = zstd.decompress(block[8:])
            if len(decoded) != size:
                raise ValueError(f"{path}: decompressed {len(decoded)} bytes, expected {size}")
            blocks.append(decoded)
    return b"".join(blocks)


def configure_output(context, project, replacements):
    """Return the configure part of a saved context that Autocheck consumes, checking its shape.

    configure_errors maps a project directory to the sorted unique
    [message, row, column] triples of its errors. make_files lists every
    ya.make read with its OWNERS; only entries of the fixture project are kept,
    the rest is the same build closure for every fixture.
    """
    errors = context["configure_errors"]
    if not isinstance(errors, dict):
        raise ValueError("configure_errors is not a map")
    output = {}
    for path, triples in errors.items():
        if not isinstance(triples, list) or not triples:
            raise ValueError(f"configure_errors[{path!r}] is not a non-empty list: {triples!r}")
        for triple in triples:
            if not (isinstance(triple, list) and len(triple) == 3 and isinstance(triple[0], str)
                    and isinstance(triple[1], int) and isinstance(triple[2], int)):
                raise ValueError(f"configure_errors[{path!r}] has a malformed error: {triple!r}")
        if [tuple(triple) for triple in triples] != sorted({tuple(triple) for triple in triples}):
            raise ValueError(f"configure_errors[{path!r}] is not sorted and unique: {triples!r}")
        output[path] = [[_replace(triple[0], replacements), triple[1], triple[2]] for triple in triples]
    make_files = {}
    prefix = f"$S/{project}"
    for entry in context["make_files"]:
        if not isinstance(entry, dict) or not isinstance(entry.get("PATH"), str):
            raise ValueError(f"malformed make_files entry: {entry!r}")
        if entry["PATH"] == prefix or entry["PATH"].startswith(prefix + "/"):
            make_files[entry["PATH"]] = entry.get("OWNERS")
    return {"configure_errors": output, "make_files": make_files}


def _replace(text, replacements):
    for old, new in replacements:
        text = text.replace(old, new)
    return text
