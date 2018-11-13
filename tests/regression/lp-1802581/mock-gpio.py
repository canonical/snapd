#!/usr/bin/python3

import atexit
import logging
import os
import selectors
import shutil
import subprocess
import sys
import tempfile


def read_gpio_pin(fd):
    data = os.read(fd, 128)
    pin = data.decode().strip()
    if not pin.isnumeric():
        logging.warning("invalid gpio pin {}".format(pin))
        return ""
    return "gpio{}".format(pin)


def export_ready(fd, mask):
    pin = read_gpio_pin(fd)
    # allow quit
    if pin == 'quit':
        return False
    if pin:
        with open(pin, "w"):
            pass
    return True
    

def unexport_ready(fd, mask):
    pin = read_gpio_pin(fd)
    try:
        os.remove(pin)
    except Exception as e:
        logging.warning("got exception {}".format(e))
    return True


def dispatch(sel):
    for key, mask in sel.select():
        callback = key.data
        if not callback(key.fileobj, mask):
            return False
    return True


if __name__ == "__main__":
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
    while True:
        if not dispatch(sel):
            break

    # cleanup when we get a quit call
    os.close(efd)
    os.close(ufd)
