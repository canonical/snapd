#!/usr/bin/env python3
# Minimal snapd REST client for the assertion-sign interface, mirroring
# api-apps-client.py from interfaces-ros-snapd-support. Talks to the daemon
# over the unix socket that every restricted snap already has (base AppArmor).
#
# The statement JSON is passed as a single command-line argument so the
# confined app never needs to read files from the host.

import http.client
import json
import socket
import sys


class UnixSocketHTTPConnection(http.client.HTTPConnection):
    def __init__(self):
        super().__init__('localhost')

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect('/run/snapd-snap.socket')


def main(argv):
    # usage: assertion-sign-client.sign <statement-json> [key]
    statement = json.loads(argv[1])
    key = argv[2] if len(argv) > 2 else "default"

    body = json.dumps({"key": key, "statement": statement})
    conn = UnixSocketHTTPConnection()
    # X-Allow-Interaction: tells the daemon to pass polkit the
    # CheckAllowInteraction flag so the user gets a consent prompt
    # (a restricted snap cannot call polkit itself; the daemon does,
    # on its behalf, using the peer PID from the socket).
    conn.request("POST", "/v2/assertions/sign", body,
                 {"Content-Type": "application/json",
                  "X-Allow-Interaction": "true"})
    response = conn.getresponse()
    out = json.loads(response.read().decode("utf-8"))
    print(json.dumps(out))
    # exit non-zero on 5xx so the task can detect hard failures
    return response.status >= 500


if __name__ == '__main__':
    sys.exit(main(sys.argv))
