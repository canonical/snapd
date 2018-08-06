package netlink

import (
	"runtime"
	"testing"
)

type testingWrapper struct {
	*testing.T
}

func (t *testingWrapper) FatalfIf(cond bool, msg string, args ...interface{}) {
	if cond {
		if len(args) == 0 {
			t.Fatal(msg)
		}
		t.Fatalf(msg, args...)
	}
}

// TestParseUEvent parse uevent bytes msg but fatal if needed
func TestParseUEvent(testing *testing.T) {
	t := testingWrapper{testing}

	samples := []UEvent{
		{
			Action: ADD,
			KObj:   "/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.2/0003:04F2:0976.0008/hidraw/hidraw4",
			Env: map[string]string{
				"ACTION":    "add",
				"DEVPATH":   "/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.2/0003:04F2:0976.0008/hidraw/hidraw4",
				"SUBSYSTEM": "hidraw",
				"MAJOR":     "247",
				"MINOR":     "4",
				"DEVNAME":   "hidraw4",
				"SEQNUM":    "2569",
			},
		},
		{
			Action: REMOVE,
			KObj:   "mykobj",
			Env: map[string]string{
				"bla": "bla",
				"abl": "abl",
				"lab": "lab",
			},
		},
	}
	for _, s := range samples {
		raw := s.Bytes()
		uevent, err := ParseUEvent(raw)
		t.FatalfIf(err != nil, "Unable to parse uevent (got: %s)", s.String())
		t.FatalfIf(uevent == nil, "Uevent can't be nil (with: %s)", s.String())

		ok, err := uevent.Equal(s)
		t.FatalfIf(!ok || err != nil, "Uevent should be equal: bijectivity fail")
	}

	raw := samples[0].Bytes()
	raw[3] = 0x00 // remove @ to fake rawdata

	uevent, err := ParseUEvent(raw)
	t.FatalfIf(err == nil && uevent != nil, "Event parsed successfully but it should be invalid, err: %s", err.Error())

}

