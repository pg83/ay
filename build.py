import ast
import base64
import functools
import hashlib
import json
import re
import uuid
from pathlib import Path

import build


ROOT = Path(__file__).parent


def touch(path):
    return [
        "python3",
        "-c",
        f"from pathlib import Path; p=Path(r'{path}'); p.parent.mkdir(parents=True, exist_ok=True); p.touch()",
    ]


def mkdir(path):
    return [
        "python3",
        "-c",
        f"from pathlib import Path; Path(r'{path}').mkdir(parents=True, exist_ok=True)",
    ]


def slug(value):
    result = re.sub(r"[^A-Za-z0-9_]+", "_", value).strip("_")
    if not result:
        raise RuntimeError(f"cannot make target name from {value!r}")
    return result


build.flags.allow({
    "arcadia": {
        "descr": "existing Arcadia tree required by ay_e2e and ya_e2e",
        "default": "",
    },
    "group": {
        "descr": "zero-based validation shard to include",
        "default": "",
    },
    "group_count": {
        "descr": "total number of validation shards",
        "default": "",
    },
    "coverage": {
        "descr": "build ay with -cover and merge unit coverage into $(B)/coverage/cover.out",
        "default": "",
    },
    "race": {
        "descr": "build ay with the Go race detector; run with `./build -Drace unit`",
        "default": "",
    },
})


def validation_partition():
    group = build.flags.group
    count = build.flags.group_count
    if bool(group) != bool(count):
        raise RuntimeError("-Dgroup and -Dgroup_count must be specified together")
    if not group:
        return None
    try:
        index = int(group)
        total = int(count)
    except ValueError as error:
        raise RuntimeError("validation shard values must be integers") from error
    if total <= 0 or index < 0 or index >= total:
        raise RuntimeError("validation shard requires 0 <= group < group_count")
    return index, total


partition = validation_partition()
coverage_enabled = bool(build.flags.coverage)
race_enabled = bool(build.flags.race)
COVERAGE_MINIMUM = "95"

GO_SOURCES = build.glob("$(S)/*.go")

GENERATED_DENSE_MAPS = [
    "$(B)/generated/go/dense_map_2.go",
]

dense_maps = command(
    name="dense_maps",
    inputs=["$(S)/dev/gen_densemap.py"],
    outputs=GENERATED_DENSE_MAPS,
    cmd=[
        "python3", "$(S)/dev/gen_densemap.py",
        "--out-dir", "$(B)/generated/go",
    ],
    descr="GS",
    color="magenta",
)

GO_MODULE_FILES = [
    *GO_SOURCES,
    *build.glob("$(S)/*.s"),
    "$(S)/go.mod",
    "$(S)/go.sum",
    "$(S)/perf_darts_data.txt",
]

GO_INPUTS = [
    *GO_MODULE_FILES,
    *GENERATED_DENSE_MAPS,
    "$(S)/dev/go_overlay.py",
    "$(S)/.gitignore",
    "$(S)/LICENSE",
    "$(S)/PROMPTS.md",
    "$(S)/STYLE.md",
    "$(S)/acceptance",
]

GO_OVERLAY = "$(B)/go-overlay.json"
GO_OVERLAY_CMD = [
    "python3", "$(S)/dev/go_overlay.py",
    "--output", GO_OVERLAY,
    "--source-root", "$(S)",
    *GENERATED_DENSE_MAPS,
]

GO_ENV = {
    "CGO_ENABLED": "1" if race_enabled else "0",
    "GOFLAGS": "-buildvcs=false",
    "GOTOOLCHAIN": "local",
    "GOWORK": "off",
}

# The unit tests run a binary built with the fault seam (chaos_on.go), which
# AY_CHAOS drives; the production binary compiles the seam away.
TEST_TAGS = ["aychaos", *(["aycoverage"] if coverage_enabled else [])]


def go_build_tail(output, tags):
    return [
        *(["-race"] if race_enabled else []),
        *(["-tags", ",".join(tags)] if tags else []),
        "-trimpath", "-buildvcs=false", "-o", output, ".",
    ]


def go_overlay_build(output, tags):
    return [
        GO_OVERLAY_CMD,
        ["go", "build", "-overlay=" + GO_OVERLAY, *go_build_tail(output, tags)],
    ]


