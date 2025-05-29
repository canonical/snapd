#!/usr/bin/python3

import argparse
import http.client
import sys
import socket

class UnixSocketHTTPConnection(http.client.HTTPConnection):
    def __init__(self):
        super().__init__('localhost')

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect('/run/snapd-snap.socket')


def main(argv):
    parser = argparse.ArgumentParser('Call the snapd REST API')
    parser.add_argument('--method', default='GET',
                        help='The HTTP method to use')
    parser.add_argument('body', metavar='BODY', default=None, nargs='?',
                        help='The HTTP request body')
    args = parser.parse_args(argv[1:])

    conn = UnixSocketHTTPConnection()
    conn.request(args.method, '/v2/apps', args.body)

    response = conn.getresponse()
    body = response.read()
    print(body.decode('UTF-8'))
    return response.status >= 500

if __name__ == '__main__':
    sys.exit(main(sys.argv))
