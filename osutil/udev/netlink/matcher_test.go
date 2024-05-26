package netlink

import (
	"testing"

	"github.com/ddkwork/golibrary/mylog"
)

type Testcases []Testcase

type Testcase struct {
	Object interface{}
	Valid  bool
}

func TestRules(testing *testing.T) {
	t := testingWrapper{testing}

	uevent := UEvent{
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
	}

	add := ADD.String()
	wrongAction := "can't match"

	rules := []RuleDefinition{
		{
			Action: nil,
			Env: map[string]string{
				"DEVNAME": "hidraw\\d+",
			},
		},

		{
			Action: &add,
			Env:    make(map[string]string, 0),
		},

		{
			Action: nil,
			Env: map[string]string{
				"SUBSYSTEM": "can't match",
				"MAJOR":     "247",
			},
		},

		{
			Action: &add,
			Env: map[string]string{
				"SUBSYSTEM": "hidraw",
				"MAJOR":     "\\d+",
			},
		},

		{
			Action: &wrongAction,
			Env: map[string]string{
				"SUBSYSTEM": "hidraw",
				"MAJOR":     "\\d+",
			},
		},
	}

	testcases := []Testcase{
		{
			Object: &rules[0],
			Valid:  true,
		},
		{
			Object: &rules[1],
			Valid:  true,
		},
		{
			Object: &rules[2],
			Valid:  false,
		},
		{
			Object: &rules[3],
			Valid:  true,
		},
		{
			Object: &rules[4],
			Valid:  false,
		},
		{
			Object: &RuleDefinitions{[]RuleDefinition{rules[0], rules[4]}},
			Valid:  true,
		},
		{
			Object: &RuleDefinitions{[]RuleDefinition{rules[4], rules[0]}},
			Valid:  true,
		},
		{
			Object: &RuleDefinitions{[]RuleDefinition{rules[2], rules[4]}},
			Valid:  false,
		},
		{
			Object: &RuleDefinitions{[]RuleDefinition{rules[3], rules[1]}},
			Valid:  true,
		},
	}

	for k, tcase := range testcases {
		matcher := tcase.Object.(Matcher)
		mylog.Check(matcher.Compile())
		t.FatalfIf(err != nil, "Testcase n°%d should compile without error, err: %v", k+1, err)

		ok := matcher.Evaluate(uevent)
		t.FatalfIf((ok != tcase.Valid) && tcase.Valid, "Testcase n°%d should evaluate event", k+1)
		t.FatalfIf((ok != tcase.Valid) && !tcase.Valid, "Testcase n°%d shouldn't evaluate event", k+1)
	}
}
