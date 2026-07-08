package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/jon-codes/getopt"
)

var fatalOnce sync.Once

const (
	cmdFileStartMarker = "--ya-start-command-file"
	cmdFileEndMarker   = "--ya-end-command-file"
)

type MakeFlags struct {
	srcRoot           string
	bldRoot           string
	outRoot           string
	installRoot       string
	threads           int
	keepGoing         bool
	dumpGraph         bool
	stats             bool
	ninja             bool
	buildType         string
	targetPlat        string
	hostPlat          string
	tflags            map[string]string
	hflags            map[string]string
	targets           []string
	verbose           bool
	testLevel         int
	sandboxing        bool
	dumpIgnoredMacros bool
	clear             bool
	cmdPrefixes       []CmdPrefix
}

func copyStatsFlags(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

func readOptionalYaConfSection(fs FS, rel, wantSection string) map[string]string {
	if fs == nil || !fs.isFile(srcRootRel, rel) {
		return nil
	}

	return readYaConfSection(fs, rel, wantSection)
}

func joinCompilerFlagStrings(parts ...string) string {
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part == "" {
			continue
		}

		out = append(out, part)
	}

	return strings.Join(out, " ")
}

func compilerFlagsFromConfig(primary, internal map[string]string, key, env string) string {
	return joinCompilerFlagStrings(primary[key], internal[key], env)
}

func warnHandler(keepGoing, verbose bool, emit func(line string)) func(Warn) {
	seen := map[string]bool{}

	return func(w Warn) {
		fatalable := w.Kind == WarnMissingInclude || w.Kind == WarnUnsupportedSource

		if fatalable && !keepGoing {
			throwFmt("%s: %s", w.Kind, w.Message)
		}

		if !(fatalable || w.Kind == WarnMissingAddincl || verbose) {
			return
		}

		line := fmt.Sprintf("\x1b[33m%s: %s\x1b[0m", w.Kind, w.Message)

		if seen[line] {
			return
		}

		seen[line] = true
		emit(line)
	}
}

func cmdMake(g GlobalFlags, args []string) int {
	mf := parseMakeFlags(args)

	mf.verbose = mf.verbose || g.Verbose

	if len(mf.targets) == 0 {
		throwFmt("make: no targets supplied and current working directory is outside the source root")
	}

	if os.Getenv("GOGC") == "" {
		if mf.threads == 0 {
			debug.SetGCPercent(-1)
		} else {
			debug.SetGCPercent(executorGCPercent)
		}
	}

	fs := newFS(mf.srcRoot)
	tools := toolchainFlags(fs)
	rootHostYaFlags := readYaConfSection(fs, "ya.conf", "host_platform_flags")
	rootTargetYaFlags := readYaConfSection(fs, "ya.conf", "flags")
	hostYaFlags := map[string]string{}
	targetYaFlags := map[string]string{}

	copyStatsFlags(hostYaFlags, rootHostYaFlags)
	copyStatsFlags(targetYaFlags, rootTargetYaFlags)

	hostInternalYaFlags := readOptionalYaConfSection(fs, "build/internal/ya.conf", "host_platform_flags")
	targetInternalYaFlags := readOptionalYaConfSection(fs, "build/internal/ya.conf", "flags")

	copyStatsFlags(hostYaFlags, hostInternalYaFlags)
	copyStatsFlags(targetYaFlags, targetInternalYaFlags)

	hOS, hISA := resolvePlatform(mf.hostPlat)
	hostFlags := make(map[string]string, len(tools)+len(hostYaFlags)+len(mf.hflags)+1)

	for k, v := range tools {
		hostFlags[k] = v
	}

	for k, v := range hostYaFlags {
		hostFlags[k] = v
	}

	for k, v := range mf.hflags {
		hostFlags[k] = v
	}

	hostFlags["PIC"] = "yes"

	if _, ok := hostFlags["GG_BUILD_TYPE"]; !ok {
		hostFlags["GG_BUILD_TYPE"] = "release"
	}

	hostP := newPlatform(
		fs,
		hOS,
		hISA,
		hostFlags,
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CFLAGS", ""),
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CXXFLAGS", ""),
	)

	targetSpec := mf.targetPlat

	if targetSpec == "" {
		targetSpec = string(makePlatformID(hOS, hISA))
	}

	tOS, tISA := resolvePlatform(targetSpec)
	targetFlags := make(map[string]string, len(tools)+len(targetYaFlags)+len(mf.tflags)+3)

	for k, v := range tools {
		targetFlags[k] = v
	}

	for k, v := range targetYaFlags {
		targetFlags[k] = v
	}

	for k, v := range mf.tflags {
		targetFlags[k] = v
	}

	if mf.buildType != "" {
		targetFlags["GG_BUILD_TYPE"] = mf.buildType
	}

	if mf.testLevel > 0 {
		targetFlags["TESTS_REQUESTED"] = "yes"
	}

	if mf.sandboxing {
		targetFlags["SANDBOXING"] = "yes"
	}

	targetFlags["PIC"] = "no"

	targetP := newPlatform(
		fs,
		tOS,
		tISA,
		targetFlags,
		compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CFLAGS", os.Getenv("CFLAGS")),
		compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CXXFLAGS", os.Getenv("CXXFLAGS")),
	)

	if platformsEquivalent(hostP, targetP) {
		targetP = hostP
	}

	events := newEventQueue()

	defer events.close()

	onWarn := warnHandler(mf.keepGoing, mf.verbose, func(line string) {
		events.post(func() {
			fmt.Fprintln(os.Stderr, line)
		})
	})

	if mf.threads == 0 {
		if mf.dumpGraph {
			for _, target := range mf.targets {
				g := genDumpGraphWithResources(fs, target, hostP, targetP, onWarn, mf.testLevel > 0)

				writeGraph("-", g, !mf.sandboxing)
			}
		} else {
			genStream(fs, mf.targets, hostP, targetP, func(*Node, *DenseMap[STR, NodeRef]) {}, onWarn, mf.testLevel > 0)
		}

		if mf.dumpIgnoredMacros {
			dumpMacroAudit(os.Stderr)
		}

		return 0
	}

	ex := newExecutor(mf.srcRoot, mf.bldRoot, fs, mf.threads, mf.keepGoing, mf.ninja, mf.sandboxing, mf.cmdPrefixes, events)

	ex.startGarbageCollector()

	if mf.clear {
		ex.clearCache()
	}

	results := genStream(fs, mf.targets, hostP, targetP, ex.onNode, onWarn, mf.testLevel > 0)

	ex.run(results)

	failedRoots := ex.failedRoots(results)

	if len(failedRoots) > 0 {
		failedStrs := make([]string, len(failedRoots))

		for i, r := range failedRoots {
			failedStrs[i] = strconv.FormatUint(uint64(r), 10)
		}

		throwFmt("build failed: %s", strings.Join(failedStrs, ", "))
	}

	for _, r := range results {
		ex.installRoot(r, mf.installRoot)
	}

	return 0
}

