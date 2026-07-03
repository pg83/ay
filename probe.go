package main

func dumpProbes(probes []string) {
	for _, p := range probes {
		switch p {
		case "map":
			reportMapProbe()
		}
	}
}
