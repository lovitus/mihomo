package main

import (
	"runtime"
	"strings"
)

const tsnetLocalPackagePrefix = "github.com/metacubex/mihomo/component/tsnet"

func allowDefaultResolverDialForCurrentStack() bool {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(2, pcs)
	if n == 0 {
		return false
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if allowDefaultResolverDialForFunction(frame.Function) {
			return true
		}
		if !more {
			return false
		}
	}
}

func allowDefaultResolverDialForFunction(fn string) bool {
	return strings.HasPrefix(fn, "tailscale.com/") || strings.HasPrefix(fn, tsnetLocalPackagePrefix)
}
