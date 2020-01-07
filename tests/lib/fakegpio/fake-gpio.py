#!/usr/bin/python3
"""Mock the /sys/class/gpio subsystem"""

import atexit
import logging
import os
import selectors
import shutil
import socket
import subprocess
import sys
import tempfile


def read_gpio_pin(read_fd: int) -> str:
    """
    read_gpio_pin reads from the given fd and return a string with the
    numeric pin or "" if an invalid pin was specified.
    """
    data = os.read(read_fd, 128)
    pin = data.decode().strip()
    if not pin.isnumeric():
        logging.warning("invalid gpio pin %s", pin)
        return ""
    return "gpio{}".format(pin)


def export_ready(read_fd: int, _) -> bool:
    """export_ready is run when the "export" file is ready for reading"""
    pin = read_gpio_pin(read_fd)
    # allow quit
    if pin == 'quit':
        return False
    if pin:
        with open(pin, "w"):
            pass
    return True


def unexport_ready(read_fd: int, _) -> bool:
    """export_ready is run when the "unexport" file is ready for reading"""
    pin = read_gpio_pin(read_fd)
    try:
        os.remove(pin)
    except OSError as err:
        logging.warning("got exception %s", err)
    return True


def dispatch(sel):
    """
    dispatch dispatches the events from the "sel" source
    """
    for key, mask in sel.select():
        callback = key.data
        if not callback(key.fileobj, mask):
            return False
    return True


def maybe_sd_notify(s: str) -> None:
    addr = os.getenv('NOTIFY_SOCKET')
    if not addr:
        return
    soc = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    soc.connect(addr)
    soc.sendall(s.encode())


def main():
    """the main method"""
    if os.getuid() != 0:
        print("must run as root")
        sys.exit(1)

    # setup mock env
    mock_gpio_dir = tempfile.mkdtemp("mock-gpio")
    atexit.register(shutil.rmtree, mock_gpio_dir)
    os.chdir(mock_gpio_dir)
    subprocess.check_call(
        ["mount", "--bind", mock_gpio_dir, "/sys/class/gpio"])
    atexit.register(lambda: subprocess.call(["umount", "/sys/class/gpio"]))

    # fake gpio export/unexport files
    os.mkfifo("export")
    os.mkfifo("unexport")

    # react to the export/unexport calls
    sel = selectors.DefaultSelector()
    efd = os.open("export", os.O_RDWR | os.O_NONBLOCK)
    ufd = os.open("unexport", os.O_RDWR | os.O_NONBLOCK)
    sel.register(efd, selectors.EVENT_READ, export_ready)
    sel.register(ufd, selectors.EVENT_READ, unexport_ready)
    # notify
    maybe_sd_notify("READY=1")
    while True:
        if not dispatch(sel):
            break

    # cleanup when we get a quit call
    os.close(efd)
    os.close(ufd)


if __name__ == "__main__":
    main()
