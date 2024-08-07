#!/usr/bin/python3

import argparse
import http.client
import sys
import socket

class UnixSocketHTTPConnection(http.client.HTTPConnection):
    def __init__(self, socket_path):
        super().__init__('localhost')
        self._socket_path = socket_path

    def connect(self):
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.connect(self._socket_path)
        self.sock = s


def main(argv):
    parser = argparse.ArgumentParser('Call the snapd REST API')
    parser.add_argument('--socket', default='/run/snapd.socket',
                        help='The socket path to connect to')
    parser.add_argument('--method', default='GET',
                        help='The HTTP method to use')
    parser.add_argument('path', metavar='PATH',
                        help='The HTTP path to request')
    parser.add_argument('body', metavar='BODY', default=None, nargs='?',
                        help='The HTTP request body')
    args = parser.parse_args(argv[1:])

    conn = UnixSocketHTTPConnection(args.socket)
    conn.request(args.method, args.path, args.body)

    response = conn.getresponse()
    body = response.read()
    print(body.decode('UTF-8'))
    return response.status >= 300

if __name__ == '__main__':
    sys.exit(main(sys.argv))
