#!/usr/bin/python3

import os
import sys
import urllib.request

from http.server import HTTPServer, SimpleHTTPRequestHandler


class XkcdRequestHandler(SimpleHTTPRequestHandler):

    XKCD_URL = "http://xkcd.com/"
    XKCD_IMG_URL = "http://imgs.xkcd.com/"

    def _mini_proxy(self, url):
        fp = urllib.request.urlopen(url)
        body = fp.read()
        info = fp.info()
        self.send_response(200, "ok")
        for k, v in info.items():
            self.send_header(k, v)
            self.end_headers()
            self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/xkcd/"):
            url = self.XKCD_URL + self.path[len("/xkcd/"):]
            return self._mini_proxy(url)
        elif self.path.startswith("/img/xkcd/"):
            url = self.XKCD_IMG_URL + self.path[len("/img/xkcd/"):]
            return self._mini_proxy(url)
        else:
            return super(XkcdRequestHandler, self).do_GET()


if __name__ == "__main__":
    # we start in the snappy base directory, ensure we are in "www"
    os.chdir(os.path.dirname(__file__) + "/../www")

    if len(sys.argv) > 1:
        port = int(sys.argv[1])
    else:
        port = 80

    httpd = HTTPServer(('', port), XkcdRequestHandler)
    httpd.serve_forever()
