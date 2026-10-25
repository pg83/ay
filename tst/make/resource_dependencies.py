import json


def fetches(graph):
    result = {}
    for node in graph["graph"]:
        if node["kv"]["p"] != "FT":
            continue
        [output] = node["outputs"]
        if output in result:
            raise AssertionError(f"duplicate fetch node for {output}")
        result[output] = node["cmds"][0]["cmd_args"][1:]
    return result


def fetched_uri(graph, name):
    output = f"$(B)/resources/{name}"
    args = fetches(graph)[output]
    if args[:2] == ["fetch", "--resource-json"]:
        _, _, raw, option, host, build_root, source_root, destination = args
        if [option, build_root, source_root, destination] != [
            "--host-platform", "$(B)", "$(S)", output,
        ]:
            raise AssertionError(f"unexpected descriptor fetch command {args!r}")
        descriptor = json.loads(raw)
        if descriptor["pattern"] != name:
            raise AssertionError(descriptor)
        if "resource" in descriptor:
            return descriptor["resource"]
        host = host.lower().removeprefix("default-").removesuffix("-x86_64").replace("windows", "win32")
        return next(resource["resource"] for resource in descriptor["resources"]
                    if resource["platform"].lower() == host)
    self_check = args[:3] + args[4:]
    if self_check != ["fetch", "$(B)", "$(S)", output]:
        raise AssertionError(f"unexpected fetch command {args!r}")
    return args[3]


def resource_dependency(case, graph, consumer, name, uri, *, argument):
    """Resolve an independently declared resource and prove its runtime use."""
    output = "$(B)/resources/" + name
    producers = [node for node in graph["graph"] if node["outputs"] == [output]]
    case.assertEqual(len(producers), 1, (name, uri, producers))
    producer = producers[0]
    case.assertEqual(producer["kv"]["p"], "FT")
    case.assertEqual(len(producer["cmds"]), 1)
    case.assertEqual(fetched_uri(graph, name), uri)
    case.assertIn(producer["uid"], consumer["deps"])
    case.assertEqual(len(argument), 2)
    case.assertTrue(any(list(argument) == cmd["cmd_args"][i:i + 2]
                        for cmd in consumer["cmds"]
                        for i in range(len(cmd["cmd_args"]) - 1)),
                    (name, argument, consumer["outputs"], consumer["cmds"]))
    return producer["uid"]


def global_resource_dependencies(case, graph, consumer, declarations):
    return [resource_dependency(case, graph, consumer, name, uri, argument=(
        "--global-resource", name + "_RESOURCE_GLOBAL::$(B)/resources/" + name))
        for name, uri in declarations]
