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

type MakeFlags struct {
	srcRoot           string
	bldRoot           string
	outRoot           string
	installRoot       string
	threads           int
	keepGoing         bool
	dumpGraph         bool
	copySources       string
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
	cmdPrefixes       []CmdPrefix
}

func copyStatsFlags(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

func readOptionalYaConfSection(fs FS, rel, wantSection string) map[string]string {
	if fs == nil || !fs.isFile(srcRootVFS, rel) {
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

func cmdMake(g GlobalFlags, args []string) int {
	defer startProfilesFromEnv()()

	mf := parseMakeFlags(args)

	// The global --verbose seeds make's local verbose; make's own --verbose can
	// still turn it on independently.
	mf.verbose = mf.verbose || g.Verbose

	if len(mf.targets) == 0 {
		throwFmt("make: no targets supplied and current working directory is outside the source root")
	}

	// -j 0 is generate-only: the process emits the graph and exits, so collecting
	// garbage along the way only costs CPU (measured on sg5: GC off cuts gen user
	// time ~20%, peak RSS 495->680 MB — fine for a short-lived process). With
	// executor threads the process lives for the whole build and shares RAM with
	// the compilers it spawns, so GC stays on — but far rarer than the default:
	// the sg5 GOGC scan plateaus at ~the GC-off user time from 400 up (the
	// 400..800 spread is the phase of the last mark cycle, not a trend), so 400
	// keeps near-full gen speed while still bounding the long-lived build
	// process's heap. An explicit GOGC in the environment wins in both modes.
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
	// build/internal/ya.conf carries the internal contour's common flags
	// (OPENSOURCE, USE_PREBUILT_TOOLS=no, the -fno-omit-frame-pointer /
	// -Wno-unknown-argument CFLAGS, …). Upstream applies it to every build,
	// test or not; both sg4 (-ttt) and sg5 references now carry these CFLAGS,
	// so it is read unconditionally (absent in the opensource snapshots, where
	// readOptionalYaConfSection returns nil).
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

	onWarn := func(w Warn) {
		if w.Kind == WarnMissingInclude && !mf.keepGoing {
			throwFmt("%s: %s", w.Kind, w.Message)
		}

		if mf.verbose {
			fmt.Fprintf(os.Stderr, "\x1b[33m%s: %s\x1b[0m\n", w.Kind, w.Message)
		}
	}

	if mf.copySources != "" {
		// Build the graph as for -G (the FS records every read), then slice the repo
		// by what was actually opened. The graph itself is discarded.
		for _, target := range mf.targets {
			genDumpGraphWithResources(fs, target, hostP, targetP, onWarn, mf.testLevel > 0)
		}

		osfs, ok := fs.(*OsFS)

		if !ok {
			throwFmt("--copy-sources requires the on-disk source FS")
		}

		throw(copySourceSlice(osfs, mf.srcRoot, mf.copySources, onWarn))

		return 0
	}

	if mf.threads == 0 {
		if mf.dumpGraph {
			for _, target := range mf.targets {
				g := genDumpGraphWithResources(fs, target, hostP, targetP, onWarn, mf.testLevel > 0)
				writeGraph("-", g, !mf.sandboxing)
			}
		} else {
			genStream(fs, mf.targets, hostP, targetP, func(*Node, *UidVec, *DenseMap[STR, NodeRef]) {}, onWarn, mf.testLevel > 0)
		}

		if mf.dumpIgnoredMacros {
			dumpMacroAudit(os.Stderr)
		}

		return 0
	}

	ex := newExecutor(mf.srcRoot, mf.bldRoot, mf.threads, mf.keepGoing, mf.ninja, mf.sandboxing, mf.cmdPrefixes)
	ex.startGarbageCollector()

	go ex.eventLoop()

	defer ex.close()

	executorWarn := func(w Warn) {
		if w.Kind == WarnMissingInclude && !mf.keepGoing {
			throwFmt("%s: %s", w.Kind, w.Message)
		}

		if mf.verbose {
			kind := w.Kind
			message := w.Message

			ex.events <- func() {
				fmt.Fprintf(os.Stderr, "\x1b[33m%s: %s\x1b[0m\n", kind, message)
			}
		}
	}

	results := genStream(fs, mf.targets, hostP, targetP, ex.onNode, executorWarn, mf.testLevel > 0)

	ex.run(results)

	failedRoots := ex.failedRoots(results)

	if len(failedRoots) > 0 {
		failedStrs := make([]string, len(failedRoots))

		for i, u := range failedRoots {
			failedStrs[i] = u.string()
		}

		throwFmt("build failed: %s", strings.Join(failedStrs, ", "))
	}

	for _, uid := range results {
		ex.installRoot(uid, mf.installRoot)
	}

	return 0
}

func genStream(fs FS, targets []string, hostP, targetP *Platform, onNode func(*Node, *UidVec, *DenseMap[STR, NodeRef]), onWarn func(Warn), testMode bool) []UID {
	all := []UID{}

	for _, t := range targets {
		ec := genStreamOne(fs, t, hostP, targetP, onNode, onWarn, testMode)
		all = append(all, ec...)
	}

	return all
}

func genStreamOne(fs FS, target string, hostP, targetP *Platform, onNode func(*Node, *UidVec, *DenseMap[STR, NodeRef]), onWarn func(Warn), testMode bool) []UID {
	emitter := newStreamingEmitter(onNode)
	runGenIntoWithResources(fs, target, hostP, targetP, emitter, onWarn, testMode)

	return emitter.finish()
}

const (
	cmdFileStartMarker = "--ya-start-command-file"
	cmdFileEndMarker   = "--ya-end-command-file"
)

func parseMakeFlags(args []string) *MakeFlags {
	state := getopt.NewState(append([]string{"ay-make"}, args...))
	config := getopt.Config{
		Opts:     getopt.OptStr("GrdktThD:j:B:o:I:"),
		LongOpts: getopt.LongOptStr("musl,help,xbuild:,install:,output:,stats,build-dir:,source-root:,keep-going,dump-graph,copy-sources:,release,debug,target-platform:,host-platform:,host-platform-flag:,verbose,sandboxing,dump-ignored-macros,cmd-prefix:"),
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
		case opt.Char == 'G' || opt.Name == "dump-graph":
			mf.dumpGraph = true
		case opt.Name == "copy-sources":
			mf.copySources = opt.OptArg
		case opt.Name == "stats":
			mf.stats = true
		case opt.Name == "musl":
			parseKV(mf.tflags, "MUSL=yes")
		case opt.Char == 'T':
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
		case opt.Char == 'D':
			parseKV(mf.tflags, opt.OptArg)
		case opt.Char == 'j':
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
		mf.srcRoot = throw2(os.Getwd())
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
		cwd, err := os.Getwd()

		if err == nil && strings.HasPrefix(cwd, mf.srcRoot+"/") {
			mf.targets = []string{cwd[len(mf.srcRoot)+1:]}
		}
	}

	return mf
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

layout flags:
  --source-root <path>          Source tree root.
  -B, --build-dir <path>        Build directory (default: ~/.ya/ay).
  -o, --output <path>           Output staging dir (default: <build-dir>/res).
  -I, --install <path>          Install outputs into this directory (default: source-root).

execution flags:
  -j, --jobs <N>                Parallel exec slots (default: NumCPU); 0 = build-only.
  -k, --keep-going              Continue past per-node failures.
  --cmd-prefix <suffix>=<pfx>   Prepend <pfx> tokens before any command arg whose path
                                ends with <suffix> (repeatable). E.g. run fetched glibc
                                binaries through a loader: bin/java=/bin/ld.linux-so.2
  -T, --ninja                   Ninja-style per-line output (default: in-place repaint).
  -t, -tt, -ttt                 Generate test nodes (small / +medium / +large).
  --stats                       Print per-kind execution stats after the build.
  -G, --dump-graph              Log a graph summary to stderr after Gen.
  --verbose                     Emit Gen-time diagnostics (unsupported sysincl records, …) to stderr.
  --sandboxing                  Run test nodes under the filesystem sandbox.

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

// colorizeMakeUsage tints the make help by the same scheme as the top-level
// listing: section headers ("… flags:" and the Usage: label) light-green, flag
// columns light-yellow. Descriptions and continuation lines stay plain.
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

// colorizeFlagLine tints the flag column (the leading "-…" token up to the 2-space
// gap before its description) light-yellow; non-flag lines pass through.
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
