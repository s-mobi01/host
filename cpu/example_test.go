// Copyright 2021 The Periph Authors. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package cpu_test

import (
	"log"
	"runtime"
	"runtime/debug"

	"periph.io/x/host/v3/cpu"
)

func ExampleSetHighPriority() {
	// GC one last them and then disable GC.
	runtime.GC()
	debug.SetGCPercent(-1)

	// Disable the Go runtime scheduler for this goroutine.
	runtime.LockOSThread()

	if err := cpu.SetHighPriority(); err != nil {
		log.Fatal(err)
	}

	// Start CPU intensive work that do not do memory allocation.
}
