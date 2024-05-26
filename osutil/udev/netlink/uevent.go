package netlink

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unsafe"

	"github.com/ddkwork/golibrary/mylog"
)

// See: http://elixir.free-electrons.com/linux/v3.12/source/lib/kobject_uevent.c#L45

const (
	ADD     KObjAction = "add"
	REMOVE  KObjAction = "remove"
	CHANGE  KObjAction = "change"
	MOVE    KObjAction = "move"
	ONLINE  KObjAction = "online"
	OFFLINE KObjAction = "offline"
	BIND    KObjAction = "bind"
	UNBIND  KObjAction = "unbind"
)

// The magic value used by udev, see https://github.com/systemd/systemd/blob/v239/src/libudev/libudev-monitor.c#L57
const libudevMagic = 0xfeedcafe

type KObjAction string

func (a KObjAction) String() string {
	return string(a)
}

func ParseKObjAction(raw string) (a KObjAction, err error) {
	a = KObjAction(raw)
	switch a {
	case ADD, REMOVE, CHANGE, MOVE, ONLINE, OFFLINE, BIND, UNBIND:
	default:
		mylog.Check(fmt.Errorf("unknow kobject action (got: %s)", raw))
	}
	return
}

type UEvent struct {
	Action KObjAction
	KObj   string
	Env    map[string]string
}

func (e UEvent) String() string {
	rv := fmt.Sprintf("%s@%s\000", e.Action.String(), e.KObj)
	for k, v := range e.Env {
		rv += k + "=" + v + "\000"
	}
	return rv
}

func (e UEvent) Bytes() []byte {
	return []byte(e.String())
}

func (e UEvent) Equal(e2 UEvent) (bool, error) {
	if e.Action != e2.Action {
		return false, fmt.Errorf("Wrong action (got: %s, wanted: %s)", e.Action, e2.Action)
	}

	if e.KObj != e2.KObj {
		return false, fmt.Errorf("Wrong kobject (got: %s, wanted: %s)", e.KObj, e2.KObj)
	}

	if len(e.Env) != len(e2.Env) {
		return false, fmt.Errorf("Wrong length of env (got: %d, wanted: %d)", len(e.Env), len(e2.Env))
	}

	var found bool
	for k, v := range e.Env {
		found = false
		for i, e := range e2.Env {
			if i == k && v == e {
				found = true
			}
		}
		if !found {
			return false, fmt.Errorf("Unable to find %s=%s env var from uevent", k, v)
		}
	}
	return true, nil
}

// Parse udev event created by udevd.
// The format of the data header is internal to udev and defined in libudev-monitor.c - see the udev_monitor_netlink_header struct.
// go-udev only looks at the "magic" number to filter out possibly invalid packets, and at the payload offset. Other fields of the header
// are ignored.
// Note, only some of the fields of the header use network byte order, for the rest udev uses native byte order of the platform.
func parseUdevEvent(raw []byte) (e *UEvent, err error) {
	// the magic number is stored in network byte order.
	magic := binary.BigEndian.Uint32(raw[8:])
	if magic != libudevMagic {
		return nil, fmt.Errorf("cannot parse libudev event: magic number mismatch")
	}

	// the payload offset int is stored in native byte order.
	payloadoff := *(*uint32)(unsafe.Pointer(&raw[16]))
	if payloadoff >= uint32(len(raw)) {
		return nil, fmt.Errorf("cannot parse libudev event: invalid data offset")
	}

	fields := bytes.Split(raw[payloadoff:], []byte{0x00}) // 0x00 = end of string
	if len(fields) == 0 {
		mylog.Check(fmt.Errorf("cannot parse libudev event: data missing"))
		return
	}

	envdata := make(map[string]string, 0)
	for _, envs := range fields[0 : len(fields)-1] {
		env := bytes.Split(envs, []byte("="))
		if len(env) != 2 {
			mylog.Check(fmt.Errorf("cannot parse libudev event: invalid env data"))
			return
		}
		envdata[string(env[0])] = string(env[1])
	}

	var action KObjAction
	action = mylog.Check2(ParseKObjAction(strings.ToLower(envdata["ACTION"])))

	// XXX: do we need kobj?
	kobj := envdata["DEVPATH"]

	e = &UEvent{
		Action: action,
		KObj:   kobj,
		Env:    envdata,
	}

	return
}

func ParseUEvent(raw []byte) (e *UEvent, err error) {
	if len(raw) > 40 && bytes.Compare(raw[:8], []byte("libudev\x00")) == 0 {
		return parseUdevEvent(raw)
	}
	fields := bytes.Split(raw, []byte{0x00}) // 0x00 = end of string

	if len(fields) == 0 {
		mylog.Check(fmt.Errorf("Wrong uevent format"))
		return
	}

	headers := bytes.Split(fields[0], []byte("@")) // 0x40 = @
	if len(headers) != 2 {
		mylog.Check(fmt.Errorf("Wrong uevent header"))
		return
	}

	action := mylog.Check2(ParseKObjAction(string(headers[0])))

	e = &UEvent{
		Action: action,
		KObj:   string(headers[1]),
		Env:    make(map[string]string, 0),
	}

	for _, envs := range fields[1 : len(fields)-1] {
		env := bytes.Split(envs, []byte("="))
		if len(env) != 2 {
			mylog.Check(fmt.Errorf("Wrong uevent env"))
			return
		}
		e.Env[string(env[0])] = string(env[1])
	}
	return
}
