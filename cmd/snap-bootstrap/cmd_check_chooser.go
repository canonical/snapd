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
	"github.com/gvalkov/golang-evdev"
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

func findKeyboard(devices []*evdev.InputDevice) *evdev.InputDevice {
	// Find the first devices that has keys and the trigger key
	// and assume it's the keyboard. I guess we could even support
	// multiple keyboards but let's keep it simple for now.
	for _, dev := range devices {
		for _, cc := range dev.Capabilities[evKeyCapability] {
			if cc == chooserTriggerKey {
				return dev
			}
		}
	}
	return nil
}

func waitForKey(dev *evdev.InputDevice, keyCode uint16, ch chan error) {
	const triggerHeldCount = 20

	heldCount := uint(0)

	for {
		ies, err := dev.Read()
		if err != nil {
			ch <- err
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
			fmt.Printf("held: %v\n", heldCount)
			if heldCount >= triggerHeldCount {
				close(ch)
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
	dev := findKeyboard(devices)
	if dev == nil {
		return fmt.Errorf("cannot find keyboard")
	}
	fmt.Printf("* using keyboard: %s (%s)\n", dev.Fn, dev.Name)
	fmt.Printf("* waiting for key: %v\n", chooserTriggerKey.Name)

	// wait for a couple of second for the key
	detectKeyCh := make(chan error)
	go waitForKey(dev, uint16(chooserTriggerKey.Code), detectKeyCh)
	select {
	case err := <-detectKeyCh:
		if err == nil {
			// channel got closed without an error
			fmt.Printf("+ got key %v\n", chooserTriggerKey)
		}
		return err
	case <-time.After(5 * time.Second):
		fmt.Printf("- no key detected\n")
		return fmt.Errorf("interrupt key not detected")
	}

	return nil
}
