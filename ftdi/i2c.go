// Copyright 2017 The Periph Authors. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// This functionality requires MPSSE.
//
// Interfacing I²C:
// http://www.ftdichip.com/Support/Documents/AppNotes/AN_113_FTDI_Hi_Speed_USB_To_I2C_Example.pdf
//
// Implementation based on
// http://www.ftdichip.com/Support/Documents/AppNotes/AN_255_USB%20to%20I2C%20Example%20using%20the%20FT232H%20and%20FT201X%20devices.pdf
//
// Page 18: MPSSE does not automatically support clock stretching for I²C.

package ftdi

import (
	"context"
	"errors"
	"fmt"

	"periph.io/x/conn/v3"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/physic"
)

const i2cSCL = 1    // D0
const i2cSDAOut = 2 // D1
const i2cSDAIn = 4  // D2

type i2cBus struct {
	f      *FT232H
	pullUp bool
}

// Close stops I²C mode, returns to high speed mode, disable tri-state.
func (d *i2cBus) Close() error {
	d.f.mu.Lock()
	err := d.stopI2C()
	d.f.mu.Unlock()
	return err
}

// Duplex implements conn.Conn.
func (d *i2cBus) Duplex() conn.Duplex {
	return conn.Half
}

func (d *i2cBus) String() string {
	return d.f.String()
}

// SetSpeed implements i2c.Bus.
func (d *i2cBus) SetSpeed(f physic.Frequency) error {
	if f > 10*physic.MegaHertz {
		return fmt.Errorf("d2xx: invalid speed %s; maximum supported clock is 10MHz", f)
	}
	if f < 100*physic.Hertz {
		return fmt.Errorf("d2xx: invalid speed %s; minimum supported clock is 100Hz; did you forget to multiply by physic.KiloHertz?", f)
	}
	d.f.mu.Lock()
	defer d.f.mu.Unlock()
	_, err := d.f.h.MPSSEClock(f * 2 / 3)
	return err
}

// Tx implements i2c.Bus.
func (d *i2cBus) Tx(addr uint16, w, r []byte) error {
	d.f.mu.Lock()
	defer d.f.mu.Unlock()

	//defer d.setI2CLinesIdle() // エラーチェックしない

	var	cmdFull		[]byte
	var	cmd			[]byte

	cmd     = d.setI2CStart()
	cmdFull = append(cmdFull, cmd...)

	var byWrite		[]byte
	var byRead		[]byte
	var	iReadCnt	int
	var err error

	byWrite = append(byWrite, d.address_byte(addr, false))
	if (len(w) != 0) {
		byWrite = append(byWrite, w...)
	}

	cmd     = d.setI2CWriteBytes(byWrite)
	cmdFull = append(cmdFull, cmd...)
	iReadCnt = len(byWrite)

	if ((len(r) != 0) && (len(w) != 0)) { // len(w)はレジスタアドレス指定済みを判定するため
		cmd     = d.setI2CStop()
		cmdFull = append(cmdFull, cmd...)

		cmd     = d.setI2CLinesIdle()
		cmdFull = append(cmdFull, cmd...)

		cmd     = d.setI2CStart()
		cmdFull = append(cmdFull, cmd...)

		byRead   = append(byRead, d.address_byte(addr, true))
		cmd      = d.setI2CWriteBytes(byRead)
		cmdFull  = append(cmdFull, cmd...)
		iReadCnt += len(byRead)

		cmd      = d.setI2CReadBytes(len(r))
		cmdFull  = append(cmdFull, cmd...)
		iReadCnt += len(r)
	}

	cmd     = d.setI2CStop()
	cmdFull = append(cmdFull, cmd...)

	err = d.transactionEnd(cmdFull, iReadCnt, r)
	if (nil != err){
		return err
	}

	return nil
}

// SCL implements i2c.Pins.
func (d *i2cBus) SCL() gpio.PinIO {
	return d.f.D0
}

// SDA implements i2c.Pins.
func (d *i2cBus) SDA() gpio.PinIO {
	return d.f.D1
}

