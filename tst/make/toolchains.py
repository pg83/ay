"""Toolchain descriptions of the fixture tree.

The gate revision keeps the C++ toolchains in the toolchain section of
build/ya.conf.json, next to their bottles; other toolchains, and in trunk
all of them, live in build/tools/toolchains/*.toolchain.json, one file per
family with its own bottles and a toolchains section.
"""

import json

DIRECTORY = "build/tools/toolchains"
SUFFIX = ".toolchain.json"
CONF = "build/ya.conf.json"


def section(path):
    """The key of the toolchain map in the description at path."""
    return "toolchain" if path == CONF else "toolchains"


def toolchain_files(test):
    """{tree-relative path: parsed description} of every toolchain description."""
    root = test.root / DIRECTORY
    files = {f"{DIRECTORY}/{entry.name}": json.loads(entry.read_text())
             for entry in sorted(root.iterdir()) if entry.name.endswith(SUFFIX)} if root.is_dir() else {}
    conf = json.loads((test.root / CONF).read_text())
    if conf.get("toolchain"):
        files[CONF] = conf
    return files


def is_cxx(chain):
    return "c++" in chain.get("tools", {})


def cxx_files(test):
    """The toolchain files that describe a C++ toolchain."""
    files = {path: config for path, config in toolchain_files(test).items()
             if any(is_cxx(chain) for chain in config.get(section(path), {}).values())}
    test.assertTrue(files, "fixture tree has no C++ toolchains to modify")
    return files


def cxx_entries(files):
    """(path, name, toolchain) of every C++ toolchain in files."""
    return [(path, name, chain) for path, config in files.items()
            for name, chain in config[section(path)].items() if is_cxx(chain)]


def default_linux_clang(test):
    """(path, name, toolchain, platform) of the default Linux x86_64 clang."""
    for path, name, chain in cxx_entries(cxx_files(test)):
        if chain.get("params", {}).get("type") != "clang":
            continue
        for platform in chain["platforms"]:
            if platform.get("default") and platform.get("host", {}).get("os") == "LINUX" and platform.get("target") == {"os": "LINUX", "arch": "x86_64"}:
                return path, name, chain, platform
    return None


def alias_windows_targets(test, **target):
    """Make every Windows-targeted toolchain also accept os WINDOWS."""
    files = {}
    for path, config in toolchain_files(test).items():
        for chain in config.get(section(path), {}).values():
            for platform in list(chain.get("platforms", [])):
                if platform.get("target", {}).get("os") == "WIN":
                    chain["platforms"].append(dict(platform, target=dict(platform["target"], os="WINDOWS", **target)))
                    files[path] = config
    test.assertTrue(files, "fixture tree has no Windows-targeted toolchains")
    write(test, files)


def write(test, files):
    for path, config in files.items():
        test.write_tree(path, config if isinstance(config, str) else json.dumps(config))
