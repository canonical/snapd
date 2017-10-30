package netlink

import "testing"

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
		UEvent{
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
		UEvent{
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