def go_binary(name, output, cmd):
    return command(
        name=name,
        inputs=GO_INPUTS,
        outputs=[output],
        deps=[dense_maps],
        cmd=cmd,
        cwd="$(S)",
        env=GO_ENV,
        descr="GO",
        color="cyan",
    )


ay = go_binary("ay", "$(B)/bin/ay", go_overlay_build("$(B)/bin/ay", []))

if coverage_enabled:
    # The cover tool opens sources by their original names and ignores
    # -overlay, so the instrumented build compiles a staged copy of the module.
    GO_STAGED_MODULE = "$(B)/go-src"
    ay_test_cmd = [
        mkdir(GO_STAGED_MODULE),
        ["cp", "--", *GO_MODULE_FILES, *GENERATED_DENSE_MAPS, GO_STAGED_MODULE + "/"],
        ["go", "build", "-C", GO_STAGED_MODULE, "-cover", "-covermode=atomic",
         *go_build_tail("$(B)/bin/ay-test", TEST_TAGS)],
    ]
else:
    ay_test_cmd = go_overlay_build("$(B)/bin/ay-test", TEST_TAGS)

ay_test = go_binary("ay_test", "$(B)/bin/ay-test", ay_test_cmd)

# go vet needs the generated dense maps, so it runs inside the build graph.
vet_stamp = "$(B)/tests/vet.stamp"
vet = command(
    name="vet",
    inputs=GO_INPUTS,
    outputs=[vet_stamp],
    deps=[dense_maps],
    cmd=[
        GO_OVERLAY_CMD,
        ["go", "vet", "-overlay=" + GO_OVERLAY, "."],
        ["go", "vet", "-overlay=" + GO_OVERLAY, "-tags", "aychaos,aycoverage", "."],
        touch(vet_stamp),
    ],
    cwd="$(S)",
    env=GO_ENV,
    descr="VT",
    color="green",
)

python_test_stamp = "$(B)/tests/python.stamp"
python_test = command(
    name="python_test",
    inputs=[
        "$(S)/acceptance",
        "$(S)/dev/config.json",
        "$(S)/dev/HISTORY.md",
        "$(S)/dev/TEXT.md",
        *build.glob("$(S)/dev/*.py"),
    ],
    outputs=[python_test_stamp],
    cmd=[
        ["python3", "-m", "unittest", "discover", "-s", "dev", "-p", "*_test.py"],
        touch(python_test_stamp),
    ],
    cwd="$(S)",
    descr="PY",
    color="green",
)


binary_tests = []
coverage_archives = []
python_sources = set(build.glob("$(S)/tst/**/*.py"))


