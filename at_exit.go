package main

var atExitHooks []func()

func atExit(fn func()) {
	atExitHooks = append(atExitHooks, fn)
}

func runAtExitHooks() {
	for i := len(atExitHooks) - 1; i >= 0; i-- {
		atExitHooks[i]()
	}
}
