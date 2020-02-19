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

package inputwatch

import (
	"fmt"

	// TODO:UC20: not packaged, reimplment the minimal things we need?
	evdev "github.com/gvalkov/golang-evdev"

	"github.com/snapcore/snapd/logger"
)

var (
	evKeyCapability = evdev.CapabilityType{Type: evdev.EV_KEY, Name: "EV_KEY"}

	// chooserTriggerKey is what activates the chooser when held down
	chooserTriggerKey = evdev.CapabilityCode{Code: 2, Name: "KEY_1"}
)

func init() {
	input = &evdevInput{}
}

type evdevKeyboardInputDevice struct {
	keyCode uint16
	dev     *evdev.InputDevice
}

func (e *evdevKeyboardInputDevice) WaitForTrigger(ch chan KeyEvent) {
	const triggerHeldCount = 20

	heldCount := uint(0)

	// TODO: find a reliable way to get/set key repeat rate? EVIOCGREP seems
	// to return bad info; the support varies from driver to driver,
	// gpio-keys may require 'autorepeat' enabled at the devicetree level
	e.dev.SetRepeatRate(1000, 0)

	for {
		ies, err := e.dev.Read()
		if err != nil {
			ch <- KeyEvent{Err: err, Dev: e}
		}
		for _, ie := range ies {
			if ie.Type != evdev.EV_KEY || ie.Code != e.keyCode {
				continue
			}
			kev := evdev.NewKeyEvent(&ie)
			switch kev.State {
			case evdev.KeyHold, evdev.KeyDown:
				heldCount++
			case evdev.KeyUp:
				heldCount = 0
			}
			logger.Noticef("%s held: %v", e.dev.Phys, heldCount)
			if heldCount >= triggerHeldCount {
				ch <- KeyEvent{Dev: e}
			}
		}
	}
}

func (e *evdevKeyboardInputDevice) String() string {
	return fmt.Sprintf("%s: %s", e.dev.Phys, e.dev.Name)
}

type evdevInput struct{}

func (e *evdevInput) FindMatchingDevices(filter InputCapabilityFilter) ([]InputDevice, error) {
	devices, err := evdev.ListInputDevices()
	if err != nil {
		return nil, fmt.Errorf("cannot list input devices: %v", err)
	}

	// XXX: this is set up to support key input only
	kc, ok := strToKey[filter.Key]
	if !ok {
		return nil, fmt.Errorf("cannot find a key matching the filter %q", filter.Key)
	}
	// chooserTriggerKey is what activates the chooser when held down
	cap := evdev.CapabilityCode{Code: kc, Name: filter.Key}

	match := func(dev *evdev.InputDevice) InputDevice {
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
	// Find the first devices that has keys and the trigger key
	// and assume it's the keyboard. I guess we could even support
	// multiple keyboards but let's keep it simple for now.
	var devs []InputDevice
	for _, dev := range devices {
		idev := match(dev)
		if idev != nil {
			devs = append(devs, idev)
		} else {
			defer dev.File.Close()
		}
	}
	return devs, nil
}

var strToKey = map[string]int{
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
