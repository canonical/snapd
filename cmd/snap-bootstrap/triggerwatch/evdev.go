// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package triggerwatch

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	// TODO:UC20: not packaged, reimplement the minimal things we need?
	"github.com/ddkwork/golibrary/mylog"
	evdev "github.com/gvalkov/golang-evdev"

	"github.com/snapcore/snapd/logger"
)

type keyEvent struct {
	Dev triggerDevice
	Err error
}

type triggerEventFilter struct {
	Key string
}

var (
	strToKey = map[string]int{
		"KEY_ESC": evdev.KEY_ESC,
		"KEY_1":   evdev.KEY_1,
		"KEY_2":   evdev.KEY_2,
		"KEY_3":   evdev.KEY_3,
		"KEY_4":   evdev.KEY_4,
		"KEY_5":   evdev.KEY_5,
		"KEY_6":   evdev.KEY_6,
		"KEY_7":   evdev.KEY_7,
		"KEY_8":   evdev.KEY_8,
		"KEY_9":   evdev.KEY_9,
		"KEY_0":   evdev.KEY_0,
	}
	evKeyCapability = evdev.CapabilityType{Type: evdev.EV_KEY, Name: "EV_KEY"}

	// hold time needed to trigger the event
	holdToTrigger = 2 * time.Second
)

func init() {
	trigger = &evdevInput{}
}

type evdevKeyboardInputDevice struct {
	keyCode uint16
	dev     *evdev.InputDevice
}

func (e *evdevKeyboardInputDevice) probeKeyState() (bool, error) {
	// XXX: evdev defines EVIOCGKEY using MAX_NAME_SIZE which is larger than
	// what is needed to store the key bitmap with KEY_MAX bits, but we need
	// to play along since the value is already encoded
	keyBitmap := new([evdev.MAX_NAME_SIZE]byte)

	// obtain the large bitmap with all key states
	// https://elixir.bootlin.com/linux/v5.5.5/source/drivers/input/evdev.c#L1163
	_, _ := mylog.Check3(syscall.RawSyscall(syscall.SYS_IOCTL, e.dev.File.Fd(), uintptr(evdev.EVIOCGKEY), uintptr(unsafe.Pointer(keyBitmap))))
	if err != 0 {
		return false, err
	}
	byteIdx := e.keyCode / 8
	keyMask := byte(1 << (e.keyCode % 8))
	isDown := keyBitmap[byteIdx]&keyMask != 0
	return isDown, nil
}

func (e *evdevKeyboardInputDevice) WaitForTrigger(ch chan keyEvent) {
	logger.Noticef("%s: starting wait, hold %s to trigger", e, holdToTrigger)

	// XXX: do not mess with setting the key repeat rate, as it's cumbersome
	// and golang-evdev SetRepeatRate() parameter order is actually reversed
	// wrt. what the kernel does. The evdev interprets EVIOCSREP arguments
	// as (delay, repeat)
	// https://elixir.bootlin.com/linux/latest/source/drivers/input/evdev.c#L1072
	// but the wrapper is passing is passing (repeat, delay)
	// https://github.com/gvalkov/golang-evdev/blob/287e62b94bcb850ab42e711bd74b2875da83af2c/device.go#L226-L230

	keyDown := mylog.Check2(e.probeKeyState())

	if keyDown {
		// looks like the key is pressed initially, we don't know when
		// that happened, but pretend it happened just now
		logger.Noticef("%s: key is already down", e)
	}

	type evdevEvent struct {
		kev *evdev.KeyEvent
		err error
	}

	// buffer large enough to collect some events
	evChan := make(chan evdevEvent, 10)

	monitorKey := func() {
		for {
			ies := mylog.Check2(e.dev.Read())

			for _, ie := range ies {
				if ie.Type != evdev.EV_KEY || ie.Code != e.keyCode {
					continue
				}
				kev := evdev.NewKeyEvent(&ie)
				evChan <- evdevEvent{kev: kev}
			}
		}
		close(evChan)
	}

	go monitorKey()

	holdTimer := time.NewTimer(holdToTrigger)
	// no sense to keep it running later either
	defer holdTimer.Stop()

	if !keyDown {
		// key isn't held yet, stop the timer
		holdTimer.Stop()
	}

	// invariant: tholdTimer is running iff keyDown is true, otherwise is stopped
Loop:
	for {
		select {
		case ev := <-evChan:
			if ev.err != nil {
				holdTimer.Stop()
				ch <- keyEvent{Err: ev.err, Dev: e}
				break Loop
			}
			kev := ev.kev
			switch kev.State {
			case evdev.KeyDown:
				if keyDown {
					// unexpected, but possible if we missed
					// a key up event right after checking
					// the initial keyboard state when the
					// key was still down
					if !holdTimer.Stop() {
						// drain the channel before the
						// timer gets reset
						<-holdTimer.C
					}
				}
				keyDown = true
				// timer is stopped at this point
				holdTimer.Reset(holdToTrigger)
				logger.Noticef("%s: trigger key down", e)
			case evdev.KeyHold:
				if !keyDown {
					keyDown = true
					// timer is not running yet at this point
					holdTimer.Reset(holdToTrigger)
					logger.Noticef("%s: unexpected hold without down", e)
				}
			case evdev.KeyUp:
				// no need to drain the channel, if it expired,
				// we'll handle it in next iteration
				holdTimer.Stop()
				keyDown = false
				logger.Noticef("%s: trigger key up", e)
			}
		case <-holdTimer.C:
			logger.Noticef("%s: hold complete", e)
			ch <- keyEvent{Dev: e}
			break Loop
		}
	}
}

func (e *evdevKeyboardInputDevice) String() string {
	return fmt.Sprintf("%s: %s", e.dev.Phys, e.dev.Name)
}

func (e *evdevKeyboardInputDevice) Close() {
	e.dev.File.Close()
}

type evdevInput struct{}

func getCapabilityCode(Key string) (evdev.CapabilityCode, error) {
	keyCode, ok := strToKey[Key]
	if !ok {
		return evdev.CapabilityCode{}, fmt.Errorf("cannot find a key matching the filter %q", Key)
	}
	return evdev.CapabilityCode{Code: keyCode, Name: Key}, nil
}

func matchDevice(cap evdev.CapabilityCode, dev *evdev.InputDevice) triggerDevice {
	for _, cc := range dev.Capabilities[evKeyCapability] {
		if cc == cap {
			return &evdevKeyboardInputDevice{
				dev:     dev,
				keyCode: uint16(cap.Code),
			}
		}
	}
	return nil
}

func (e *evdevInput) Open(filter triggerEventFilter, node string) (triggerDevice, error) {
	evdevDevice := mylog.Check2(evdev.Open(node))

	cap := mylog.Check2(getCapabilityCode(filter.Key))

	return matchDevice(cap, evdevDevice), nil
}

func (e *evdevInput) FindMatchingDevices(filter triggerEventFilter) ([]triggerDevice, error) {
	devices := mylog.Check2(evdev.ListInputDevices())

	// NOTE: this supports so far only key input devices
	cap := mylog.Check2(getCapabilityCode(filter.Key))

	// collect all input devices that can emit the trigger key
	var devs []triggerDevice
	for _, dev := range devices {
		idev := matchDevice(cap, dev)
		if idev != nil {
			devs = append(devs, idev)
		} else {
			defer dev.File.Close()
		}
	}
	return devs, nil
}
