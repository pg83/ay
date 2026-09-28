import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


RED = "\x1b[31m"
RESET = "\x1b[0m"


def run_ay(*args, cwd, timeout=60, env=None):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        cwd=cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )


def write_tree(root, files):
    for name, content in files.items():
        (root / name).write_text(content)


def read_tree(root):
    return {
        path.name: path.read_text()
        for path in sorted(root.iterdir())
        if path.is_file()
    }


LINT_FIXTURES = {
    'comments.go': """\
// Package comment.
package main

// +build ignore

//go:generate echo keep
// plain doc
/* block */
func commented() int {
	//extern foo
	return 1 // trailing
}
""",
    'vars.go': """\
package main

import "fmt"

//go:generate echo block
var (
	//go:generate echo spec
	a = 1
	b = 2 //go:trailing
)

func useVars() {
	fmt.Println(a, b, c, d, e)
}

//go:generate echo single
var c = 3

var d = []int{
	1,
}

var e = 5 //go:after
""",
    'single_var.go': """\
package main

func useSingle() int {
	return single
}

var single = 1
""",
    'consts.go': """\
package main

var early = 0

//go:generate echo iota
const (
	k0 = iota
	k1
)

func useConsts() int { return k0 + k1 + k2 + k3 + k4 }

//go:generate echo block
const (
	//go:generate echo spec
	k2 = 2
	k3 = 3 //go:trailing
)

//go:generate echo single
const k4 = 4 //go:tail
""",
    'consts_one.go': """\
package main

import "os"

const lone = "x"

var _ = os.Args
""",
    'blocks.go': """\
package main

import "fmt"

type Point struct {

	x, y int

}

type Shape interface {

	area() int

}

func empty() {}
func asmOnly()
func one() int { return 1 }
//go:noinline
func control(xs []int, ch chan int) int {

	total := 0
	for _, x := range xs {
		total += x
	}
	if total > 10 {
		return total
	}
	switch total {
	case 1:
		fmt.Println("one")
		total++
	default:
	}
	select {
	case v := <-ch:
		total += v
		fmt.Println(v)
	default:
	}
	defer fmt.Println("done")
	go func() {
		fmt.Println("async")
	}()
	go fmt.Println("plain")
	var any interface{} = total
	switch any.(type) {
	case int:
	}
	for i := 0; i < 2; i++ {
		if i == 0 {
			continue
		}
		break
	}
loop:
	for {
		break loop
	}
	fmt.Println(
		"multi",
	)
	a, b := fmt.Println(
		"assign",
	)
	_, _ = a, b
	a, b = fmt.Println(
		"again",
	)
	p := Point{

		x: 1,

	}
	p = Point{
		y: 2,
	}
	p.x = 2
	p.y = 3
	total = p.x
	//go:generate echo lead
	if total > 0 {
		total--
	}
	//go:generate echo plain
	fmt.Println(total)
	goto end
end:
	return total

}
""",
    'assign.go': """\
package main

func coalesce(p *Point, m map[int]int, ptr *int) int {
	a := 1

	b := 2

	//go:generate echo keep
	c := 3
	x, y := a, b

	x = 4

	x += 1

	y = 5
	c = 6
	p.x = 1

	p.y = 2

	q = 3
	m[0] = 1

	m[1] = 2
	*ptr = 3

	(*ptr) = 4
	f().x = 5

	g().y = 6
	d := []int{
		1,
	}

	e := 7
	return a + b + c + x + y + e + len(d)
}
""",
}

