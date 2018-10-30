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

package ifacestate_test

import (
	"crypto/sha256"
	"fmt"

	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
)

type hotplugSuite struct{}

var _ = Suite(&hotplugSuite{})

func keyHelper(input string) string {
	return fmt.Sprintf("0%x", sha256.Sum256([]byte(input)))
}

func (s *hotplugSuite) TestDefaultDeviceKey(c *C) {
	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":        "a/path",
		"ACTION":         "add",
		"SUBSYSTEM":      "foo",
		"ID_V4L_PRODUCT": "v4lproduct",
		"NAME":           "name",
		"ID_VENDOR_ID":   "vendor",
		"ID_MODEL_ID":    "model",
		"ID_SERIAL":      "serial",
		"ID_REVISION":    "revision",
	})
	c.Assert(err, IsNil)
	key, err := ifacestate.DefaultDeviceKey(di, 0)
	c.Assert(err, IsNil)

	// sanity check
	c.Check(key, HasLen, 65)
	c.Check(key, Equals, "08bcbdcda3fee3534c0288506d9b75d4e26fe3692a36a11e75d05eac9ebf5ca7d")
	c.Assert(key, Equals, keyHelper("ID_V4L_PRODUCT\x00v4lproduct\x00ID_VENDOR_ID\x00vendor\x00ID_MODEL_ID\x00model\x00ID_SERIAL\x00serial\x00"))

	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":      "a/path",
		"ACTION":       "add",
		"SUBSYSTEM":    "foo",
		"NAME":         "name",
		"ID_WWN":       "wnn",
		"ID_MODEL_ENC": "modelenc",
		"ID_REVISION":  "revision",
	})
	c.Assert(err, IsNil)
	key, err = ifacestate.DefaultDeviceKey(di, 0)
	c.Assert(err, IsNil)
	c.Assert(key, Equals, keyHelper("NAME\x00name\x00ID_WWN\x00wnn\x00ID_MODEL_ENC\x00modelenc\x00ID_REVISION\x00revision\x00"))

	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":       "a/path",
		"ACTION":        "add",
		"SUBSYSTEM":     "foo",
		"PCI_SLOT_NAME": "pcislot",
		"ID_MODEL_ENC":  "modelenc",
	})
	c.Assert(err, IsNil)
	key, err = ifacestate.DefaultDeviceKey(di, 0)
	c.Assert(key, Equals, keyHelper("PCI_SLOT_NAME\x00pcislot\x00ID_MODEL_ENC\x00modelenc\x00"))
	c.Assert(err, IsNil)

	// real device #1 - Lime SDR device
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVNAME":                 "/dev/bus/usb/002/002",
		"DEVNUM":                  "002",
		"DEVPATH":                 "/devices/pci0000:00/0000:00:14.0/usb2/2-3",
		"DEVTYPE":                 "usb_device",
		"DRIVER":                  "usb",
		"ID_BUS":                  "usb",
		"ID_MODEL":                "LimeSDR-USB",
		"ID_MODEL_ENC":            "LimeSDR-USB",
		"ID_MODEL_FROM_DATABASE":  "Myriad-RF LimeSDR",
		"ID_MODEL_ID":             "6108",
		"ID_REVISION":             "0000",
		"ID_SERIAL":               "Myriad-RF_LimeSDR-USB_0009060B00492E2C",
		"ID_SERIAL_SHORT":         "0009060B00492E2C",
		"ID_USB_INTERFACES":       ":ff0000:",
		"ID_VENDOR":               "Myriad-RF",
		"ID_VENDOR_ENC":           "Myriad-RF",
		"ID_VENDOR_FROM_DATABASE": "OpenMoko, Inc.",
		"ID_VENDOR_ID":            "1d50",
		"MAJOR":                   "189",
		"MINOR":                   "129",
		"PRODUCT":                 "1d50/6108/0",
		"SUBSYSTEM":               "usb",
		"TYPE":                    "0/0/0",
		"USEC_INITIALIZED":        "6125378086 ",
	})
	c.Assert(err, IsNil)
	key, err = ifacestate.DefaultDeviceKey(di, 0)
	c.Assert(err, IsNil)
	c.Assert(key, Equals, keyHelper("ID_VENDOR_ID\x001d50\x00ID_MODEL_ID\x006108\x00ID_SERIAL\x00Myriad-RF_LimeSDR-USB_0009060B00492E2C\x00"))

	// real device #2 - usb-serial port adapter
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVLINKS":                       "/dev/serial/by-id/usb-FTDI_FT232R_USB_UART_AH06W0EQ-if00-port0 /dev/serial/by-path/pci-0000:00:14.0-usb-0:2:1.0-port0",
		"DEVNAME":                        "/dev/ttyUSB0",
		"DEVPATH":                        "/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0",
		"ID_BUS":                         "usb",
		"ID_MM_CANDIDATE":                "1",
		"ID_MODEL_ENC":                   "FT232R\x20USB\x20UART",
		"MODEL_FROM_DATABASE":            "FT232 Serial (UART) IC",
		"ID_MODEL_ID":                    "6001",
		"ID_PATH":                        "pci-0000:00:14.0-usb-0:2:1.0",
		"ID_PATH_TAG":                    "pci-0000_00_14_0-usb-0_2_1_0",
		"ID_PCI_CLASS_FROM_DATABASE":     "Serial bus controller",
		"ID_PCI_INTERFACE_FROM_DATABASE": "XHCI",
		"ID_PCI_SUBCLASS_FROM_DATABASE":  "USB controller",
		"ID_REVISION":                    "0600",
		"ID_SERIAL":                      "FTDI_FT232R_USB_UART_AH06W0EQ",
		"ID_SERIAL_SHORT":                "AH06W0EQ",
		"ID_TYPE":                        "generic",
		"ID_USB_DRIVER":                  "ftdi_sio",
		"ID_USB_INTERFACES":              ":ffffff:",
		"ID_USB_INTERFACE_NUM":           "00",
		"ID_VENDOR":                      "FTDI",
		"ID_VENDOR_ENC":                  "FTDI",
		"ID_VENDOR_FROM_DATABASE":        "Future Technology Devices International, Ltd",
		"ID_VENDOR_ID":                   "0403",
		"MAJOR":                          "188",
		"MINOR":                          "0",
		"SUBSYSTEM":                      "tty",
		"TAGS":                           ":systemd:",
		"USEC_INITIALIZED":               "6571662103",
	})
	c.Assert(err, IsNil)
	key, err = ifacestate.DefaultDeviceKey(di, 0)
	c.Assert(err, IsNil)
	c.Assert(key, Equals, keyHelper("ID_VENDOR_ID\x000403\x00ID_MODEL_ID\x006001\x00ID_SERIAL\x00FTDI_FT232R_USB_UART_AH06W0EQ\x00"))

	// real device #3 - integrated web camera
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"COLORD_DEVICE":        "1",
		"COLORD_KIND":          "camera",
		"DEVLINKS":             "/dev/v4l/by-path/pci-0000:00:14.0-usb-0:11:1.0-video-index0 /dev/v4l/by-id/usb-CN0J8NNP7248766FA3H3A01_Integrated_Webcam_HD_200901010001-video-index0",
		"DEVNAME":              "/dev/video0",
		"DEVPATH":              "/devices/pci0000:00/0000:00:14.0/usb1/1-11/1-11:1.0/video4linux/video0",
		"ID_BUS":               "usb",
		"ID_FOR_SEAT":          "video4linux-pci-0000_00_14_0-usb-0_11_1_0",
		"ID_MODEL":             "Integrated_Webcam_HD",
		"ID_MODEL_ENC":         "Integrated_Webcam_HD",
		"ID_MODEL_ID":          "57c3",
		"ID_PATH":              "pci-0000:00:14.0-usb-0:11:1.0",
		"ID_PATH_TAG":          "pci-0000_00_14_0-usb-0_11_1_0",
		"ID_REVISION":          "5806",
		"ID_SERIAL":            "CN0J8NNP7248766FA3H3A01_Integrated_Webcam_HD_200901010001",
		"ID_SERIAL_SHORT":      "200901010001",
		"ID_TYPE":              "video",
		"ID_USB_DRIVER":        "uvcvideo",
		"ID_USB_INTERFACES":    ":0e0100:0e0200:",
		"ID_USB_INTERFACE_NUM": "00",
		"ID_V4L_CAPABILITIES":  ":capture:",
		"ID_V4L_PRODUCT":       "Integrated_Webcam_HD: Integrate",
		"ID_V4L_VERSION":       "2",
		"ID_VENDOR":            "CN0J8NNP7248766FA3H3A01",
		"ID_VENDOR_ENC":        "CN0J8NNP7248766FA3H3A01",
		"ID_VENDOR_ID":         "0bda",
		"MAJOR":                "81",
		"MINOR":                "0",
		"SUBSYSTEM":            "video4linux",
		"TAGS":                 ":uaccess:seat:",
		"USEC_INITIALIZED":     "3411321",
	})
	c.Assert(err, IsNil)
	key, err = ifacestate.DefaultDeviceKey(di, 0)
	c.Assert(err, IsNil)
	c.Assert(key, Equals, keyHelper("ID_V4L_PRODUCT\x00Integrated_Webcam_HD: Integrate\x00ID_VENDOR_ID\x000bda\x00ID_MODEL_ID\x0057c3\x00ID_SERIAL\x00CN0J8NNP7248766FA3H3A01_Integrated_Webcam_HD_200901010001\x00"))

	// key cannot be computed - empty string
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":   "a/path",
		"ACTION":    "add",
		"SUBSYSTEM": "foo",
	})
	c.Assert(err, IsNil)
	key, err = ifacestate.DefaultDeviceKey(di, 0)
	c.Assert(err, IsNil)
	c.Assert(key, Equals, "")
}

