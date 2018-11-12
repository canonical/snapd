#!/usr/bin/python3

import os
import selectors
import sys


def data_to_gpio(data):
    return "gpio"+data.decode().strip()


def export_ready(fd, mask):
    data = os.read(fd, 128)
    # allow quit
    if data == b'quit\n':
        return False
    with open(data_to_gpio(data), "w"):
        pass
    return True
    

def unexport_ready(fd, mask):
    os.remove(data_to_gpio(os.read(fd, 128)))
    return True


def dispatch(sel):
    for key, mask in sel.select():
        callback = key.data
        if not callback(key.fileobj, mask):
            return False
    return True


if __name__ == "__main__":
    os.chdir(sys.argv[1])
    
    # fake gpio export/unexport files
    os.mkfifo("export")
    os.mkfifo("unexport")

    # ensure that we create the right 
    sel = selectors.DefaultSelector()
    efd = os.open("export", os.O_RDWR | os.O_NONBLOCK)
    ufd = os.open("unexport", os.O_RDWR | os.O_NONBLOCK)
    sel.register(efd, selectors.EVENT_READ, export_ready)
    sel.register(ufd, selectors.EVENT_READ, unexport_ready)
    while True:
        if not dispatch(sel):
            break

    # cleanup
    os.close(efd)
    os.close(ufd)