LINT_EXPECTED = {
    'comments.go': """\
//go:build ignore
// +build ignore

package main

//go:generate echo keep

func commented() int {
	//extern foo
	return 1
}
""",
    'vars.go': """\
package main

import "fmt"

var (
	//go:generate echo block
	//go:generate echo spec
	a = 1
	b = 2 //go:trailing
	//go:generate echo single
	c = 3
	e = 5 //go:after
)

var d = []int{
	1,
}

func useVars() {
	fmt.Println(a, b, c, d, e)
}
""",
    'single_var.go': """\
package main

var single = 1

func useSingle() int {
	return single
}
""",
    'consts.go': """\
package main

var early = 0

const (
	//go:generate echo block
	//go:generate echo spec
	k2 = 2
	k3 = 3 //go:trailing
	//go:generate echo single
	k4 = 4 //go:tail
)

//go:generate echo iota
const (
	k0 = iota
	k1
)

func useConsts() int {
	return k0 + k1 + k2 + k3 + k4
}
""",
    'consts_one.go': """\
package main

import "os"

var _ = os.Args

const lone = "x"
""",
    'blocks.go': """\
package main

import "fmt"

type Point struct {
	x, y int
}

type Shape interface {
	area() int
}

func empty() {
}

func asmOnly()

func one() int {
	return 1
}

//go:noinline
func control(xs []int, ch chan int) int {
	total := 0

	for _, x := range xs {
		total += x
	}

	if total > 10 {
		return total
	}

	switch total {
	case 1:
		fmt.Println("one")
		total++
	default:
	}

	select {
	case v := <-ch:
		total += v
		fmt.Println(v)
	default:
	}

	defer fmt.Println("done")

	go func() {
		fmt.Println("async")
	}()

	go fmt.Println("plain")
	var any interface{} = total

	switch any.(type) {
	case int:
	}

	for i := 0; i < 2; i++ {
		if i == 0 {
			continue
		}

		break
	}

loop:
	for {
		break loop
	}

	fmt.Println(
		"multi",
	)

	a, b := fmt.Println(
		"assign",
	)

	_, _ = a, b

	a, b = fmt.Println(
		"again",
	)

	p := Point{
		x: 1,
	}

	p = Point{
		y: 2,
	}
	p.x = 2
	p.y = 3

	total = p.x

	//go:generate echo lead
	if total > 0 {
		total--
	}

	//go:generate echo plain
	fmt.Println(total)
	goto end

end:
	return total
}
""",
    'assign.go': """\
package main

func coalesce(p *Point, m map[int]int, ptr *int) int {
	a := 1
	b := 2

	//go:generate echo keep
	c := 3
	x, y := a, b

	x = 4

	x += 1

	y = 5
	c = 6

	p.x = 1
	p.y = 2

	q = 3

	m[0] = 1
	m[1] = 2

	*ptr = 3
	(*ptr) = 4

	f().x = 5
	g().y = 6

	d := []int{
		1,
	}

	e := 7

	return a + b + c + x + y + e + len(d)
}
""",
}

LINT_STDERR = """\
refac lint: strip-comments: comments.go
refac lint: consolidate-vars: vars.go
refac lint: consolidate-vars: single_var.go
refac lint: consolidate-consts: consts.go
refac lint: expand-func-bodies: consts.go
refac lint: consolidate-vars: consts_one.go
refac lint: expand-func-bodies: blocks.go
refac lint: func-blank-lines: blocks.go
refac lint: blank-around-blocks: blocks.go
refac lint: tight-braces: blocks.go
refac lint: blank-around-blocks: assign.go
refac lint: coalesce-assign: assign.go
"""

CASE_LINT = """\
package main

import "strings"

type lower struct{ s string }

type Iface interface {
	Upper()
	String() string
	lowerOK()
	embedded
}

func Exported() {}

func (l lower) Method() {}

func (l lower) okLower() {}

func (l lower) String() string { return l.string() }

func (l *lower) UnmarshalJSON(b []byte) error { l.unmarshalJSON(b) }

func (l lower) Len() int

func (l lower) Less(i, j int) bool { x := i; return x < j }

func (lower) Push(x any) { panic(x) }

func (l lower) Pop() any { return l.a(), l.b() }

func (l lower) MarshalJSON() ([]byte, error) { return nil }

func (l lower) Unwrap() error { l }

func (l lower) Swap(i, j int) { i = j }

func (l lower) Len() int { return len(l.s) }

func (l lower) Error() string { return l.inner().error() }

func (l lower) Error() string { return strings.ToUpper(l.s) }

func (l lower) String() string { return l.other() }
"""

