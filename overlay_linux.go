package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func cmdOverlay(_ GlobalFlags, args []string) int {
	if len(args) < 3 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "usage: ay dev overlay <directory> -- <command> [args...]")

		return 2
	}

	root := throw2(filepath.EvalSymlinks(throw2(filepath.Abs(args[0]))))
	info := throw2(os.Stat(root))

	if !info.IsDir() {
		throwFmt("overlay: source must be a directory: %s", root)
	}

	if dir := os.Getenv("AY_OVERLAY_DIR"); dir != "" {
		overlayExec(root, dir, args[2:])
	}

	dir := throw2(os.MkdirTemp("", "ay-overlay-"))

	defer func() {
		_ = os.Chmod(filepath.Join(dir, "work", "work"), 0o700)
		throw(os.RemoveAll(dir))
	}()

	if strings.ContainsAny(root+dir, ",:\\") {
		throwFmt("overlay: directory paths must not contain commas, colons or backslashes")
	}

	throw(os.Mkdir(filepath.Join(dir, "upper"), 0o700))
	throw(os.Mkdir(filepath.Join(dir, "work"), 0o700))

	parentNS := throw2(os.Readlink("/proc/self/ns/mnt"))
	cmd := exec.Command(throw2(os.Executable()), append([]string{"dev", "overlay", root, "--"}, args[2:]...)...)

	cmd.Env = append(os.Environ(), "AY_OVERLAY_DIR="+dir, "AY_OVERLAY_PARENT_NS="+parentNS)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:  unix.CLONE_NEWUSER | unix.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getuid(), HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getgid(), HostID: os.Getgid(), Size: 1}},
		AmbientCaps: []uintptr{unix.CAP_SYS_ADMIN, unix.CAP_DAC_OVERRIDE},
		Setpgid:     true,
	}

	interrupts := make(chan os.Signal, 1)

	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(interrupts)

	throw(cmd.Start())

	done := make(chan struct{})

	defer close(done)

	go func() {
		for {
			select {
			case sig := <-interrupts:
				_ = syscall.Kill(-cmd.Process.Pid, sig.(syscall.Signal))
			case <-done:
				return
			}
		}
	}()

	err := cmd.Wait()

	var exit *exec.ExitError

	if errors.As(err, &exit) {
		status := exit.Sys().(syscall.WaitStatus)

		if status.Signaled() {
			return 128 + int(status.Signal())
		}

		return exit.ExitCode()
	}

	throw(err)

	return 0
}

func overlayExec(root, dir string, args []string) {
	runtime.LockOSThread()

	currentNS := throw2(os.Readlink("/proc/self/ns/mnt"))
	parentNS := os.Getenv("AY_OVERLAY_PARENT_NS")

	if parentNS == "" || parentNS == currentNS {
		throwFmt("overlay: refusing to mount outside a new mount namespace")
	}

	cwd := throw2(os.Getwd())

	throw(unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""))

	options := "userxattr,lowerdir=" + root + ",upperdir=" + filepath.Join(dir, "upper") + ",workdir=" + filepath.Join(dir, "work")

	throw(unix.Mount("overlay", root, "overlay", 0, options))
	throw(os.Chdir(cwd))
	throw(unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0))
	throw(os.Unsetenv("AY_OVERLAY_DIR"))
	throw(os.Unsetenv("AY_OVERLAY_PARENT_NS"))
	flushCoverage()
	throw(syscall.Exec(throw2(exec.LookPath(args[0])), args, os.Environ()))
}