func (s *hotplugSuite) TestDefaultDeviceKeyError(c *C) {
	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":      "a/path",
		"ACTION":       "add",
		"SUBSYSTEM":    "foo",
		"NAME":         "name",
		"ID_VENDOR_ID": "vendor",
		"ID_MODEL_ID":  "model",
		"ID_SERIAL":    "serial",
	})
	c.Assert(err, IsNil)
	_, err = ifacestate.DefaultDeviceKey(di, 16)
	c.Assert(err, ErrorMatches, "internal error: invalid key version 16")
}

func (s *hotplugSuite) TestEnsureUniqueName(c *C) {
	fakeRepositoryLookup := func(n string) bool {
		reserved := map[string]bool{
			"slot1":    true,
			"slot":     true,
			"slot1234": true,
			"slot-1":   true,
			"slot-2":   true,
			"slot3-5":  true,
			"slot3-6":  true,
			"11":       true,
			"12foo":    true,
		}
		return !reserved[n]
	}

	names := []struct{ proposedName, resultingName string }{
		{"foo", "foo"},
		{"slot", "slot2"},
		{"slot1", "slot2"},
		{"slot1234", "slot1235"},
		{"slot-1", "slot2"},
		{"slot3-5", "slot36"},
		{"slot3-1", "slot3-1"},
		{"11", "12"},
		{"12foo", "12foo1"},
	}

	for _, name := range names {
		c.Assert(ifacestate.EnsureUniqueName(name.proposedName, fakeRepositoryLookup), Equals, name.resultingName)
	}
}