CASE_LINT_STDERR = """\
refac lint: expand-func-bodies: names.go
refac lint: blank-around-blocks: names.go
refac lint: case-convention: names.go:5: lower-case type lower
refac lint: case-convention: names.go:8: upper-case interface method Upper
refac lint: case-convention: names.go:14: upper-case function Exported
refac lint: case-convention: names.go:17: upper-case method Method
refac lint: case-convention: names.go:31: non-wrapper upper-case stdlib method Len
refac lint: case-convention: names.go:33: non-wrapper upper-case stdlib method Less
refac lint: case-convention: names.go:39: non-wrapper upper-case stdlib method Push
refac lint: case-convention: names.go:43: non-wrapper upper-case stdlib method Pop
refac lint: case-convention: names.go:47: non-wrapper upper-case stdlib method MarshalJSON
refac lint: case-convention: names.go:51: non-wrapper upper-case stdlib method Unwrap
refac lint: case-convention: names.go:55: non-wrapper upper-case stdlib method Swap
refac lint: case-convention: names.go:59: non-wrapper upper-case stdlib method Len
refac lint: case-convention: names.go:63: non-wrapper upper-case stdlib method Error
refac lint: case-convention: names.go:67: non-wrapper upper-case stdlib method Error
refac lint: case-convention: names.go:71: non-wrapper upper-case stdlib method String
refac lint: case-convention: names.go
"""

OWNED_GLOBALS = """\
package main

import "strings"

type ANY uint32

type EnvVars []string

const limit = 2

var typed []ANY

var fixed [limit]VFS

var envs EnvVars

var other []int

var ptrs []*VFS

var lit = []STR{1}

var structLit = struct{}{}

var mapLit = map[string]int{}

var fromCall = makeRefs()

var fromSel = strings.Fields("a")

var fromMethodLike = notSlice()

var _ = []ANY{}

var pairA, pairB = []VFS{}, 3

var multiA, multiB []NodeRef

var extraName, missingValue = []VFS{}

var number = 1

func makeRefs() []NodeRef {
	return nil
}

func (a ANY) method() []ANY {
	return nil
}

func noResult() {
}

func two() (int, int) {
	return 1, 2
}

func notSlice() int {
	return 0
}
"""

OWNED_GLOBALS_LINTED = """\
package main

import "strings"

var (
	typed                   []ANY
	fixed                   [limit]VFS
	envs                    EnvVars
	other                   []int
	ptrs                    []*VFS
	lit                     = []STR{1}
	structLit               = struct{}{}
	mapLit                  = map[string]int{}
	fromCall                = makeRefs()
	fromSel                 = strings.Fields("a")
	fromMethodLike          = notSlice()
	_                       = []ANY{}
	pairA, pairB            = []VFS{}, 3
	multiA, multiB          []NodeRef
	extraName, missingValue = []VFS{}
	number                  = 1
)

const limit = 2

type ANY uint32

type EnvVars []string

func makeRefs() []NodeRef {
	return nil
}

func (a ANY) method() []ANY {
	return nil
}

func noResult() {
}

func two() (int, int) {
	return 1, 2
}

func notSlice() int {
	return 0
}
"""

OWNED_GLOBALS_GEN = """\
package main

func registerKnownGlobalChunks() {
	registerOwnedSlice(envs)
	registerOwnedSlice(extraName)
	registerOwnedSlice(fromCall)
	registerOwnedSlice(lit)
	registerOwnedSlice(multiA)
	registerOwnedSlice(multiB)
	registerOwnedSlice(pairA)
	registerOwnedSlice(typed)
}
"""

