// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package hotplug

import (
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type udevadmSuite struct {
	testutil.BaseTest
}

var _ = Suite(&udevadmSuite{})

func (s *udevadmSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *udevadmSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *udevadmSuite) TestParsingHappy(c *C) {
	cmd := testutil.MockCommand(c, udevadmBin, `#!/bin/sh
cat << __END__
P: /devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0
E: DEVPATH=/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0
E: DRIVER=ftdi_sio
E: SUBSYSTEM=usb-serial

P: /devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0
N: ttyUSB0
S: serial/by-id/usb-FTDI_FT232R_USB_UART_AH06W0EQ-if00-port0
S: serial/by-path/pci-0000:00:14.0-usb-0:2:1.0-port0
E: DEVLINKS=/dev/serial/by-path/pci-0000:00:14.0-usb-0:2:1.0-port0 /dev/serial/by-id/usb-FTDI_FT232R_USB_UART_AH06W0EQ-if00-port0
E: DEVNAME=/dev/ttyUSB0
E: DEVPATH=/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0
E: ID_BUS=usb
E: ID_MODEL=FT232R_USB_UART
E: ID_MODEL_FROM_DATABASE=FT232 Serial (UART) IC
E: ID_MODEL_ID=6001
E: ID_PATH=pci-0000:00:14.0-usb-0:2:1.0
E: ID_PATH_TAG=pci-0000_00_14_0-usb-0_2_1_0
E: ID_PCI_CLASS_FROM_DATABASE=Serial bus controller
E: ID_PCI_INTERFACE_FROM_DATABASE=XHCI
E: ID_PCI_SUBCLASS_FROM_DATABASE=USB controller
E: ID_REVISION=0600
E: ID_SERIAL=FTDI_FT232R_USB_UART_AH06W0EQ
E: ID_SERIAL_SHORT=AH06W0EQ
E: ID_TYPE=generic
E: ID_USB_DRIVER=ftdi_sio
E: ID_USB_INTERFACES=:ffffff:
E: ID_USB_INTERFACE_NUM=00
E: ID_VENDOR=FTDI
E: ID_VENDOR_FROM_DATABASE=Future Technology Devices International, Ltd
E: ID_VENDOR_ID=0403
E: MAJOR=188
E: MINOR=0
E: SUBSYSTEM=tty
E: TAGS=:systemd:
__END__
`)
	defer cmd.Restore()

	devs := make(chan *HotplugDeviceInfo)
	errors := make(chan error)
	c.Assert(EnumerateExistingDevices(devs, errors), IsNil)

	devices := []*HotplugDeviceInfo{}
	var stop bool
	for !stop {
		select {
		case err := <-errors:
			c.Fatalf("unexpected error: %s", err)
		case dev, ok := <-devs:
			if !ok {
				stop = true
				break
			}
			devices = append(devices, dev)
		}
	}

	_, ok := <-errors
	c.Assert(ok, Equals, false)

	c.Assert(devices, HasLen, 2)

	v, _ := devices[0].Attribute("DEVPATH")
	c.Assert(v, Equals, "/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0")

	v, _ = devices[1].Attribute("DEVPATH")
	c.Assert(v, Equals, "/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0")

	v, _ = devices[1].Attribute("MAJOR")
	c.Assert(v, Equals, "188")

	v, _ = devices[1].Attribute("TAGS")
	c.Assert(v, Equals, ":systemd:")
}

func (s *udevadmSuite) TestParsingError(c *C) {
	cmd := testutil.MockCommand(c, udevadmBin, `#!/bin/sh
cat << __END__
P: /devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0
E: DEVPATH

E: K=V

P: /devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0
E: DEVPATH=foo
__END__
`)
	defer cmd.Restore()

	devs := make(chan *HotplugDeviceInfo)
	errors := make(chan error)
	c.Assert(EnumerateExistingDevices(devs, errors), IsNil)

	var parseErrors []error
	devices := []*HotplugDeviceInfo{}

	var stop bool
	for !stop {
		select {
		case e, ok := <-errors:
			if ok {
				parseErrors = append(parseErrors, e)
			}
		case dev, ok := <-devs:
			if !ok {
				stop = true
			} else {
				devices = append(devices, dev)
			}
		}
	}

	_, ok := <-errors
	c.Assert(ok, Equals, false)

	c.Assert(parseErrors, HasLen, 2)
	c.Assert(parseErrors[0], ErrorMatches, `failed to parse udevadm output "E: DEVPATH"`)
	c.Assert(parseErrors[1], ErrorMatches, `no device block marker found before "E: K=V"`)

	// successfully parsed devices are still reported
	c.Assert(devices, HasLen, 1)
	v, _ := devices[0].Attribute("DEVPATH")
	c.Assert(v, Equals, "foo")
}
