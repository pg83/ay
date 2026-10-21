package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/jon-codes/getopt"
)

var fatalOnce sync.Once

var yaHostToolchainFlags = map[string]string{
	"NO_DEBUGINFO":     "yes",
	"TOOL_BUILD_MODE":  "yes",
	"CLANG_COVERAGE":   "no",
	"TRAVERSE_RECURSE": "no",
	"CONSISTENT_DEBUG": "yes",
	"TIDY":             "no",
}

const (
	cmdFileStartMarker = "--ya-start-command-file"
	cmdFileEndMarker   = "--ya-end-command-file"
)

type MakeFlags struct {
	testRetries   int
	testTraceback string
	testBuildType string

	addModulesToResults bool
	srcRoot             string
	bldRoot             string
	outRoot             string
	installRoot         string
	threads             int
	keepGoing           bool
	dumpGraph           bool
	stats               bool
	ninja               bool
	buildType           string
	hostBuildType       string
	targetPlat          string
	hostPlat            string
	dflags              map[string]string
	tflags              map[string]string
	hflags              map[string]string
	platformFlags       map[string]string
	targets             []string
	verbose             bool
	testLevel           int
	runTests            int
	testSizes           []string
	testTags            []string
	sandboxing          bool
	dist                bool
	cacheTests          bool
	dumpIgnoredMacros   bool
	clear               bool
	cmdPrefixes         []CmdPrefix
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
		fatalable := w.Kind == WarnMissingInclude || w.Kind == WarnUnsupportedSource ||
			w.Kind == WarnUnknownMacro || w.Kind == WarnBadMacroArgs ||
			w.Kind == WarnMissingProducer || w.Kind == WarnModuleFailed

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

	fs := newFS(mf.srcRoot)
	caches := newGenCaches(fs)
	rootYaConf := readYaConf(fs, "ya.conf")
	hostP, targetP := makePlatforms(fs, mf, caches, rootYaConf)
	tests := newTestOpts(mf, rootYaConf)
	events := newEventQueue()

	defer events.close()

	onWarn := warnHandler(mf.keepGoing, mf.verbose, func(line string) {
		events.post(func() {
			fmt.Fprintln(os.Stderr, line)
		})
	})

	if mf.threads == 0 {
		if mf.dumpGraph {
			g := genDumpGraphWithResources(fs, caches, mf.targets, hostP, targetP, onWarn, tests, mf.keepGoing, demandSelf, mf.addModulesToResults)

			writeGraph(g, !mf.sandboxing)
		} else {
			genStream(fs, caches, mf.targets, hostP, targetP, func(*Node, *DenseMap[STR, NodeRef]) {}, onWarn, tests, mf.keepGoing)
		}

		if mf.dumpIgnoredMacros {
			dumpMacroAudit(os.Stderr)
		}

		return 0
	}

	ex := newExecutor(mf.srcRoot, mf.bldRoot, fs, mf.threads, mf.keepGoing, mf.ninja, mf.sandboxing, mf.clear, mf.cmdPrefixes, events)

	ex.startGarbageCollector()

	results := genStream(fs, caches, mf.targets, hostP, targetP, ex.onNode, onWarn, tests, mf.keepGoing)

	ex.run(results)

	if mf.stats {
		ex.printStats()
	}

	failedRoots := ex.failedRoots(results)

	if len(failedRoots) > 0 {
		failedStrs := make([]string, len(failedRoots))

		for i, r := range failedRoots {
			failedStrs[i] = strconv.FormatUint(uint64(r), 10)
		}

		throwFmt("build failed: %s", strings.Join(failedStrs, ", "))
	}

	if failed := ex.failed.Load(); failed > 0 {
		throwFmt("build failed: %d nodes failed", failed)
	}

	for _, r := range results {
		ex.installRoot(r, mf.installRoot)
	}

	return 0
}