@functools.cache
def python_imports(path):
    syntax = ast.parse((ROOT / path.removeprefix("$(S)/")).read_text())
    modules = [path.removeprefix("$(S)/").rsplit("/", 1)[0].replace("/", ".")]
    for node in ast.walk(syntax):
        if isinstance(node, ast.Import):
            modules.extend(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module:
            modules.append(node.module)
            modules.extend(f"{node.module}.{alias.name}" for alias in node.names)
    paths = set()
    for module in modules:
        parts = module.split(".")
        paths.add("$(S)/" + "/".join(parts) + ".py")
        paths.update("$(S)/" + "/".join(parts[:i]) + "/__init__.py"
                     for i in range(1, len(parts) + 1))
    return paths & python_sources


def python_test_inputs(path):
    pending = [path, "$(S)/tst/lib.py"]
    seen = set()
    while pending:
        source = pending.pop()
        if source not in seen:
            seen.add(source)
            pending.extend(python_imports(source))
    return sorted(seen)

for test_path in build.glob("$(S)/tst/**/test_*.py"):
    if test_path.startswith("$(S)/tst/make/"):
        continue
    test_name = test_path.rsplit("/", 1)[-1][len("test_"):-len(".py")]
    test_slug = slug(test_name)
    test_module = test_path.removeprefix("$(S)/").removesuffix(".py").replace("/", ".")
    test_stamp = f"$(B)/tests/{test_slug}.stamp"
    test_inputs = python_test_inputs(test_path)
    test_commands = [["python3", "-m", test_module], touch(test_stamp)]
    test_outputs = [test_stamp]
    test_env = {
        "AY_TEST_BINARY": ay_test.outputs[0],
        "AY_TEST_SSH_OAUTH": "",
        "PYTHONDONTWRITEBYTECODE": "1",
    }
    if race_enabled:
        test_env["GORACE"] = "halt_on_error=1 atexit_sleep_ms=0"
    if coverage_enabled:
        covdata_dir = f"$(B)/coverage/raw/{test_slug}"
        coverage_archive = f"$(B)/coverage/{test_slug}.tar"
        test_commands = [
            mkdir(covdata_dir),
            ["python3", "-m", test_module],
            ["python3", "$(S)/dev/coverage.py", "pack", "--dir", covdata_dir, "--out", coverage_archive],
            touch(test_stamp),
        ]
        test_inputs.append("$(S)/dev/coverage.py")
        test_outputs = [test_stamp, coverage_archive]
        test_env["GOCOVERDIR"] = covdata_dir
        coverage_archives.append(coverage_archive)
    binary_tests.append(command(
        name=f"unit_{test_slug}",
        inputs=test_inputs,
        outputs=test_outputs,
        deps=[ay_test],
        cmd=test_commands,
        cwd="$(S)",
        env=test_env,
        descr="BT",
        color="green",
    ))

make_test_paths = sorted(build.glob("$(S)/tst/make/test_*.py"))
make_test_inputs = sorted(build.glob("$(S)/tst/make/*.py"))
# An external, mutable Arcadia is not an input tracked by build's CAS. Give
# only these test invocations fresh identities; never reuse a cached pass.
make_test_run = uuid.uuid4().hex
ay_e2e_tests = []
make_test_results = []
make_coverage_archives = []
make_test_binary = ay_test if coverage_enabled or race_enabled else ay
for test_path in make_test_paths:
    name = test_path.rsplit("/", 1)[-1].removesuffix(".py")
    result_file = f"$(B)/e2e/ay/{name}.json"
    make_test_results.append(result_file)
    test_inputs = list(make_test_inputs)
    test_outputs = [result_file]
    test_env = {"AY_E2E_RUN_ID": make_test_run, "PYTHONDONTWRITEBYTECODE": "1"}
    test_commands = [
        ["python3", "-m", "tst.make", "--backend", "ay",
         "--arcadia", build.flags.arcadia, "--binary", make_test_binary.outputs[0],
         "--work-dir", "$(B)/e2e/work", "--result-file", result_file, f"tst.make.{name}"],
    ]
    if race_enabled:
        test_env["GORACE"] = "halt_on_error=1 atexit_sleep_ms=0"
    if coverage_enabled:
        covdata_dir = f"$(B)/coverage/make/raw/{name}"
        coverage_archive = f"$(B)/coverage/make/{name}.tar"
        test_commands.insert(0, mkdir(covdata_dir))
        test_commands.append([
            "python3", "$(S)/dev/coverage.py", "pack",
            "--dir", covdata_dir, "--out", coverage_archive,
        ])
        test_inputs.append("$(S)/dev/coverage.py")
        test_outputs.append(coverage_archive)
        test_env["GOCOVERDIR"] = covdata_dir
        make_coverage_archives.append(coverage_archive)
    ay_e2e_tests.append(command(
        name=f"ay_e2e_collect_{name.removeprefix('test_')}",
        inputs=test_inputs,
        outputs=test_outputs,
        deps=[make_test_binary],
        cmd=test_commands,
        cwd="$(S)",
        env=test_env,
        descr="BT", color="green", local=True,
    ))

ay_e2e_members = list(ay_e2e_tests)
if coverage_enabled:
    make_coverage = command(
        name="make_coverage_report",
        inputs=["$(S)/dev/coverage.py", *make_coverage_archives],
        outputs=["$(B)/coverage/make/cover.out", "$(B)/coverage/make/report.txt"],
        deps=ay_e2e_tests,
        cmd=[
            "python3", "$(S)/dev/coverage.py", "merge",
            "--out", "$(B)/coverage/make/cover.out",
            "--report", "$(B)/coverage/make/report.txt",
            "--minimum", "0", *make_coverage_archives,
        ],
        env=GO_ENV,
        descr="CV", color="yellow",
    )
    ay_e2e_members.append(make_coverage)

ya_e2e_tests = []
for test_path in make_test_paths:
    name = test_path.rsplit("/", 1)[-1].removesuffix(".py")
    stamp = f"$(B)/e2e/ya/{name}.stamp"
    ya_e2e_tests.append(command(
        name=f"ya_e2e_{name.removeprefix('test_')}",
        inputs=make_test_inputs,
        outputs=[stamp],
        deps=[ay],
        cmd=[
            ["python3", "-m", "tst.make", "--backend", "ya",
             "--arcadia", build.flags.arcadia, "--overlay-binary", ay.outputs[0],
             "--work-dir", "$(B)/e2e/work", f"tst.make.{name}"],
            touch(stamp),
        ],
        cwd="$(S)",
        env={"AY_E2E_RUN_ID": make_test_run, "PYTHONDONTWRITEBYTECODE": "1"},
        descr="BT", color="green", local=True,
    ))
ay_e2e_gate = command(
    name="ay_e2e_gate",
    inputs=["$(S)/tst/make/check.py", *make_test_results],
    outputs=["$(B)/e2e/ay/gate.stamp"],
    deps=ay_e2e_members,
    cmd=["python3", "-m", "tst.make.check", "--stamp", "$(B)/e2e/ay/gate.stamp", *make_test_results],
    cwd="$(S)",
    descr="BT", color="green", local=True,
)
group("ay_e2e", *ay_e2e_members, ay_e2e_gate)
group("ay_e2e_results", *ay_e2e_members)
group("ya_e2e", *ya_e2e_tests)

if coverage_enabled:
    cli_coverage = command(
        name="cli_coverage",
        inputs=["$(S)/dev/coverage.py", *coverage_archives, *make_coverage_archives],
        outputs=["$(B)/coverage/cli/cover.out", "$(B)/coverage/cli/report.txt"],
        deps=[*binary_tests, *ay_e2e_tests],
        cmd=[
            "python3", "$(S)/dev/coverage.py", "merge",
            "--out", "$(B)/coverage/cli/cover.out",
            "--report", "$(B)/coverage/cli/report.txt",
            "--minimum", "90", *coverage_archives, *make_coverage_archives,
        ],
        env=GO_ENV,
        descr="CV", color="yellow",
    )

unit_members = [python_test, *binary_tests]
if coverage_enabled:
    coverage_profile = "$(B)/coverage/cover.out"
    coverage_report = "$(B)/coverage/report.txt"
    coverage = command(
        name="coverage",
        inputs=["$(S)/dev/coverage.py", *coverage_archives],
        outputs=[coverage_profile, coverage_report],
        deps=binary_tests,
        cmd=[
            "python3", "$(S)/dev/coverage.py", "merge",
            "--out", coverage_profile, "--report", coverage_report,
            "--minimum", COVERAGE_MINIMUM,
            *coverage_archives,
        ],
        env=GO_ENV,
        descr="CV",
        color="yellow",
    )
    unit_members.append(coverage)


with (ROOT / "dev" / "config.json").open(encoding="utf-8") as stream:
    validation_config = json.load(stream)

resource_targets = {}
resource_specs = {}


def validation_resource(url, checksum):
    resource = url.rstrip("/").rsplit("/", 1)[-1]
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", resource):
        raise RuntimeError(f"unsafe validation resource id in {url!r}")
    checksum = checksum or "-"
    spec = (url, checksum)
    previous = resource_specs.get(resource)
    if previous is not None and previous != spec:
        raise RuntimeError(f"conflicting validation resource {resource}: {previous!r} vs {spec!r}")
    resource_specs[resource] = spec
    if resource not in resource_targets:
        output = f"$(B)/validation/resources/{resource}.archive"
        resource_targets[resource] = command(
            name=f"validation_resource_{slug(resource)}",
            inputs=["$(S)/dev/fetch_validation_resource.py"],
            outputs=[output],
            cmd=[
                "python3",
                "$(S)/dev/fetch_validation_resource.py",
                url,
                output,
                checksum,
            ],
            descr="DL",
            color="blue",
        )
    return resource_targets[resource]


validation_results = []
validation_gates = []
validation_result_paths = []
validation_gate_by_id = {}

for case in validation_config:
    case_id = case["id"]
    case_slug = slug(case_id)
    if case_id != case_slug:
        raise RuntimeError(f"validation case id must be path-safe: {case_id!r}")
    slice_resource = validation_resource(case["slice_url"], case.get("slice_sha256"))
    graph_resource = validation_resource(case["graph_url"], case.get("graph_sha256"))
    result_directory = f"$(B)/validation/cases/{case_id}"
    result_path = result_directory + "/result.json"
    encoded_spec = base64.urlsafe_b64encode(
        json.dumps(case, sort_keys=True, separators=(",", ":")).encode()
    ).decode()

    result = command(
        name=f"validation_data_{case_slug}",
        inputs=[
            "$(S)/dev/validate_case.py",
            "$(S)/dev/validation_lib.py",
        ],
        outputs=[result_directory],
        deps=[ay, slice_resource, graph_resource],
        cmd=[
            "python3",
            "$(S)/dev/validate_case.py",
            "--ay", ay.outputs[0],
            "--spec-base64", encoded_spec,
            "--slice-archive", slice_resource.outputs[0],
            "--graph-archive", graph_resource.outputs[0],
            "--out", result_directory,
        ],
        cwd="$(B)",
        env={"AY_TEST_SSH_OAUTH": ""},
        descr="VR",
        color="magenta",
    )

    gate_stamp = f"$(B)/validation/gates/{case_id}.stamp"
    gate = command(
        name=f"validate_{case_slug}",
        inputs=[
            "$(S)/dev/check_validation_result.py",
            "$(S)/dev/validation_lib.py",
        ],
        outputs=[gate_stamp],
        deps=[result],
        cmd=[
            "python3",
            "$(S)/dev/check_validation_result.py",
            result_path,
            gate_stamp,
        ],
        descr="VG",
        color="green",
    )

    validation_results.append(result)
    validation_gates.append(gate)
    validation_result_paths.append(result_path)
    validation_gate_by_id[case_id] = gate
    group(f"validation_result_{case_slug}", result)

summary_json = "$(B)/validation/summary.json"
summary_text = "$(B)/validation/summary.txt"
validation_summary = command(
    name="validation_summary",
    inputs=[
        "$(S)/dev/validation_summary.py",
        "$(S)/dev/validation_lib.py",
    ],
    outputs=[summary_json, summary_text],
    deps=validation_results,
    cmd=[
        "python3",
        "$(S)/dev/validation_summary.py",
        "--json", summary_json,
        "--text", summary_text,
        *validation_result_paths,
    ],
    descr="VS",
    color="cyan",
)

validation_gate_stamp = "$(B)/validation/gate.stamp"
validation_gate = command(
    name="validation_gate",
    inputs=[
        "$(S)/dev/check_validation_summary.py",
        "$(S)/dev/validation_lib.py",
    ],
    outputs=[validation_gate_stamp],
    deps=[validation_summary],
    cmd=[
        "python3",
        "$(S)/dev/check_validation_summary.py",
        summary_json,
        validation_gate_stamp,
    ],
    descr="VG",
    color="green",
)

selected_validation_gates = validation_gates
if partition is not None:
    group_index, group_count = partition
    ranked_validation_gates = sorted(
        validation_gate_by_id.items(),
        key=lambda item: (hashlib.sha256(item[0].encode()).digest(), item[0]),
    )
    selected_validation_gates = [
        gate
        for rank, (_case_id, gate) in enumerate(ranked_validation_gates)
        if rank % group_count == group_index
    ]

group("install", ay)
group("unit", *unit_members)
group("validation_resources", *resource_targets.values())
group("validation_results", validation_summary)
group("validation_report", validation_summary)
group("validation_cases", *validation_gates)
group("validation_shard", *selected_validation_gates)
group(
    "validate_source_features",
    validation_gate_by_id["feature_resource_order"],
    validation_gate_by_id["feature_cython_py3_inputs"],
    validation_gate_by_id["feature_cpp_generated_header"],
    validation_gate_by_id["feature_cython_py2_inputs"],
)
group("validate", validation_summary, validation_gate)
group("test", *unit_members, validation_summary, validation_gate)
