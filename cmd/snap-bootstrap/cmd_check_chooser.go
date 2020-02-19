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

package main

import (
	"fmt"
	"time"

	// TODO:UC20: not packaged, reimplment the minimal things we need?
	evdev "github.com/gvalkov/golang-evdev"

	"github.com/snapcore/snapd/logger"
)

func init() {
	const (
		short = "Check if the chooser should be run"
		long  = ""
	)

	if _, err := parser.AddCommand("check-chooser", short, long, &cmdCheckChooser{}); err != nil {
		panic(err)
	}
}

type cmdCheckChooser struct{}

func (c *cmdCheckChooser) Execute(args []string) error {
	// TODO:UC20: check in the gadget if there is a hook or some
	// binary we should run for chooser detection/display. This
	// will require some design work and also thinking if/how such
	// a hook can be confined.

	return checkChooserTriggerKey()
}

var (
	evKeyCapability = evdev.CapabilityType{Type: evdev.EV_KEY, Name: "EV_KEY"}

	// chooserTriggerKey is what activates the chooser when held down
	chooserTriggerKey = evdev.CapabilityCode{Code: 2, Name: "KEY_1"}
)

func findKeyboards(devices []*evdev.InputDevice) []*evdev.InputDevice {
	// Find the first devices that has keys and the trigger key
	// and assume it's the keyboard. I guess we could even support
	// multiple keyboards but let's keep it simple for now.
	var devs []*evdev.InputDevice
	for _, dev := range devices {
		for _, cc := range dev.Capabilities[evKeyCapability] {
			if cc == chooserTriggerKey {
				logger.Noticef("keyboard: %s", dev.String())
				devs = append(devs, dev)
			}
		}
	}
	return devs
}

type keyEvent struct {
	dev *evdev.InputDevice
	err error
}

func waitForKey(dev *evdev.InputDevice, keyCode uint16, ch chan keyEvent) {
	const triggerHeldCount = 20

	heldCount := uint(0)

	for {
		ies, err := dev.Read()
		if err != nil {
			ch <- keyEvent{err: err, dev: dev}
		}
		for _, ie := range ies {
			if ie.Type != evdev.EV_KEY || ie.Code != keyCode {
				continue
			}
			kev := evdev.NewKeyEvent(&ie)
			switch kev.State {
			case evdev.KeyHold, evdev.KeyDown:
				heldCount++
			case evdev.KeyUp:
				heldCount = 0
			}
			logger.Noticef("%s held: %v", dev.Phys, heldCount)
			if heldCount >= triggerHeldCount {
				ch <- keyEvent{dev: dev}
			}
		}
	}
}

func checkChooserTriggerKey() error {
	// XXX: close unneeded input devices again?
	devices, err := evdev.ListInputDevices()
	if err != nil {
		return fmt.Errorf("cannot list input devices: %v", err)
	}
	keyDevs := findKeyboards(devices)
	if keyDevs == nil {
		return fmt.Errorf("cannot find keyboards")
	}
	logger.Noticef("waiting for key: %v", chooserTriggerKey.Name)

	// wait for a couple of second for the key
	detectKeyCh := make(chan keyEvent, len(keyDevs))
	for _, kbd := range keyDevs {
		go waitForKey(kbd, uint16(chooserTriggerKey.Code), detectKeyCh)
	}
	select {
	case kev := <-detectKeyCh:
		if kev.err == nil {
			// channel got closed without an error
			logger.Noticef("%s: + got key %v", kev.dev.Phys, chooserTriggerKey)
		}
		return err
	case <-time.After(5 * time.Second):
		logger.Noticef("- no key detected")
		return fmt.Errorf("interrupt key not detected")
	}

	return nil
}