func (s *hotplugSuite) TestMakeSlotName(c *C) {
	names := []struct{ proposedName, resultingName string }{
		{"", ""},
		{"-", ""},
		{"slot1", "slot1"},
		{"-slot1", "slot1"},
		{"a--slot-1", "a-slot-1"},
		{"(-slot", "slot"},
		{"(--slot", "slot"},
		{"slot-", "slot"},
		{"slot---", "slot"},
		{"slot-(", "slot"},
		{"Integrated_Webcam_HD", "integratedwebcamhd"},
		{"Xeon E3-1200 v5/E3-1500 v5/6th Gen Core Processor Host Bridge/DRAM Registers", "xeone3-1200v5e3-1500"},
	}
	for _, name := range names {
		c.Assert(ifacestate.MakeSlotName(name.proposedName), Equals, name.resultingName)
	}
}

func (s *hotplugSuite) TestSuggestedSlotName(c *C) {

	events := []struct {
		eventData map[string]string
		outName   string
	}{{
		map[string]string{
			"DEVPATH":                "a/path",
			"ACTION":                 "add",
			"SUBSYSTEM":              "foo",
			"NAME":                   "Name",
			"ID_MODEL":               "Longer Name",
			"ID_MODEL_FROM_DATABASE": "Longest Name",
		},
		"name",
	}, {
		map[string]string{
			"DEVPATH":                "a/path",
			"ACTION":                 "add",
			"SUBSYSTEM":              "foo",
			"ID_MODEL":               "Longer Name",
			"ID_MODEL_FROM_DATABASE": "Longest Name",
		},
		"longername",
	}, {
		map[string]string{
			"DEVPATH":                "a/path",
			"ACTION":                 "add",
			"SUBSYSTEM":              "foo",
			"ID_MODEL_FROM_DATABASE": "Longest Name",
		},
		"longestname",
	}, {
		map[string]string{
			"DEVPATH":   "a/path",
			"ACTION":    "add",
			"SUBSYSTEM": "foo",
		},
		"fallbackname",
	},
	}

	for _, data := range events {
		di, err := hotplug.NewHotplugDeviceInfo(data.eventData)
		c.Assert(err, IsNil)

		slotName := ifacestate.SuggestedSlotName(di, "fallbackname")
		c.Assert(slotName, Equals, data.outName)
	}
}