CONSTS_FIXTURES = {
    'emit.go': """\
package main

import "strings"

// existingVFS doc.
var existingVFS = source("contrib/tools/x")

var (
	keepMe = 1
	// existingStr doc.
	existingStr  = internStr("hello") // trailing
	notHoistable = strings.ToUpper("x")
	dupA         = internStr("dup")
	dupB         = internStr("dup")
	extraValue   = 1, intern("extra")
)

const constArg = internArg("-O2")

type unrelated int

func len2() {}

func emit() {
	use(intern("$(S)/contrib/tools/x"))
	use(source("util/generic"))
	use(build("util/generic/lib.a"))
	use(intern("$(B)/gen/out.h"))
	use(internStr("hello"))
	use(internStr("world"))
	use(internArg("-O2"))
	use(internArg("-fPIC"))
	use(internEnv("PATH"))
	use(intern("func"))
	use(intern("len"))
	use(intern("///"))
	use(intern("9lives"))
	use(internStr("emit"))
	use(pkg.intern("x"))
	use(intern("a", "b"))
	use(intern(name))
	use(intern(`raw`))
	use(intern(1))
	use(other("x"))
	use(wrap(internStr("nested")))
	use(internArgs([]string{"-O2", "-g"}))
	use(internArgs(list))
	use(internArgs([]string{}))
	use(internArgs([]string{"-a", name}))
	use(internArgs([]string{"-b"}, 2))
}

var tail = wrap(internEnv("HOME"), internStr("world"))
""",
    'inline.go': """\
package main

var (x = intern("inline")
	y = 2
)
""",
    'plain.go': """\
package main

func plain() int {
	return 1
}
""",
    'env_consts.go': """\
package main

var (
	envStale = internEnv("STALE")
)
""",
    'broken.go': """\
package main

func broken( {
""",
}

CONSTS_STDERR = """\
refac consts: broken.go: parse: broken.go:3:14: expected ')', found '{'
refac consts: rewrote emit.go
refac consts: inline.go: format failed (left unchanged): 3:2: expected declaration, found y
refac consts: generated env_consts.go (2 consts)
refac consts: generated vfs_consts.go (10 consts)
refac consts: generated str_consts.go (6 consts)
refac consts: generated arg_consts.go (4 consts)
"""

CONSTS_EXPECTED = {
    'arg_consts.go': """\
package main

// ARG (compiler/linker token) constants, interned once. Generated by `ay refac consts`.
var (
	argA     = internArg("-a")
	argFpic  = internArg("-fPIC")
	argG     = internArg("-g")
	constArg = internArg("-O2")
)
""",
    'broken.go': """\
package main

func broken( {
""",
    'emit.go': """\
package main

import "strings"

var (
	keepMe       = 1
	notHoistable = strings.ToUpper("x")
	extraValue   = 1, intern("extra")
)

const constArg = internArg("-O2")

type unrelated int

func len2() {}

func emit() {
	use(existingVFS)
	use(utilGeneric)
	use(bldUtilGenericLibA)
	use(bldGenOutH)
	use(existingStr)
	use(strWorld)
	use(constArg)
	use(argFpic)
	use(envPath)
	use(func2)
	use(len3)
	use(v)
	use(v9lives)
	use(strEmit)
	use(pkg.intern("x"))
	use(intern("a", "b"))
	use(intern(name))
	use(raw)
	use(intern(1))
	use(other("x"))
	use(wrap(strNested))
	use([]ARG{
		constArg,
		argG,
	})
	use(internArgs(list))
	use(internArgs([]string{}))
	use(internArgs([]string{"-a", name}))
	use(internArgs([]string{"-b"}, 2))
}

var tail = wrap(internEnv("HOME"), strWorld)
""",
    'env_consts.go': """\
package main

// ENV name constants, interned once. Generated by `ay refac consts`.
var (
	envPath  = internEnv("PATH")
	envStale = internEnv("STALE")
)
""",
    'inline.go': """\
package main

var (x = intern("inline")
	y = 2
)
""",
    'plain.go': """\
package main

func plain() int {
	return 1
}
""",
    'str_consts.go': """\
package main

// STR constants, interned once. Generated by `ay refac consts`.
var (
	dupA        = internStr("dup")
	dupB        = internStr("dup")
	existingStr = internStr("hello")
	strEmit     = internStr("emit")
	strNested   = internStr("nested")
	strWorld    = internStr("world")
)
""",
    'vfs_consts.go': """\
package main

// VFS path constants, interned once. Generated by `ay refac consts`.
var (
	bldGenOutH         = build("gen/out.h")
	bldUtilGenericLibA = build("util/generic/lib.a")
	existingVFS        = source("contrib/tools/x")
	func2              = intern("func")
	len3               = intern("len")
	raw                = intern("raw")
	utilGeneric        = source("util/generic")
	v                  = intern("///")
	v9lives            = intern("9lives")
	x                  = intern("inline")
)
""",
}


