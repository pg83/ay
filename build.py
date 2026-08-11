import base64
import hashlib
import json
import re
from pathlib import Path

import build


ROOT = Path(__file__).parent


def source_files(directory):
    root = ROOT / directory
    return [
        "$(S)/" + path.relative_to(ROOT).as_posix()
        for path in sorted(root.rglob("*"))
        if path.is_file()
    ]


def touch(path):
    return [
        "python3",
        "-c",
        f"from pathlib import Path; p=Path(r'{path}'); p.parent.mkdir(parents=True, exist_ok=True); p.touch()",
    ]


def slug(value):
    result = re.sub(r"[^A-Za-z0-9_]+", "_", value).strip("_")
    if not result:
        raise RuntimeError(f"cannot make target name from {value!r}")
    return result


build.flags.allow({
    "group": {
        "descr": "zero-based validation shard to include",
        "default": "",
    },
    "group_count": {
        "descr": "total number of validation shards",
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

GO_SOURCES = [
    path for path in build.glob("$(S)/*.go")
    if not path.endswith("_test.go")
]
GO_TEST_SOURCES = build.glob("$(S)/*_test.go")

GENERATED_DENSE_MAPS = [
    "$(B)/generated/go/dense_map_2.go",
    "$(B)/generated/go/dense_map_2_test.go",
    "$(B)/generated/go/dense_map_3.go",
    "$(B)/generated/go/dense_map_3_test.go",
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

GO_INPUTS = [
    *GO_SOURCES,
    *build.glob("$(S)/*.s"),
    *GENERATED_DENSE_MAPS,
    "$(S)/dev/go_overlay.py",
    "$(S)/.gitignore",
    "$(S)/CLAUDE.md",
    "$(S)/GOALS.md",
    "$(S)/LICENSE",
    "$(S)/PROMPTS.md",
    "$(S)/STYLE.md",
    "$(S)/acceptance",
    "$(S)/go.mod",
    "$(S)/go.sum",
    "$(S)/perf_darts_data.txt",
    *source_files("vendor"),
]

GO_OVERLAY = "$(B)/go-overlay.json"
GO_OVERLAY_CMD = [
    "python3", "$(S)/dev/go_overlay.py",
    "--output", GO_OVERLAY,
    "--source-root", "$(S)",
    *GENERATED_DENSE_MAPS,
]

GO_ENV = {
    "CGO_ENABLED": "0",
    "GOFLAGS": "-mod=vendor -buildvcs=false",
    "GOTOOLCHAIN": "local",
    "GOWORK": "off",
}

ay = command(
    name="ay",
    inputs=GO_INPUTS,
    outputs=["$(B)/bin/ay"],
    deps=[dense_maps],
    cmd=[
        GO_OVERLAY_CMD,
        [
            "go", "build",
            "-overlay=" + GO_OVERLAY,
            "-trimpath",
            "-buildvcs=false",
            "-o", "$(B)/bin/ay",
            ".",
        ],
    ],
    cwd="$(S)",
    env=GO_ENV,
    descr="GO",
    color="cyan",
)

go_test_stamp = "$(B)/tests/go.stamp"
go_test = command(
    name="go_test",
    inputs=[*GO_INPUTS, *GO_TEST_SOURCES],
    outputs=[go_test_stamp],
    deps=[dense_maps],
    cmd=[
        GO_OVERLAY_CMD,
        [
            "go", "test",
            "-overlay=" + GO_OVERLAY,
            "-count=1", "-timeout=2m", ".",
        ],
        touch(go_test_stamp),
    ],
    cwd="$(S)",
    env={**GO_ENV, "AY_TEST_SSH_OAUTH": ""},
    descr="UT",
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
for test_path in build.glob("$(S)/tst/test_*.py"):
    test_name = test_path.rsplit("/", 1)[-1][len("test_"):-len(".py")]
    test_slug = slug(test_name)
    test_stamp = f"$(B)/tests/{test_slug}.stamp"
    binary_tests.append(command(
        name=f"unit_{test_slug}",
        inputs=[test_path, "$(S)/tst/lib.py"],
        outputs=[test_stamp],
        deps=[ay],
        cmd=[
            ["python3", test_path],
            touch(test_stamp),
        ],
        cwd="$(S)",
        env={
            "AY_TEST_BINARY": ay.outputs[0],
            "AY_TEST_SSH_OAUTH": "",
            "PYTHONDONTWRITEBYTECODE": "1",
        },
        descr="BT",
        color="green",
    ))


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
group("unit", go_test, python_test, *binary_tests)
group("validation_resources", *resource_targets.values())
group("validation_results", validation_summary)
group("validation_report", validation_summary)
group("validation_cases", *validation_gates)
group("validation_shard", *selected_validation_gates)
group("validate", validation_summary, validation_gate)
group("test", go_test, python_test, *binary_tests, validation_summary, validation_gate)