// setupI2C initializes the MPSSE to the state to run an I²C transaction.
//
// Defaults to 400kHz.
//
// When pullUp is true; output alternates between Out(Low) and In(PullUp).
//
// when pullUp is false; pins are set in Tristate so Out(High) becomes float
// instead of drive High. Low still drives low. That's called open collector.
func (d *i2cBus) setupI2C(pullUp bool) error {
	if pullUp {
		return errors.New("d2xx: PullUp will soon be implemented")
	}
	// TODO(maruel): We could set these only *during* the I²C operation, which
	// would make more sense.
	f := 400 * physic.KiloHertz
	clk := ((30 * physic.MegaHertz / f) - 1) * 2 / 3

	var cmd []byte
	cmd = append(cmd,
		clock30MHz,              // 0x8A; Disable clock divide-by-5 for 60Mhz master clock
		clockNormal,             // 0x97; Ensure adaptive clocking is off
		clock3Phase,             // 0x8C; Enable 3 phase data clocking, data valid on both clock edges for I2C
		dataTristate,            // 0x9E; Enable drive-zero mode on the lines used for I2C ...
		0x07,                    // 0x07; ... on the bits AD0, 1 and 2 of the lower port...
		0x00,                    // 0x00; ...not required on the upper port AC 0-7
		internalLoopbackDisable, // 0x85; Ensure internal loopback is off
	)

	cmd = append(cmd,
		clockSetDivisor,
		byte(clk),
		byte(clk>>8),
	)

	//cmd := buf[:4]
	//if !d.pullUp {
	//	// TODO(maruel): Do not mess with other GPIOs tristate.
	//	cmd = append(cmd, dataTristate, 7, 0)
	//}
	if _, err := d.f.h.Write(cmd); err != nil {
		return err
	}
	d.f.usingI2C = true
	d.pullUp = pullUp

	cmd = d.setI2CLinesIdle()
	cmd = append(cmd, flush)
	if _, err := d.f.h.Write(cmd); err != nil {
		return err
	}
	return nil
}

// stopI2C resets the MPSSE to a more "normal" state.
func (d *i2cBus) stopI2C() error {
	// Resets to 30MHz.
	buf := [4 + 3]byte{
		clock2Phase,
		clock30MHz, 0, 0,
	}
	cmd := buf[:4]
	if !d.pullUp {
		// TODO(maruel): Do not mess with other GPIOs tristate.
		cmd = append(cmd, dataTristate, 0, 0)
	}
	_, err := d.f.h.Write(cmd)
	d.f.usingI2C = false
	return err
}

// setI2CLinesIdle sets all D0 and D1 lines high.
//
// Does not touch D3~D7.
func (d *i2cBus) setI2CLinesIdle() ([]byte) {
	const mask = 0xFF &^ (i2cSCL | i2cSDAOut | i2cSDAIn)
	// TODO(maruel): d.pullUp
	d.f.dbus.direction = d.f.dbus.direction&mask | i2cSCL | i2cSDAOut
	//d.f.dbus.value = d.f.dbus.value & mask
	cmd := []byte{
		//gpioSetD, d.f.dbus.value | i2cSCL | i2cSDAOut, d.f.dbus.direction,
		gpioSetD, i2cSCL | i2cSDAOut, d.f.dbus.direction,
		gpioSetD, i2cSCL | i2cSDAOut, d.f.dbus.direction,
		gpioSetD, i2cSCL | i2cSDAOut, d.f.dbus.direction,
		gpioSetD, i2cSCL | i2cSDAOut, d.f.dbus.direction,

		//flush,
	}
	//_, err := d.f.h.Write(cmd[:])
	return cmd
}

// setI2CStart starts an I²C transaction.
//
// Does not touch D3~D7.
func (d *i2cBus) setI2CStart() ([]byte) {
	// TODO(maruel): d.pullUp
	dir := d.f.dbus.direction
	//v := d.f.dbus.value
	// Assumes last setup was d.setI2CLinesIdle(), e.g. D0 and D1 are high, so
	// skip this.
	//
	// Runs the command 4 times as a way to delay execution.
	cmd := []byte{
		// SCL high, SDA low for 600ns
		//gpioSetD, v | i2cSCL, dir,
		gpioSetD, i2cSCL, dir,
		gpioSetD, i2cSCL, dir,
		gpioSetD, i2cSCL, dir,
		gpioSetD, i2cSCL, dir,

		// SCL low, SDA low
		//gpioSetD, v, dir,
		gpioSetD, 0x00, dir,
		gpioSetD, 0x00, dir,
		gpioSetD, 0x00, dir,
		gpioSetD, 0x00, dir,

		//gpioSetC, 0xFB, 0x40,	//LED setting?
	}
	//_, err := d.f.h.Write(cmd[:])
	//return err
	return cmd
}

