"""Toolchain descriptions of the fixture tree.

Trunk describes toolchains in build/tools/toolchains/*.toolchain.json, one
file per toolchain family with its own bottles; build/ya.conf.json keeps an
empty legacy section, and ya reads only the files.
"""

import json

DIRECTORY = "build/tools/toolchains"
SUFFIX = ".toolchain.json"


def toolchain_files(test):
    """{tree-relative path: parsed file} of every toolchain file."""
    root = test.root / DIRECTORY
    return {f"{DIRECTORY}/{entry.name}": json.loads(entry.read_text())
            for entry in sorted(root.iterdir()) if entry.name.endswith(SUFFIX)}


def is_cxx(chain):
    return "c++" in chain.get("tools", {})


def cxx_files(test):
    """The toolchain files that describe a C++ toolchain."""
    files = {path: config for path, config in toolchain_files(test).items()
             if any(is_cxx(chain) for chain in config.get("toolchains", {}).values())}
    test.assertTrue(files, "fixture tree has no C++ toolchains to modify")
    return files


def cxx_entries(files):
    """(path, name, toolchain) of every C++ toolchain in files."""
    return [(path, name, chain) for path, config in files.items()
            for name, chain in config["toolchains"].items() if is_cxx(chain)]


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
        for chain in config.get("toolchains", {}).values():
            for platform in list(chain.get("platforms", [])):
                if platform.get("target", {}).get("os") == "WIN":
                    chain["platforms"].append(dict(platform, target=dict(platform["target"], os="WINDOWS", **target)))
                    files[path] = config
    test.assertTrue(files, "fixture tree has no Windows-targeted toolchains")
    write(test, files)


def write(test, files):
    for path, config in files.items():
        test.write_tree(path, config if isinstance(config, str) else json.dumps(config))