func makePlatforms(fs FS, mf *MakeFlags, caches *GenCaches, rootYaConf map[string]any) (hostP, targetP *Platform) {
	rootHostYaFlags := yaConfStringTable(rootYaConf["host_platform_flags"])
	rootTargetYaFlags := yaConfStringTable(rootYaConf["flags"])
	hostInternalYaFlags := readOptionalYaConfSection(fs, "build/internal/ya.conf", "host_platform_flags")
	targetInternalYaFlags := readOptionalYaConfSection(fs, "build/internal/ya.conf", "flags")
	hostPlatformFlags := map[string]string{}
	targetOptFlags := map[string]string{}

	copyStatsFlags(hostPlatformFlags, rootHostYaFlags)
	copyStatsFlags(hostPlatformFlags, hostInternalYaFlags)
	copyStatsFlags(hostPlatformFlags, mf.hflags)
	copyStatsFlags(targetOptFlags, rootTargetYaFlags)
	copyStatsFlags(targetOptFlags, targetInternalYaFlags)
	copyStatsFlags(targetOptFlags, mf.dflags)

	disableFlake8Migrations := rootYaConf["disable_flake8_migrations"] != false

	if value, ok := os.LookupEnv("YA_TEST_DISABLE_FLAKE8_MIGRATIONS"); ok {
		disableFlake8Migrations = envEnabled(value)
	}

	if disableFlake8Migrations {
		targetOptFlags["DISABLE_FLAKE8_MIGRATIONS"] = "yes"
	}

	hOS, hISA := resolvePlatform(mf.hostPlat)
	targetSpec := mf.targetPlat

	if targetSpec == "" {
		targetSpec = string(makePlatformID(hOS, hISA))
	}

	tOS, tISA := resolvePlatform(targetSpec)
	toolchains := readYaConfToolchains(fs)
	hostName := toolchainPlatformName(hOS, hISA)
	hostTC := resolveCxxToolchain(fs, toolchains, hostName, hostName)
	targetTC := resolveCxxToolchain(fs, toolchains, hostName, toolchainPlatformName(tOS, tISA))
	hostTCFlags := map[string]string{}

	copyStatsFlags(hostTCFlags, yaHostToolchainFlags)
	copyStatsFlags(hostTCFlags, hostPlatformFlags)

	targetTCFlags := map[string]string{}

	copyStatsFlags(targetTCFlags, mf.tflags)

	if mf.sandboxing {
		hostTCFlags["SANDBOXING"] = "yes"
		targetTCFlags["SANDBOXING"] = "yes"
	}

	hostFlags := map[string]string{}

	if mf.testLevel > 0 {
		hostFlags["TESTS_REQUESTED"] = "yes"
		targetOptFlags["TESTS_REQUESTED"] = "yes"
	}

	copyStatsFlags(hostFlags, hostPlatformFlags)
	hostFlags["IS_CROSS_TOOLS"] = "yes"
	hostFlags["IS_CROSS_SANITIZE"] = "yes"
	yaToolchainFlags(hostFlags, hostTC, hostTCFlags)

	targetFlags := map[string]string{}

	copyStatsFlags(targetFlags, targetOptFlags)
	targetFlags["IS_CROSS_SANITIZE"] = "yes"
	targetFlags["FORCE_NO_PIC"] = "yes"
	yaToolchainFlags(targetFlags, targetTC, targetTCFlags)

	if mf.testLevel > 0 {
		targetFlags["TRAVERSE_RECURSE_FOR_TESTS"] = "yes"
		targetFlags["TRAVERSE_DEPENDS"] = "yes"
	}

	srcRoot := throw2(filepath.Abs(mf.srcRoot))
	localFlags := readLocalYmake(fs)
	hostRun := startYmakeConf(fs, srcRoot, mf.hostBuildType, !mf.dist, localFlags, hostFlags, hostTC)
	targetRun := startYmakeConf(fs, srcRoot, mf.buildType, !mf.dist, localFlags, targetFlags, targetTC)

	var picRun *YmakeConfRun

	startPIC := func() *YmakeConfRun {
		picFlags := maps.Clone(targetFlags)

		delete(picFlags, "FORCE_NO_PIC")

		return startYmakeConf(fs, srcRoot, mf.buildType, !mf.dist, localFlags, picFlags, targetTC)
	}

	caches.prewarm(fs, mf.targets, func() {
		if targetFlags["FORCE_NO_PIC"] == "yes" {
			picRun = startPIC()
		}
	})

	pc := readPlatformConf(fs, caches.parsers)
	hostVars, hostRoot := hostRun.wait()

	hostP = newPlatform(
		hOS,
		hISA,
		mf.hostBuildType,
		hostTC,
		hostVars,
		hostRoot,
		pc,
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CFLAGS", ""),
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CXXFLAGS", ""),
	)

	targetCFlags := compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CFLAGS", os.Getenv("CFLAGS"))
	targetCXXFlags := compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CXXFLAGS", os.Getenv("CXXFLAGS"))
	targetVars, targetRoot := targetRun.wait()

	targetP = newPlatform(tOS, tISA, mf.buildType, targetTC, targetVars, targetRoot, pc, targetCFlags, targetCXXFlags)
	targetP.picMake = func() *Platform {
		if picRun == nil {
			picRun = startPIC()
		}

		picVars, picRoot := picRun.wait()

		return newPlatform(tOS, tISA, mf.buildType, targetTC, picVars, picRoot, pc, targetCFlags, targetCXXFlags)
	}

	return hostP, targetP
}

