// Copyright 2021 The Periph Authors. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package cpu

// On older linux (confirmed on 3.10), sched_priority is defined as
// __sched_priority but the reverse is not set. Try to workaround.

/*
#define __sched_priority  sched_priority
#include <sched.h>
#include <sys/resource.h>
*/
import "C"
import (
	"fmt"
)

func setHighPriority() error {
	// RLIMIT_NICE
	if ret := C.setpriority(0, 0, -20); ret != 0 {
		return fmt.Errorf("cpu: failed to set process nice priority to -20: %d", ret)
	}
	sp := C.struct_sched_param{sched_priority: 1}
	if ret := C.sched_setscheduler(0, C.SCHED_FIFO, &sp); ret != 0 {
		return fmt.Errorf("cpu: failed to set thread priority to SCHED_FIFO: %d", ret)
	}
	return nil
}
