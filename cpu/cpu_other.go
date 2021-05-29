// Copyright 2021 The Periph Authors. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

//go:build !darwin && !linux && !windows
// +build !darwin,!linux,!windows

package cpu

import "errors"

func setHighPriority() error {
	return errors.New("cpu: high priority is not supported on this OS")
}