func genStream(fs FS, targets []string, hostP, targetP *Platform, onNode func(*Node, *DenseMap[STR, NodeRef]), onWarn func(Warn), testMode bool) []NodeRef {
	emitter := newStreamingEmitter(onNode)

	for _, t := range targets {
		runGenIntoWithResources(fs, t, hostP, targetP, emitter, onWarn, testMode)
	}

	return emitter.finish()
}

func parseMakeFlags(args []string) *MakeFlags {
	state := getopt.NewState(append([]string{"ay-make"}, args...))

	config := getopt.Config{
		Opts:     getopt.OptStr("GrdktThD:j:B:o:I:"),
		LongOpts: getopt.LongOptStr("musl,help,xbuild:,install:,output:,stats,build-dir:,source-root:,keep-going,dump-graph,release,debug,target-platform:,host-platform:,host-platform-flag:,verbose,sandboxing,dump-ignored-macros,clear,cmd-prefix:,jobs:,ninja,define:"),
		Mode:     getopt.ModeInOrder,
		Func:     getopt.FuncGetOptLong,
	}

	mf := &MakeFlags{
		buildType: "debug",
		threads:   runtime.NumCPU(),
		tflags:    map[string]string{},
		hflags:    map[string]string{},
	}

	for opt, err := range state.All(config) {
		if err == getopt.ErrDone {
			break
		}

		throw(err)

		switch {
		case opt.Char == 'h' || opt.Name == "help":
			printMakeUsage(os.Stdout)
			os.Exit(0)
		case opt.Char == 'k' || opt.Name == "keep-going":
			mf.keepGoing = true
		case opt.Name == "clear":
			mf.clear = true
		case opt.Char == 'G' || opt.Name == "dump-graph":
			mf.dumpGraph = true
		case opt.Name == "stats":
			mf.stats = true
		case opt.Name == "musl":
			parseKV(mf.tflags, "MUSL=yes")
		case opt.Char == 'T' || opt.Name == "ninja":
			mf.ninja = true
		case opt.Char == 't':
			mf.testLevel++
		case opt.Char == 'o' || opt.Name == "output":
			mf.outRoot = opt.OptArg
		case opt.Char == 'I' || opt.Name == "install":
			mf.installRoot = opt.OptArg
		case opt.Char == 'B' || opt.Name == "build-dir":
			mf.bldRoot = opt.OptArg
		case opt.Name == "source-root":
			mf.srcRoot = opt.OptArg
		case opt.Name == "cmd-prefix":
			mf.cmdPrefixes = append(mf.cmdPrefixes, parseCmdPrefix(opt.OptArg))
		case opt.Char == 'D' || opt.Name == "define":
			parseKV(mf.tflags, opt.OptArg)
		case opt.Char == 'j' || opt.Name == "jobs":
			n, perr := strconv.Atoi(opt.OptArg)

			throw(perr)
			mf.threads = n
		case opt.Char == 'r' || opt.Name == "release":
			mf.buildType = "release"
		case opt.Char == 'd' || opt.Name == "debug":
			mf.buildType = "debug"
		case opt.Name == "xbuild":
			mf.buildType = opt.OptArg
		case opt.Name == "target-platform":
			mf.targetPlat = opt.OptArg
		case opt.Name == "host-platform":
			mf.hostPlat = opt.OptArg
		case opt.Name == "host-platform-flag":
			parseKV(mf.hflags, opt.OptArg)
		case opt.Name == "verbose":
			mf.verbose = true
		case opt.Name == "sandboxing":
			mf.sandboxing = true
		case opt.Name == "dump-ignored-macros":
			mf.dumpIgnoredMacros = true
			enableMacroAudit()
		case opt.Char == 1:

			mf.targets = append(mf.targets, opt.OptArg)
		default:
			throwFmt("make: unhandled flag %v", opt)
		}
	}

	if mf.srcRoot == "" {
		mf.srcRoot = findSourceRoot(throw2(os.Getwd()))
	}

	if mf.bldRoot == "" {
		mf.bldRoot = filepath.Join(throw2(os.UserHomeDir()), ".ya", "ay")
	}

	if mf.outRoot == "" {
		mf.outRoot = filepath.Join(mf.bldRoot, "res")
	}

	if mf.installRoot == "" {
		mf.installRoot = mf.srcRoot
	}

	if len(mf.targets) == 0 {
		cwd := throw2(os.Getwd())

		if rel, err := filepath.Rel(mf.srcRoot, cwd); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			mf.targets = []string{rel}
		}
	}

	return mf
}

