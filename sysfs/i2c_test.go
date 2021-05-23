// Copyright 2016 The Periph Authors. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package sysfs

import (
	"errors"
	"testing"

	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/conn/v3/physic"
)

func TestNewI2C(t *testing.T) {
	if b, err := NewI2C(-1); b != nil || err == nil {
		t.Fatal("invalid bus")
	}
}

func TestI2C_faked(t *testing.T) {
	// Create a fake I2C to test methods.
	bus := I2C{f: &ioctlClose{}, busNumber: 24}
	if s := bus.String(); s != "I2C24" {
		t.Fatal(s)
	}
	if bus.hasRead {
		t.Fatal("hasRead")
	}
	if bus.brokenRead {
		t.Fatal("should not have brokenRead")
	}
	if bus.Tx(0x401, nil, nil) == nil {
		t.Fatal("empty Tx")
	}
	if err := bus.Tx(1, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := bus.Tx(1, []byte{0}, nil); err != nil {
		t.Fatal(err)
	}
	if err := bus.Tx(1, nil, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := bus.Tx(1, []byte{0}, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if bus.SetSpeed(0) == nil {
		t.Fatal("0 is invalid")
	}
	if bus.SetSpeed(100*physic.MegaHertz+1) == nil {
		t.Fatal(">100MHz is invalid")
	}
	if err := bus.SetSpeed(physic.KiloHertz); err == nil || err.Error() != "sysfs-i2c: not supported" {
		t.Fatal(err)
	}
	defer func() {
		drvI2C.setSpeed = nil
	}()
	called := false
	if err := I2CSetSpeedHook(func(f physic.Frequency) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := bus.SetSpeed(physic.KiloHertz); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("i2c speed hook should have been called")
	}
	bus.SCL()
	bus.SDA()
	if !bus.hasRead {
		t.Fatal("hasRead")
	}
	if bus.brokenRead {
		t.Fatal("should not have brokenRead")
	}
	if err := bus.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestI2C_read_fallback(t *testing.T) {
	bus := I2C{f: &ioctlClose{ioctlErr: errors.New("ioctl err")}, busNumber: 24}
	if err := bus.Tx(1, nil, []byte{0}); err != nil {
		t.Fatal(err)
	}
	// The second time, a different code path is used.
	if err := bus.Tx(1, nil, []byte{0}); err != nil {
		t.Fatal(err)
	}
	if !bus.brokenRead {
		t.Fatal("should have brokenRead")
	}
	if err := bus.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestI2C_read_no_fallback(t *testing.T) {
	bus := I2C{f: &ioctlClose{ioctlErr: errors.New("ioctl err"), readErr: errors.New("read err")}, busNumber: 24}
	if bus.Tx(1, nil, []byte{0}) == nil {
		t.Fatal("expected failure")
	}
	if err := bus.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestI2C_functionality(t *testing.T) {
	expected := "I2C|10BIT_ADDR|PROTOCOL_MANGLING|SMBUS_PEC|NOSTART|SMBUS_BLOCK_PROC_CALL|SMBUS_QUICK|SMBUS_READ_BYTE|SMBUS_WRITE_BYTE|SMBUS_READ_BYTE_DATA|SMBUS_WRITE_BYTE_DATA|SMBUS_READ_WORD_DATA|SMBUS_WRITE_WORD_DATA|SMBUS_PROC_CALL|SMBUS_READ_BLOCK_DATA|SMBUS_WRITE_BLOCK_DATA|SMBUS_READ_I2C_BLOCK|SMBUS_WRITE_I2C_BLOCK"
	if s := functionality(0xFFFFFFFF).String(); s != expected {
		t.Fatal(s)
	}
}

func TestDriver_Init(t *testing.T) {
	d := driverI2C{}
	if _, err := d.Init(); err == nil {
		// It will fail on non-linux.
		defer func() {
			for _, name := range d.buses {
				if err := i2creg.Unregister(name); err != nil {
					t.Fatal(err)
				}
			}
		}()
		if len(d.buses) != 0 {
			// It may fail due to ACL.
			b, _ := i2creg.Open("")
			if b != nil {
				// If opening succeeded, closing must always succeed.
				if err := b.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if d.Prerequisites() != nil {
		t.Fatal("unexpected prerequisite")
	}
	if drvI2C.setSpeed != nil {
		t.Fatal("unexpected setSpeed")
	}
	defer func() {
		drvI2C.setSpeed = nil
	}()
	if I2CSetSpeedHook(nil) == nil {
		t.Fatal("must fail on nil hook")
	}
	if err := I2CSetSpeedHook(func(f physic.Frequency) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if I2CSetSpeedHook(func(f physic.Frequency) error { return nil }) == nil {
		t.Fatal("second I2CSetSpeedHook must fail")
	}
}

func BenchmarkI2C(b *testing.B) {
	b.ReportAllocs()
	i := ioctlClose{}
	bus := I2C{f: &i}
	var w [16]byte
	var r [16]byte
	for i := 0; i < b.N; i++ {
		if err := bus.Tx(0x01, w[:], r[:]); err != nil {
			b.Fatal(err)
		}
	}
}
