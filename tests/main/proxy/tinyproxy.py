#!/usr/bin/python3

# Tiny HTTP Proxy. Based on the work of SUZUKI Hisao.
#
# Ported to py3 and modified to remove the bits we don't need
# and modernized.

import http.server
import select
import socket
import socketserver
import sys
import urllib.parse


class ProxyHandler (http.server.BaseHTTPRequestHandler):
    server_version = "testsproxy/1.0"

    def log_request(self, m=""):
        super().log_request(m)
        sys.stdout.flush()
        sys.stderr.flush()

    def handle(self):
        (ip, port) =  self.client_address
        super().handle()

    def _connect_to(self, netloc, soc):
        i = netloc.find(':')
        if i >= 0:
            host_port = netloc[:i], int(netloc[i+1:])
        else:
            host_port = netloc, 80
        try:
            soc.connect(host_port)
        except socket.error as arg:
            try:
                msg = arg[1]
            except:
                msg = arg
            self.send_error(404, msg)
            return False
        return True

    def do_CONNECT(self):
        soc = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            if self._connect_to(self.path, soc):
                self.log_request(200)
                s = self.protocol_version + " 200 Connection established\r\n"
                self.wfile.write(s.encode())
                s = "Proxy-agent: {}\r\n".format(self.version_string())
                self.wfile.write(s.encode())
                self.wfile.write("\r\n".encode())
                self._read_write(soc, 300)
        finally:
            soc.close()
            self.connection.close()

    def do_GET(self):
        (scm, netloc, path, params, query, fragment) = urllib.parse.urlparse(
            self.path, 'http')
        if scm != 'http' or fragment or not netloc:
            s = "bad url {}".format(self.path)
            self.send_error(400, s.encode())
            return
        soc = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            if self._connect_to(netloc, soc):
                self.log_request()
                s = "{} {} {}\r\n".format(
                    self.command,
                    urllib.parse.urlunparse(('', '', path, params, query, '')),
                    self.request_version)
                soc.send(s.encode())
                self.headers['Connection'] = 'close'
                del self.headers['Proxy-Connection']
                for key, val in self.headers.items():
                    s = "{}: {}\r\n".format(key, val)
                    soc.send(s.encode())
                soc.send("\r\n".encode())
                self._read_write(soc)
        finally:
            soc.close()
            self.connection.close()

    def _read_write(self, soc, max_idling=20):
        iw = [self.connection, soc]
        ow = []
        count = 0
        while True:
            count += 1
            (ins, _, exs) = select.select(iw, ow, iw, 3)
            if exs:
                break
            if ins:
                for i in ins:
                    if i is soc:
                        out = self.connection
                    else:
                        out = soc
                    data = i.recv(8192)
                    if data:
                        out.send(data)
                        count = 0
            if count == max_idling:
                break

    do_HEAD = do_GET
    do_POST = do_GET
    do_PUT  = do_GET
    do_DELETE=do_GET

class ThreadingHTTPServer (socketserver.ThreadingMixIn,
                           http.server.HTTPServer):
    pass

if __name__ == '__main__':
    port=3128
    print("starting tinyproxy on port {}".format(port))
    http.server.test(ProxyHandler, ThreadingHTTPServer, port=port)