func findSourceRoot(start string) string {
	for dir := start; ; {
		if info, err := os.Stat(filepath.Join(dir, "ya.conf")); err == nil && !info.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)

		if parent == dir {
			return start
		}

		dir = parent
	}
}

func parseKV(into map[string]string, kv string) {
	idx := strings.IndexByte(kv, '=')

	if idx < 0 {
		into[kv] = "yes"

		return
	}

	into[kv[:idx]] = kv[idx+1:]
}

func printMakeUsage(w io.Writer) {
	const usage = `usage: ay make [flags] [targets...]
  Build the targets in dependency order, executing per-node cmds.
  Without targets, builds the current directory relative to the source root.

layout flags:
  --source-root <path>          Source tree root.
  -B, --build-dir <path>        Build directory (default: ~/.ya/ay).
  -o, --output <path>           Output staging dir (default: <build-dir>/res).
  -I, --install <path>          Install outputs into this directory (default: source-root).

execution flags:
  -h, --help                    Show this help.
  -j, --jobs <N>                Parallel exec slots (default: NumCPU); 0 = generate the graph only, no execution.
  -k, --keep-going              Continue past per-node failures.
  --clear                       Move cas/tmp/uid into grb at start (fresh build cache).
  --cmd-prefix <suffix>=<pfx>   Prepend <pfx> tokens before any command arg whose path
                                ends with <suffix> (repeatable). E.g. run fetched glibc
                                binaries through a loader: bin/java=/bin/ld.linux-so.2
  -T, --ninja                   Ninja-style per-line output (default: in-place repaint).
  -t, -tt, -ttt                 Generate test nodes (small / +medium / +large).
  --stats                       Print per-kind execution stats after the build.
  -G, --dump-graph              With -j 0, write the generated graph as JSON to stdout.
  --dump-ignored-macros         With -j 0, print service-keyword macro args no handler models.
  --verbose                     Emit Gen-time diagnostics (unsupported sysincl records, …) to stderr.
  --sandboxing                  Set SANDBOXING=yes and run every node in an isolated
                                source/build sandbox; with -G, keep $(S) inputs in the dump.

configuration flags:
  -r, --release                 GG_BUILD_TYPE=release.
  -d, --debug                   GG_BUILD_TYPE=debug (default).
  --xbuild <value>              GG_BUILD_TYPE=<value> (overrides -r/-d).
  --musl                        MUSL=yes.
  --target-platform <id>        Target platform id (default: <host>).
  --host-platform <id>          Host platform id (default: <host>).
  -D, --define KEY=VALUE        Target-axis -D flag (repeatable).
  --host-platform-flag KEY=V    Host-axis -D flag (repeatable).
`

	fmt.Fprint(w, colorizeMakeUsage(usage))
}

func colorizeMakeUsage(s string) string {
	lines := strings.Split(s, "\n")

	for i, line := range lines {
		switch {
		case strings.HasSuffix(line, "flags:"):
			lines[i] = clHeader(line)
		case strings.HasPrefix(line, "usage:"):
			lines[i] = clHeader("usage:") + line[len("usage:"):]
		default:
			lines[i] = colorizeFlagLine(line)
		}
	}

	return strings.Join(lines, "\n")
}

func colorizeFlagLine(line string) string {
	trimmed := strings.TrimLeft(line, " ")

	if !strings.HasPrefix(trimmed, "-") {
		return line
	}

	indent := line[:len(line)-len(trimmed)]

	if gap := strings.Index(trimmed, "  "); gap >= 0 {
		return indent + clFlag(trimmed[:gap]) + trimmed[gap:]
	}

	return indent + clFlag(trimmed)
}
