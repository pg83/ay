"""Private C++ toolchain target aliases for CLI fixtures."""

from tst.make import toolchains


def add_linux_clang_target(test, architecture):
    selected = toolchains.default_linux_clang(test)
    test.assertIsNotNone(selected)
    path, name, chain, platform = selected
    files = toolchains.cxx_files(test)
    for file, config in files.items():
        key = toolchains.section(file)
        config[key] = {chain: value for chain, value in config[key].items() if not toolchains.is_cxx(value) or chain == name}
    files[path][toolchains.section(path)][name]["platforms"] = [platform, dict(platform, target={"os": "LINUX", "arch": architecture})]
    toolchains.write(test, files)