RESET_TYPES = """\
package main

const size = 4

type Holder struct {
	names   []STR
	nodes   []*Node
	fixed   [size]int
	index   map[string]int
	count   int
	nested  struct{ a int }
	a, b    []int
	embedded
}

type NotStruct int

var unrelated = 1
"""

RESET_OTHER = """\
package main

type (
	Other struct{ xs []uint64 }
)
"""

HOLDER_RESET = """\
// Code generated by `ay dev refac reset Holder`. DO NOT EDIT.

package main

func (x *Holder) reset() {
	*x = Holder{
		names: x.names[:0],
		nodes: scrub(x.nodes),
		index: clearedMap(x.index),
		a:     x.a[:0],
		b:     x.b[:0],
	}
}
"""

OTHER_RESET = """\
// Code generated by `ay dev refac reset Other`. DO NOT EDIT.

package main

func (x *Other) reset() {
	*x = Other{
		xs: x.xs[:0],
	}
}
"""

GO_MOD = "module casefix\n\ngo 1.21\n"

CASE_RENAMES = """\
package main

import (
	"fmt"
	"os"
	"path/filepath"
	str "strings"
)

type point struct{ x int }

type Shape interface {
	Area() int
	String() string
}

func (p point) Area() int { return p.x }

func (p point) String() string { return fmt.Sprint(p.x) }

func Helper(p point) int { return p.Area() }

func Str() string { return str.ToUpper("x") }

func Filepath() string { return filepath.Base("/a/b") }

func Fmt() {}

func main() {
	var s Shape = point{1}
	fmt.Println(Helper(point{2}), s.Area(), Str(), Filepath())
	Fmt()
	_ = os.Args
}
"""

CASE_RENAMED = """\
package main

import (
	"fmt"
	"os"
	"path/filepath"
	str "strings"
)

type Point struct{ x int }

type Shape interface {
	area() int
	String() string
}

func (p Point) area() int { return p.x }

func (p Point) String() string { return fmt.Sprint(p.x) }

func helper(p Point) int { return p.area() }

func Str() string { return str.ToUpper("x") }

func Filepath() string { return filepath.Base("/a/b") }

func Fmt() {}

func main() {
	var s Shape = Point{1}
	fmt.Println(helper(Point{2}), s.area(), Str(), Filepath())
	Fmt()
	_ = os.Args
}
"""

CASE_FALLBACKS = """\
package main

func Helper() {}

type thing struct{}

func (thing) Run() {}

func main() {
	var t thing
	f := Run
	t.Helper()
	_ = f
}
"""

CASE_FALLBACKS_AFTER = """\
package main

func helper() {}

type Thing struct{}

func (Thing) run() {}

func main() {
	var t Thing
	f := run
	t.helper()
	_ = f
}
"""


class RefacTestCase(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-refac-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)