// setI2CStop completes an I²C transaction.
//
// Does not touch D3~D7.
func (d *i2cBus) setI2CStop() ([]byte) {
	// TODO(maruel): d.pullUp
	dir := d.f.dbus.direction
	//v := d.f.dbus.value
	// Runs the command 4 times as a way to delay execution.
	cmd := []byte{
		// SCL low, SDA low
		//gpioSetD, v, dir,
		gpioSetD, 0x00, dir,
		gpioSetD, 0x00, dir,
		gpioSetD, 0x00, dir,
		gpioSetD, 0x00, dir,

		// SCL high, SDA low
		//gpioSetD, v | i2cSCL, dir,
		gpioSetD, i2cSCL, dir,
		gpioSetD, i2cSCL, dir,
		gpioSetD, i2cSCL, dir,
		gpioSetD, i2cSCL, dir,

		// SCL high, SDA high
		//gpioSetD, v | i2cSCL | i2cSDAOut, dir,
		gpioSetD, i2cSCL | i2cSDAOut, dir,
		gpioSetD, i2cSCL | i2cSDAOut, dir,
		gpioSetD, i2cSCL | i2cSDAOut, dir,
		gpioSetD, i2cSCL | i2cSDAOut, dir,
	}

	return cmd
}

func (d *i2cBus) setI2CWriteBytes(w []byte) ([]byte) {
	// TODO(maruel): d.pullUp
	dir := d.f.dbus.direction
	//v := d.f.dbus.value

	var cmdfull []byte

	// TODO(maruel): Implement both with and without NAK check.
	cmd1 := []byte{
		// Data out, the 0 will be replaced with the byte.
		dataOut | dataOutFall, 0, 0, //0,
	}

	cmd2 := []byte{
		// Set back to idle.
		//gpioSetD, v | i2cSDAOut, dir,
		gpioSetD, i2cSDAOut, dir,
		gpioSetD, i2cSDAOut, dir,
		gpioSetD, i2cSDAOut, dir,
		gpioSetD, i2cSDAOut, dir,

		// Read ACK/NAK.
		dataIn | dataBit, 0,
	}

	for _, c := range w {
		cmdfull = append(cmdfull, cmd1...)
		cmdfull = append(cmdfull, c)
		cmdfull = append(cmdfull, cmd2...)
	}

	return cmdfull
}

func (d *i2cBus) setI2CReadBytes(setCnt int) ([]byte) {
	// TODO(maruel): d.pullUp
	dir := d.f.dbus.direction
	//v := d.f.dbus.value

	cmd1 := []byte{
		// Read 8 bits.
		//dataIn | dataBit, 0, 0,				// 0x22, 0x00, 0x00
		dataIn, 0, 0		,					// 0x20, 0x00, 0x00

		// Send ACK/NAK.
		dataOut | dataOutFall | dataBit, 0,		// 0x13, 0x00
	}

	cmd2 := []byte{
		// Set back to idle.
		//gpioSetD, v | i2cSCL | i2cSDAOut, dir,	// 0x80, 0x03, 0x03
		//gpioSetD, v | i2cSDAOut, dir,	// 0x80, 0x02, 0x03
		gpioSetD, i2cSDAOut, dir,		// 0x80, 0x02, 0x03
		// Force read buffer flush. This is only necessary if NAK are not ignored.
	}

	var cmdfull []byte

	for iCnt := 0; iCnt < setCnt; iCnt ++ {
		cmdfull = append(cmdfull, cmd1...)
		if (iCnt != (setCnt - 1)) { // 最終データでないか?
			cmdfull = append(cmdfull, 0x00) // ACK
		} else {
			cmdfull = append(cmdfull, 0xFF) // NAK (0x80?)
		}
		cmdfull = append(cmdfull, cmd2...)
	}

	return cmdfull
}

func (d *i2cBus) transactionEnd(w []byte, readCnt int, r []byte) (error) {
	// TODO(maruel): WAT?
	var	err		error
	err = d.f.h.Flush()
	if (nil != err) {
		return err
	}

	readBuff := make([]byte, readCnt)

	var cmdfull []byte
	cmdfull = append(cmdfull, w...)
	cmdfull = append(cmdfull, flush)

	_, err = d.f.h.Write(cmdfull[:])
	if (nil != err) {
		return err
	}

	_, err = d.f.h.ReadAll(context.Background(), readBuff[:])
	if (nil != err) {
		return err
	}

	// verify acks
	var	iCnt		int
	for iCnt = 0; iCnt < (readCnt - len(r)); iCnt ++ {
		if (readBuff[iCnt] & 0x01) != 0 {
			return errors.New("got NAK")
		}
	}

	// set Recv Data
	for iCnt = 0; iCnt < len(r); iCnt ++ {
		r[iCnt] = readBuff[(readCnt - len(r)) + iCnt]
	}

	return nil
}

func (d *i2cBus) address_byte(uiAddr uint16, bRead bool) byte {
	var byAddr byte

	if (bRead == true) {
		byAddr = byte((uiAddr << 1) | 0x01)
	} else {
		byAddr = byte((uiAddr << 1) & 0xFE)
	}

	return byAddr
}

