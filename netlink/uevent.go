package netlink

import (
	"bytes"
	"fmt"
	"strings"
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

type KObjAction string

func (a KObjAction) String() string {
	return string(a)
}

func ParseKObjAction(raw string) (a KObjAction, err error) {
	a = KObjAction(raw)
	switch a {
	case ADD, REMOVE, CHANGE, MOVE, ONLINE, OFFLINE, BIND, UNBIND:
	default:
		err = fmt.Errorf("unknow kobject action (got: %s)", raw)
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

func parseUdevEvent(raw []byte) (e *UEvent, err error) {
	fields := bytes.Split(raw[40:], []byte{0x00}) // 0x00 = end of string

	if len(fields) == 0 {
		err = fmt.Errorf("Wrong libudev format")
		return
	}

	envdata := make(map[string]string, 0)
	for _, envs := range fields[0 : len(fields)-1] {
		env := bytes.Split(envs, []byte("="))
		if len(env) != 2 {
			err = fmt.Errorf("Wrong libudev env")
			return
		}
		envdata[string(env[0])] = string(env[1])
	}

	var action KObjAction
	action, err = ParseKObjAction(strings.ToLower(envdata["ACTION"]))
	if err != nil {
		return
	}

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
	if bytes.Compare(raw[:7], []byte("libudev")) == 0 {
		return parseUdevEvent(raw)
	}
	fields := bytes.Split(raw, []byte{0x00}) // 0x00 = end of string

	if len(fields) == 0 {
		err = fmt.Errorf("Wrong uevent format")
		return
	}

	headers := bytes.Split(fields[0], []byte("@")) // 0x40 = @
	if len(headers) != 2 {
		err = fmt.Errorf("Wrong uevent header")
		return
	}

	action, err := ParseKObjAction(string(headers[0]))
	if err != nil {
		return
	}

	e = &UEvent{
		Action: action,
		KObj:   string(headers[1]),
		Env:    make(map[string]string, 0),
	}

	for _, envs := range fields[1 : len(fields)-1] {
		env := bytes.Split(envs, []byte("="))
		if len(env) != 2 {
			err = fmt.Errorf("Wrong uevent env")
			return
		}
		e.Env[string(env[0])] = string(env[1])
	}
	return
}