class RefacLintTest(RefacTestCase):
    def test_lint_rewrites_files_to_the_style_guide(self):
        write_tree(self.root, LINT_FIXTURES)
        result = run_ay("dev", "refac", "lint", *LINT_FIXTURES, cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, LINT_STDERR)
        self.assertEqual(read_tree(self.root), LINT_EXPECTED)
        again = run_ay("dev", "refac", "lint", *LINT_FIXTURES, cwd=self.root)
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual(again.stderr, "")
        self.assertEqual(read_tree(self.root), LINT_EXPECTED)

    def test_case_convention_reports_violations(self):
        (self.root / "names.go").write_text(CASE_LINT)
        result = run_ay("dev", "refac", "lint", "names.go", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, CASE_LINT_STDERR)

    def test_parse_error_aborts_at_case_convention(self):
        broken = "package main\n\nfunc broken( {\n"
        (self.root / "broken.go").write_text(broken)
        result = run_ay("dev", "refac", "lint", "broken.go", cwd=self.root)
        self.assertEqual(result.returncode, 1)
        message = "broken.go:3:14: expected ')', found '{'"
        self.assertEqual(
            result.stderr,
            f"refac lint: broken.go: parse: {message}\n" * 7
            + RED + message + RESET + "\n",
        )
        self.assertEqual((self.root / "broken.go").read_text(), broken)

    def test_consolidation_that_breaks_syntax_is_skipped(self):
        oneline = "package main\n\nvar a = 1; var b = 2\n\nconst c = 1; const d = 2\n"
        (self.root / "oneline.go").write_text(oneline)
        result = run_ay("dev", "refac", "lint", "oneline.go", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "refac lint: oneline.go: consolidate-vars format failed (left unchanged): "
            "9:1: expected declaration, found ';'\n"
            "refac lint: oneline.go: consolidate-consts format failed (left unchanged): "
            "11:1: expected declaration, found ';'\n",
        )
        self.assertEqual((self.root / "oneline.go").read_text(), oneline)

    def test_lint_without_arguments_generates_ownership_globals(self):
        (self.root / "globals.go").write_text(OWNED_GLOBALS)
        ignored = "package main\n\nvar   ignored = 1\n"
        (self.root / "globals_test.go").write_text(ignored)
        (self.root / "notes.txt").write_text("not go\n")
        (self.root / "dir.go").mkdir()
        result = run_ay("dev", "refac", "lint", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "refac lint: consolidate-vars: globals.go\n"
            "refac lint: consolidate-consts: globals.go\n"
            "refac lint: node-ownership-globals: node_ownership_gen.go\n",
        )
        self.assertEqual((self.root / "globals.go").read_text(), OWNED_GLOBALS_LINTED)
        self.assertEqual(
            (self.root / "node_ownership_gen.go").read_text(),
            OWNED_GLOBALS_GEN,
        )
        self.assertEqual((self.root / "globals_test.go").read_text(), ignored)
        again = run_ay("dev", "refac", "lint", cwd=self.root)
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual(again.stderr, "")


class RefacConstsTest(RefacTestCase):
    def test_consts_hoists_interned_literals(self):
        write_tree(self.root, CONSTS_FIXTURES)
        result = run_ay("dev", "refac", "consts", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            sorted(result.stderr.splitlines()),
            sorted(CONSTS_STDERR.splitlines()),
        )
        self.assertEqual(read_tree(self.root), CONSTS_EXPECTED)

    def test_consts_reuses_generated_names_and_removes_empty_files(self):
        write_tree(self.root, {
            "str_consts.go": (
                "package main\n\nvar (\n\tstrOld = internStr(\"old\")\n)\n"
            ),
            "arg_consts.go": "package main\n",
            "use.go": (
                "package main\n\nfunc use() {\n"
                "\tf(internStr(\"old\"), internStr(\"new\"))\n}\n"
            ),
        })
        result = run_ay("dev", "refac", "consts", "str_consts.go", "arg_consts.go", "use.go", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sorted(result.stderr.splitlines()), [
            "refac consts: generated str_consts.go (2 consts)",
            "refac consts: removed empty arg_consts.go",
            "refac consts: rewrote use.go",
        ])
        self.assertEqual(read_tree(self.root), {
            "str_consts.go": (
                "package main\n\n"
                "// STR constants, interned once. Generated by `ay refac consts`.\n"
                "var (\n"
                "\tstrNew = internStr(\"new\")\n"
                "\tstrOld = internStr(\"old\")\n"
                ")\n"
            ),
            "use.go": "package main\n\nfunc use() {\n\tf(strOld, strNew)\n}\n",
        })


class RefacResetTest(RefacTestCase):
    def setUp(self):
        super().setUp()
        write_tree(self.root, {"types.go": RESET_TYPES, "other.go": RESET_OTHER})

    def test_reset_generates_capacity_preserving_reset(self):
        result = run_ay("dev", "refac", "reset", "Holder", "Other", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout,
            "refac reset: wrote types_reset_gen.go\n"
            "refac reset: wrote other_reset_gen.go\n",
        )
        self.assertEqual((self.root / "types_reset_gen.go").read_text(), HOLDER_RESET)
        self.assertEqual((self.root / "other_reset_gen.go").read_text(), OTHER_RESET)

    def test_reset_errors(self):
        cases = {
            (): "refac reset: expected struct type names",
            ("NotStruct",): "refac reset: NotStruct is not a struct",
            ("Missing",): "refac reset: type Missing not found",
        }
        for args, message in cases.items():
            result = run_ay("dev", "refac", "reset", *args, cwd=self.root)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, RED + message + RESET + "\n")
        (self.root / "other_reset_gen.go").mkdir()
        result = run_ay("dev", "refac", "reset", "Other", cwd=self.root)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            RED + "refac reset: open other_reset_gen.go: is a directory" + RESET + "\n",
        )


