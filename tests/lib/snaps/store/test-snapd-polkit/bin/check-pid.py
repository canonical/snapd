#!/usr/bin/env python3
import sys
import dbus
import dbus.lowlevel

def run(pid, pid_start, uid, action_id):
    bus = dbus.SystemBus()
    obj = bus.get_object("org.freedesktop.PolicyKit1", "/org/freedesktop/PolicyKit1/Authority", False)
    polkit = dbus.Interface(obj, "org.freedesktop.PolicyKit1.Authority")
    subject = ("unix-process", {
        "pid": dbus.UInt32(pid, variant_level=1),
        "start-time": dbus.UInt64(pid_start, variant_level=1),
    })
    details = {"uid": uid}
    ret = polkit.CheckAuthorization(subject, action_id, details, dbus.UInt32(0), "")
    if len(ret) != 3:
        raise Exception("unexpected number of return values")

    if type(ret[0]) != dbus.types.Boolean:
        raise Exception("unexpected type of ret[0] %s, expected dbus.Boolean", type(ret[0]))
    if type(ret[1]) != dbus.types.Boolean:
        raise Exception("unexpected type of ret[1] %s, expected dbus.Boolean", type(ret[1]))
    if type(ret[2]) != dbus.types.Dictionary:
        raise Exception("unexpected type of ret[1] %s, expected dbus.Boolean", type(ret[2]))

    msg = dbus.lowlevel.Message()
    print(msg.guess_signature(ret), bool(ret[0]), bool(ret[1]), dict(ret[2]))


if __name__ == "__main__":
    pid = sys.argv[1]
    pid_start = sys.argv[2]
    uid = sys.argv[3]
    action_id = sys.argv[4]
    run(pid, pid_start, uid, action_id)