// writeBytes writes multiple bytes within an I²C transaction.
//
// Does not touch D3~D7.
func (d *i2cBus) writeBytes(w []byte) error {
	// TODO(maruel): d.pullUp
	dir := d.f.dbus.direction
	v := d.f.dbus.value
	//v := byte(0x00)
	// TODO(maruel): WAT?
	if err := d.f.h.Flush(); err != nil {
		return err
	}

	readBuff := make([]byte, len(w))
	var cmdfull []byte

	// TODO(maruel): Implement both with and without NAK check.
	cmd1 := []byte{
		// Data out, the 0 will be replaced with the byte.
		dataOut | dataOutFall, 0, 0, //0,
	}

	cmd2 := []byte{
		// Set back to idle.
		//gpioSetD, v | i2cSCL | i2cSDAOut, dir,
		gpioSetD, v | i2cSDAOut, dir,
		gpioSetD, v | i2cSDAOut, dir,
		gpioSetD, v | i2cSDAOut, dir,
		gpioSetD, v | i2cSDAOut, dir,

		// Read ACK/NAK.
		dataIn | dataBit, 0,
	}

	// setI2CStop 相当
	cmd3 := []byte{
		//gpioSetD, v, dir,
		//gpioSetD, v, dir,
		//gpioSetD, v, dir,
		//gpioSetD, v, dir,

		//gpioSetD, v | i2cSCL, dir,
		//gpioSetD, v | i2cSCL, dir,
		//gpioSetD, v | i2cSCL, dir,
		//gpioSetD, v | i2cSCL, dir,

		//gpioSetD, v | i2cSCL | i2cSDAOut, dir,
		//gpioSetD, v | i2cSCL | i2cSDAOut, dir,
		//gpioSetD, v | i2cSCL | i2cSDAOut, dir,
		//gpioSetD, v | i2cSCL | i2cSDAOut, dir,

		flush,
	}

	for _, c := range w {
		cmdfull = append(cmdfull, cmd1...)
		cmdfull = append(cmdfull, c)
		cmdfull = append(cmdfull, cmd2...)
	}
	cmdfull = append(cmdfull, cmd3...)

	if _, err := d.f.h.Write(cmdfull[:]); err != nil {
		return err
	}
	if _, err := d.f.h.ReadAll(context.Background(), readBuff[:]); err != nil {
		return err
	}
	//if r[0]&1 == 0 {
	//	return errors.New("got NAK")
	//}

	for _, rcv := range readBuff {
		if (rcv & 0x01) != 0 {
			return errors.New("got NAK")
		}
	}

	return nil
}

// readBytes reads multiple bytes within an I²C transaction.
//
// Does not touch D3~D7.
func (d *i2cBus) readBytes(r []byte) error {
	// TODO(maruel): d.pullUp
	dir := d.f.dbus.direction
	v := d.f.dbus.value

	//cmd := [...]byte{
	//// Read 8 bits.
	//dataIn | dataBit, 7,
	//// Send ACK/NAK.
	//dataOut | dataOutFall | dataBit, 0, 0,
	//// Set back to idle.
	//gpioSetD, v | i2cSCL | i2cSDAOut, dir,
	//// Force read buffer flush. This is only necessary if NAK are not ignored.
	//flush,
	//}

	cmd1 := []byte{
		// Read 8 bits.
		dataIn | dataBit, 7,

		// Send ACK/NAK.
		dataOut | dataOutFall | dataBit, 0,
	}

	cmd2 := []byte{
		// Set back to idle.
		gpioSetD, v | i2cSCL | i2cSDAOut, dir,
		// Force read buffer flush. This is only necessary if NAK are not ignored.
	}

	cmd3 := []byte{
		flush,
	}

	var cmdfull []byte
	for iCnt := range r {
		cmdfull = append(cmdfull, cmd1...)
		if iCnt != (len(r) - 1) { // 最終データでないか?
			cmdfull = append(cmdfull, 0x00) // ACK
		} else {
			cmdfull = append(cmdfull, 0xFF) // NAK (0x80?)
		}
		cmdfull = append(cmdfull, cmd2...)
	}
	cmdfull = append(cmdfull, cmd3...)

	if _, err := d.f.h.Write(cmdfull[:]); err != nil {
		return err
	}
	if _, err := d.f.h.ReadAll(context.Background(), r[:]); err != nil {
		return err
	}
	return nil
}

var _ i2c.BusCloser = &i2cBus{}
var _ i2c.Pins = &i2cBus{}