class RefacCaseTest(RefacTestCase):
    def setUp(self):
        super().setUp()
        self.assertIsNotNone(shutil.which("go"), "refac case needs go on PATH")
        (self.root / "go.mod").write_text(GO_MOD)

    def case(self, source):
        (self.root / "main.go").write_text(source)
        return run_ay("dev", "refac", "case", cwd=self.root, timeout=120)

    def test_case_renames_declarations_and_references(self):
        result = self.case(CASE_RENAMES)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            'refac case: main.go: function Str lowers to reserved "str" — rename manually\n'
            'refac case: main.go: function Filepath lowers to reserved "filepath" — rename manually\n'
            'refac case: main.go: function Fmt lowers to reserved "fmt" — rename manually\n'
            "refac case: renamed decls: 2 types, 1 method names\n",
        )
        self.assertEqual((self.root / "main.go").read_text(), CASE_RENAMED)
        build = subprocess.run(
            ["go", "build", "./..."],
            cwd=self.root,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=120,
            check=False,
        )
        self.assertEqual(build.returncode, 0, build.stdout)

    def test_case_gives_up_without_a_fixpoint(self):
        # Each build reports one more stale reference, as a chain of packages
        # does when every fix only lets the next package fail.
        fakebin = self.root / "fakebin"
        fakebin.mkdir()
        (fakebin / "go").write_text(
            "#!/bin/sh\n"
            "[ \"$1\" = build ] || exit 0\n"
            "line=$(grep -n -m1 '= foo{}' main.go | cut -d: -f1)\n"
            "[ -z \"$line\" ] || echo \"./main.go:$line:6: undefined: foo\"\n"
        )
        (fakebin / "go").chmod(0o755)
        references = "\t_ = foo{}\n" * 510
        (self.root / "main.go").write_text(
            f"package main\n\ntype foo struct{{}}\n\nfunc main() {{\n{references}}}\n"
        )
        env = {**os.environ, "PATH": f"{fakebin}{os.pathsep}{os.environ['PATH']}"}
        result = run_ay("dev", "refac", "case", cwd=self.root, env=env, timeout=120)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("refac case: no fixpoint after 501 rounds", result.stderr)

    def test_case_reports_unfixable_build_errors(self):
        result = self.case("package main\n\nfunc main() {\n\tundefinedThing()\n}\n")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            "refac case: renamed decls: 0 types, 0 method names\n"
            "refac case: unfixable build errors remain:\n"
            "# casefix\n"
            "./main.go:4:2: undefined: undefinedThing\n",
        )

    def test_case_reports_vet_findings_as_unfixable(self):
        result = self.case(
            'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Printf("%d\\n", "s")\n}\n'
        )
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            "refac case: renamed decls: 0 types, 0 method names\n"
            "refac case: unfixable build errors remain:\n",
        )

    def test_case_leaves_positions_remapped_by_line_directives(self):
        source = (
            "package main\n\nfunc Helper() {}\n\n"
            "func main() {\n//line main.go:1:1\n\tHelper()\n}\n"
        )
        result = self.case(source)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            "refac case: renamed decls: 1 types, 0 method names\n"
            "refac case: unfixable build errors remain:\n"
            "# casefix\n"
            "main.go:1:2: undefined: Helper\n",
        )
        self.assertEqual(
            (self.root / "main.go").read_text(),
            source.replace("func Helper", "func helper"),
        )

    def test_case_falls_back_across_rename_tables(self):
        result = self.case(CASE_FALLBACKS)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            "refac case: renamed decls: 2 types, 1 method names\n"
            "refac case: unfixable build errors remain:\n"
            "# casefix\n"
            "./main.go:11:7: undefined: run\n"
            "./main.go:12:4: t.helper undefined (type Thing has no field or method helper)\n",
        )
        self.assertEqual((self.root / "main.go").read_text(), CASE_FALLBACKS_AFTER)


if __name__ == "__main__":
    unittest.main(verbosity=2)