func (s *hotplugSuite) TestUpdateDeviceTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	tss := ifacestate.UpdateDevice(st, "interface", "key", map[string]interface{}{"attr": "value"})
	c.Assert(tss, NotNil)
	c.Assert(tss.Tasks(), HasLen, 3)

	task1 := tss.Tasks()[0]
	c.Assert(task1.Kind(), Equals, "hotplug-disconnect")

	iface, key, err := ifacestate.GetHotplugAttrs(task1)
	c.Assert(err, IsNil)
	c.Assert(iface, Equals, "interface")
	c.Assert(key, Equals, "key")

	task2 := tss.Tasks()[1]
	c.Assert(task2.Kind(), Equals, "hotplug-update-slot")
	iface, key, err = ifacestate.GetHotplugAttrs(task2)
	c.Assert(err, IsNil)
	c.Assert(iface, Equals, "interface")
	c.Assert(key, Equals, "key")
	var attrs map[string]interface{}
	c.Assert(task2.Get("slot-attrs", &attrs), IsNil)
	c.Assert(attrs, DeepEquals, map[string]interface{}{"attr": "value"})

	task3 := tss.Tasks()[2]
	c.Assert(task3.Kind(), Equals, "hotplug-connect")
	iface, key, err = ifacestate.GetHotplugAttrs(task2)
	c.Assert(err, IsNil)
	c.Assert(iface, Equals, "interface")
	c.Assert(key, Equals, "key")

	wt := task2.WaitTasks()
	c.Assert(wt, HasLen, 1)
	c.Assert(wt[0], DeepEquals, task1)

	wt = task3.WaitTasks()
	c.Assert(wt, HasLen, 1)
	c.Assert(wt[0], DeepEquals, task2)
}
