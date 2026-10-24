def resource_dependency(case, graph, consumer, name, uri, *, argument):
    """Resolve an independently declared resource and prove its runtime use."""
    output = "$(B)/resources/" + name
    producers = [node for node in graph["graph"] if node["outputs"] == [output]]
    case.assertEqual(len(producers), 1, (name, uri, producers))
    producer = producers[0]
    case.assertEqual(producer["kv"]["p"], "FT")
    case.assertEqual(len(producer["cmds"]), 1)
    args = producer["cmds"][0]["cmd_args"]
    case.assertEqual(args[1:], ["fetch", "$(B)", "$(S)", uri, output])
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