func yaToolchainFlags(flags map[string]string, tc *Toolchain, tcFlags map[string]string) {
	if params, ok := tc.Params["params"].(map[string]any); ok {
		if extra, ok := params["extra_flags"].(map[string]any); ok {
			for k, v := range extra {
				if s, ok := yaConfScalar(v); ok {
					flags[k] = s
				}
			}
		}
	}

	if tcFlags["SANDBOXING"] == "yes" {
		tcFlags["FAKEID"] += "sandboxing"
	}

	copyStatsFlags(flags, tcFlags)
	flags[strings.ToUpper(tc.Name)] = "yes"
	flags["YA"] = "yes"
}

func genStream(fs FS, caches *GenCaches, targets []string, hostP, targetP *Platform, onNode func(*Node, *DenseMap[STR, NodeRef]), onWarn func(Warn), tests *TestOpts, keepGoing bool) []NodeRef {
	emitter := newStreamingEmitter(onNode)

	runGenIntoWithResources(fs, caches, targets, hostP, targetP, emitter, onWarn, tests, keepGoing, demandSelf)

	return emitter.finish()
}

func parseMakeFlags(args []string) *MakeFlags {
	state := getopt.NewState(append([]string{"ay-make"}, args...))

	config := getopt.Config{
		Opts: getopt.OptStr("GrdktThxD:j:B:o:I:C:"),
		LongOpts: getopt.LongOptStr("musl,help,xbuild:,install:,output:,stat,build-dir:,source-root:,keep-going,dump-graph,release,debug,target-platform:,host-platform:,host-platform-flag:,host-build-type:,verbose,sandboxing,dump-ignored-macros,clear,cmd-prefix:,jobs:,ninja,define:," +
			"target:,build:,target-platform-build-type:,target-platform-flag:,target-platform-tests,test-size:,test-tag:,test-traceback:," +
			"dist,ignore-nodes-exit-code,warning-mode:,cache-tests,output-only-tests,cache-namespace:,report-skipped-suites,tests-retries:," +
			"backup-test-results,add-modules-to-results,do-not-output-stderrs,no-src-links,add-changed-ok-configures,no-yt-store"),
		Mode: getopt.ModeInOrder,
		Func: getopt.FuncGetOptLong,
	}

	targetBuildType := ""

	mf := &MakeFlags{
		buildType:     "debug",
		testTraceback: "short",
		hostBuildType: "release",
		dflags:        map[string]string{},
		threads:       runtime.NumCPU(),
		tflags:        map[string]string{},
		hflags:        map[string]string{},
		platformFlags: map[string]string{},
	}

	for opt, err := range state.All(config) {
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
		case opt.Name == "stat":
			mf.stats = true
		case opt.Name == "musl":
			parseKV(mf.tflags, "MUSL=yes")
		case opt.Char == 'T' || opt.Name == "ninja":
			mf.ninja = true
		case opt.Char == 't':
			mf.testLevel++
			mf.runTests++
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
			parseKV(mf.dflags, opt.OptArg)
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
		case opt.Name == "host-build-type":
			mf.hostBuildType = opt.OptArg
		case opt.Char == 'C' || opt.Name == "target":
			for _, t := range strings.Split(opt.OptArg, ";") {
				if t != "" {
					mf.targets = append(mf.targets, t)
				}
			}
		case opt.Name == "build":
			mf.buildType = opt.OptArg
		case opt.Name == "target-platform-build-type":
			targetBuildType = opt.OptArg
		case opt.Name == "target-platform-flag":
			parseKV(mf.tflags, opt.OptArg)
			parseKV(mf.platformFlags, opt.OptArg)
		case opt.Name == "target-platform-tests":
			mf.testLevel = max(mf.testLevel, 1)
		case opt.Name == "test-size":
			mf.testSizes = append(mf.testSizes, strings.ToLower(opt.OptArg))
		case opt.Name == "test-tag":
			mf.testTags = append(mf.testTags, opt.OptArg)
		case opt.Name == "dist":
			mf.dist = true
		case opt.Name == "cache-tests":
			mf.cacheTests = true
		case opt.Name == "tests-retries":
			mf.testRetries = throw2(strconv.Atoi(opt.OptArg))
		case opt.Name == "test-traceback":
			mf.testTraceback = opt.OptArg
		case opt.Char == 'x', opt.Name == "ignore-nodes-exit-code",
			opt.Name == "warning-mode", opt.Name == "output-only-tests",
			opt.Name == "cache-namespace", opt.Name == "report-skipped-suites",
			opt.Name == "backup-test-results", opt.Name == "do-not-output-stderrs",
			opt.Name == "no-src-links", opt.Name == "add-changed-ok-configures", opt.Name == "no-yt-store":
		case opt.Name == "verbose":
			mf.verbose = true
		case opt.Name == "add-modules-to-results":
			mf.addModulesToResults = true
		case opt.Name == "sandboxing":
			mf.sandboxing = true
		case opt.Name == "dump-ignored-macros":
			mf.dumpIgnoredMacros = true
			enableMacroAudit()
		case opt.Char == 1:

			mf.targets = append(mf.targets, opt.OptArg)
		}
	}

	mf.testBuildType = mf.buildType

	if targetBuildType != "" {
		mf.buildType = targetBuildType
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
  --stat                        Print the 10 longest tasks and per-kind totals after the build.
  -G, --dump-graph              With -j 0, write the generated graph as JSON to stdout.
  --dump-ignored-macros         With -j 0, print service-keyword macro args no handler models.
  --verbose                     Emit Gen-time diagnostics (unsupported sysincl records, …) to stderr.
  --sandboxing                  Set SANDBOXING=yes and run every node in an isolated
                                source/build sandbox; with -G, keep $(S) inputs in the dump.

configuration flags:
  -r, --release                 Build type release.
  -d, --debug                   Build type debug (default).
  --xbuild <value>              Build type <value> (overrides -r/-d).
  --musl                        MUSL=yes.
  --build <value>               Build type <value>, as --xbuild.
  --target-platform <id>        Target platform id (default: <host>).
  --target-platform-build-type T  Target build type; overrides --build.
  --target-platform-flag KEY=V  Target-axis -D flag (repeatable).
  --target-platform-tests       Generate test nodes, as -t.
  --host-platform <id>          Host platform id (default: <host>).
  --host-build-type <value>     Host build type (default: release).
  -D, --define KEY=VALUE        Target-axis -D flag (repeatable).
  --host-platform-flag KEY=V    Host-axis -D flag (repeatable).

target flags:
  -C, --target <dirs>           Targets separated by ';', as positional ones
                                (repeatable). Several targets make one graph.

ya compatibility flags:
  -x                            Accepted from ya command lines, not modeled.
  --test-size <size>            Emit only test suites of these sizes
                                (repeatable).
  --test-tag <tags>             Select test tags; prefix exclusions with -
                                (repeatable).
  --dist                        Configure for distbuild: ymake_conf.py runs
                                without -l.
  --ignore-nodes-exit-code      Accepted, not modeled.
  --warning-mode <modes>        Accepted, not modeled.
  --cache-tests                 Test outputs are tarred (no dir outputs).
  --output-only-tests           Accepted, not modeled.
  --cache-namespace <name>      Accepted, not modeled.
  --report-skipped-suites       Accepted, not modeled.
  --tests-retries <n>           Schedule n test runs and merge their results.
  --backup-test-results         Accepted, not modeled.
  --add-modules-to-results      Accepted, not modeled.
  --do-not-output-stderrs       Accepted, not modeled.
  --no-src-links                Accepted, not modeled.
  --add-changed-ok-configures   Accepted, not modeled.
  --no-yt-store                 Accepted, not modeled.
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
	gap := strings.Index(trimmed, "  ")

	return indent + clFlag(trimmed[:gap]) + trimmed[gap:]
}