func TestParseUdevEvent(testing *testing.T) {
	if runtime.GOARCH == "s390x" {
		testing.Skip("This test assumes little-endian architecture")
	}

	t := testingWrapper{testing}

	// Input samples obtained by running the main testing binary in monitor mode
	// and having fmt.Printf("%q\n", raw) in the ParseUEvent method.
	samples := []struct {
		Input    []byte
		Expected UEvent
	}{{
		Input: []byte("libudev\x00\xfe\xed\xca\xfe(\x00\x00\x00(\x00\x00\x00\xd5\x03\x00\x00\x8a\xfa\x90\xc8\x00\x00\x00\x00\x02\x00\x04\x00\x10\x80\x00\x00" +
			"ACTION=remove\x00DEVPATH=/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0\x00SUBSYSTEM=tty\x00" +
			"DEVNAME=/dev/ttyUSB0\x00SEQNUM=4344\x00MAJOR=188\x00MINOR=0\x00USEC_INITIALIZED=75223543693\x00ID_BUS=usb\x00" +
			"ID_VENDOR_ID=0403\x00ID_MODEL_ID=6001\x00ID_PCI_CLASS_FROM_DATABASE=Serial bus controller\x00" +
			"ID_PCI_SUBCLASS_FROM_DATABASE=USB controller\x00ID_PCI_INTERFACE_FROM_DATABASE=XHCI\x00" +
			"ID_VENDOR_FROM_DATABASE=Future Technology Devices International, Ltd\x00ID_MODEL_FROM_DATABASE=FT232 Serial (UART) IC\x00" +
			"ID_VENDOR=FTDI\x00ID_VENDOR_ENC=FTDI\x00ID_MODEL=FT232R_USB_UART\x00ID_MODEL_ENC=FT232R\\x20USB\\x20UART\x00" +
			"ID_REVISION=0600\x00ID_SERIAL=FTDI_FT232R_USB_UART_AH06W0EQ\x00ID_SERIAL_SHORT=AH06W0EQ\x00ID_TYPE=generic\x00" +
			"ID_USB_INTERFACES=:ffffff:\x00ID_USB_INTERFACE_NUM=00\x00ID_USB_DRIVER=ftdi_sio\x00" +
			"ID_PATH=pci-0000:00:14.0-usb-0:2:1.0\x00ID_PATH_TAG=pci-0000_00_14_0-usb-0_2_1_0\x00ID_MM_CANDIDATE=1\x00" +
			"DEVLINKS=/dev/serial/by-path/pci-0000:00:14.0-usb-0:2:1.0-port0 /dev/serial/by-id/usb-FTDI_FT232R_USB_UART_AH06W0EQ-if00-port0\x00TAGS=:systemd:\x00"),
		Expected: UEvent{
			Action: REMOVE,
			KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0",
			Env: map[string]string{
				"MINOR": "0",
				"ID_PCI_CLASS_FROM_DATABASE":     "Serial bus controller",
				"ID_PCI_SUBCLASS_FROM_DATABASE":  "USB controller",
				"ID_VENDOR_FROM_DATABASE":        "Future Technology Devices International, Ltd",
				"ID_MODEL_ENC":                   "FT232R\\x20USB\\x20UART",
				"ID_USB_INTERFACES":              ":ffffff:",
				"ID_PATH":                        "pci-0000:00:14.0-usb-0:2:1.0",
				"ID_MODEL":                       "FT232R_USB_UART",
				"ID_TYPE":                        "generic",
				"DEVLINKS":                       "/dev/serial/by-path/pci-0000:00:14.0-usb-0:2:1.0-port0 /dev/serial/by-id/usb-FTDI_FT232R_USB_UART_AH06W0EQ-if00-port0",
				"ID_MODEL_ID":                    "6001",
				"ID_USB_INTERFACE_NUM":           "00",
				"ID_PATH_TAG":                    "pci-0000_00_14_0-usb-0_2_1_0",
				"TAGS":                           ":systemd:",
				"ACTION":                         "remove",
				"DEVNAME":                        "/dev/ttyUSB0",
				"ID_REVISION":                    "0600",
				"ID_VENDOR_ENC":                  "FTDI",
				"ID_USB_DRIVER":                  "ftdi_sio",
				"ID_MM_CANDIDATE":                "1",
				"SEQNUM":                         "4344",
				"ID_VENDOR":                      "FTDI",
				"SUBSYSTEM":                      "tty",
				"MAJOR":                          "188",
				"ID_BUS":                         "usb",
				"ID_VENDOR_ID":                   "0403",
				"ID_MODEL_FROM_DATABASE":         "FT232 Serial (UART) IC",
				"ID_SERIAL":                      "FTDI_FT232R_USB_UART_AH06W0EQ",
				"DEVPATH":                        "/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0",
				"USEC_INITIALIZED":               "75223543693",
				"ID_PCI_INTERFACE_FROM_DATABASE": "XHCI",
				"ID_SERIAL_SHORT":                "AH06W0EQ",
			}}}, {
		Input: []byte("libudev\x00\xfe\xed\xca\xfe(\x00\x00\x00(\x00\x00\x00\xf2\x02\x00\x00\x05w\xc5\xe5'\xf8\xf5\f\x00\x00\x00\x00\x00\x00\x00\x00" +
			"ACTION=add\x00DEVPATH=/devices/pci0000:00/0000:00:14.0/usb1/1-2\x00SUBSYSTEM=usb\x00DEVNAME=/dev/bus/usb/001/033\x00DEVTYPE=usb_device\x00" +
			"PRODUCT=10c4/ea60/100\x00TYPE=0/0/0\x00BUSNUM=001\x00DEVNUM=033\x00SEQNUM=4410\x00MAJOR=189\x00MINOR=32\x00USEC_INITIALIZED=77155422759\x00" +
			"ID_VENDOR=Silicon_Labs\x00ID_VENDOR_ENC=Silicon\\x20Labs\x00ID_VENDOR_ID=10c4\x00ID_MODEL=CP2102_USB_to_UART_Bridge_Controller\x00" +
			"ID_MODEL_ENC=CP2102\\x20USB\\x20to\\x20UART\\x20Bridge\\x20Controller\x00ID_MODEL_ID=ea60\x00ID_REVISION=0100\x00" +
			"ID_SERIAL=Silicon_Labs_CP2102_USB_to_UART_Bridge_Controller_0001\x00ID_SERIAL_SHORT=0001\x00ID_BUS=usb\x00ID_USB_INTERFACES=:ff0000:\x00" +
			"ID_VENDOR_FROM_DATABASE=Cygnal Integrated Products, Inc.\x00ID_MODEL_FROM_DATABASE=CP2102/CP2109 UART Bridge Controller [CP210x family]\x00" +
			"DRIVER=usb\x00ID_MM_DEVICE_MANUAL_SCAN_ONLY=1\x00"),
		Expected: UEvent{
			Action: ADD,
			KObj:   "/devices/pci0000:00/0000:00:14.0/usb1/1-2",
			Env: map[string]string{
				"DEVTYPE":                "usb_device",
				"SEQNUM":                 "4410",
				"DRIVER":                 "usb",
				"DEVPATH":                "/devices/pci0000:00/0000:00:14.0/usb1/1-2",
				"SUBSYSTEM":              "usb",
				"BUSNUM":                 "001",
				"ID_USB_INTERFACES":      ":ff0000:",
				"USEC_INITIALIZED":       "77155422759",
				"ID_VENDOR_ENC":          "Silicon\\x20Labs",
				"ID_VENDOR_ID":           "10c4",
				"ID_SERIAL":              "Silicon_Labs_CP2102_USB_to_UART_Bridge_Controller_0001",
				"ACTION":                 "add",
				"DEVNAME":                "/dev/bus/usb/001/033",
				"MAJOR":                  "189",
				"ID_MODEL_FROM_DATABASE": "CP2102/CP2109 UART Bridge Controller [CP210x family]",
				"TYPE":                          "0/0/0",
				"ID_REVISION":                   "0100",
				"ID_BUS":                        "usb",
				"PRODUCT":                       "10c4/ea60/100",
				"DEVNUM":                        "033",
				"MINOR":                         "32",
				"ID_MODEL_ENC":                  "CP2102\\x20USB\\x20to\\x20UART\\x20Bridge\\x20Controller",
				"ID_MM_DEVICE_MANUAL_SCAN_ONLY": "1",
				"ID_VENDOR":                     "Silicon_Labs",
				"ID_MODEL":                      "CP2102_USB_to_UART_Bridge_Controller",
				"ID_MODEL_ID":                   "ea60",
				"ID_SERIAL_SHORT":               "0001",
				"ID_VENDOR_FROM_DATABASE":       "Cygnal Integrated Products, Inc.",
			},
		},
	}}

	for _, s := range samples {
		uevent, err := ParseUEvent(s.Input)
		t.FatalfIf(err != nil, "Unable to parse uevent: %s", err)
		ok, err := uevent.Equal(s.Expected)
		t.FatalfIf(!ok || err != nil, "Uevent should be equal: bijectivity fail,\n%s", err)
	}

	invalidMagic := []byte("libudev\x00\xfe\xed\xca\xff(\x00\x00\x00(\x00\x00\x00\xd5\x03\x00\x00\x8a\xfa\x90\xc8\x00\x00\x00\x00\x02\x00\x04\x00\x10\x80\x00\x00ACTION=remove\x00DEVPATH=foo\x00")
	uevent, err := ParseUEvent(invalidMagic)
	t.FatalfIf(err == nil && uevent != nil, "Event parsed successfully but it should be invalid, err: %s", err)
	t.FatalfIf(err.Error() != "cannot parse libudev event: magic number mismatch", "Expecting magic number error, got %s", err)

	invalidOffset := []byte("libudev\x00\xfe\xed\xca\xfe(\xff\xff\xff(\xff\xff\xff\xd5\xf3\xff\xff\x8a\xfa\x90\xc8\x00\x00\x00\x00\x02\x00\x04\x00\x10\x80\x00\x00ACTION=remove\x00DEVPATH=foo\x00")
	uevent, err = ParseUEvent(invalidOffset)
	t.FatalfIf(err == nil && uevent != nil, "Event parsed successfully but it should be invalid, err: %s", err)
	t.FatalfIf(err.Error() != "cannot parse libudev event: invalid data offset", "Expecting invalud offset error, got %s", err)
}
