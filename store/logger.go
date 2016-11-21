// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package store

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"

	"github.com/snapcore/snapd/logger"
)

type debugflag uint

// set these via the Key environ
const (
	DebugRequest = debugflag(1 << iota)
	DebugResponse
	DebugBody
)

func (f debugflag) debugRequest() bool {
	return f&DebugRequest != 0
}

func (f debugflag) debugResponse() bool {
	return f&DebugResponse != 0
}

func (f debugflag) debugBody() bool {
	return f&DebugBody != 0
}

// LoggedTransport is an http.RoundTripper that can be used by
// http.Client to log request/response roundtrips.
type LoggedTransport struct {
	Transport http.RoundTripper
	Key       string
	body      bool
}

// RoundTrip is from the http.RoundTripper interface.
func (tr *LoggedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	flags := tr.getFlags()

	if flags.debugRequest() {
		buf, _ := httputil.DumpRequestOut(req, tr.body && flags.debugBody())
		logger.Debugf("> %q", buf)
	}

	rsp, err := tr.Transport.RoundTrip(req)

	if err == nil && flags.debugResponse() {
		buf, _ := httputil.DumpResponse(rsp, tr.body && flags.debugBody())
		logger.Debugf("< %q", buf)
	}

	return rsp, err
}

func (tr *LoggedTransport) getFlags() debugflag {
	flags, err := strconv.Atoi(os.Getenv(tr.Key))
	if err != nil {
		flags = 0
	}

	return debugflag(flags)
}

var validCertFps [2][]byte

func init() {
	var err error

	// obtained via:
	// python3 -c 'import ssl,socket,hashlib,binascii; conn=ssl.create_default_context().wrap_socket(socket.socket(socket.AF_INET), server_hostname="search.apps.ubuntu.com"); conn.connect(("search.apps.ubuntu.com", 443));sha256=hashlib.sha256();sha256.update(conn.getpeercert(binary_form=True));print(binascii.hexlify(sha256.digest()))'
	// python3 -c 'import ssl,socket,hashlib,binascii; conn=ssl.create_default_context().wrap_socket(socket.socket(socket.AF_INET), server_hostname="assertions.ubuntu.com"); conn.connect(("assertions.ubuntu.com", 443));sha256=hashlib.sha256();sha256.update(conn.getpeercert(binary_form=True));print(binascii.hexlify(sha256.digest()))'
	for i, h := range []string{
		"12b4681522999af0f46a1965cdacbbfa376370644a0606f441a33fb26952d3ef",
		"be1294e0682fe95ea1b692bb002e642763d6cba61f48d8437f4ed6f87a6c0e7d",
	} {
		validCertFps[i], err = hex.DecodeString(h)
		if err != nil {
			panic("internal error cannot decode validCertFp")
		}
	}
}

func pinnedDialTLS(network, addr string) (net.Conn, error) {
	c, err := tls.Dial(network, addr, nil)
	if err != nil {
		return nil, err
	}

	for _, peercert := range c.ConnectionState().PeerCertificates {
		fp := sha256.Sum256(peercert.Raw)
		for _, validCertFp := range validCertFps {
			if bytes.Equal(fp[0:], validCertFp) {
				return c, nil
			}
		}
	}

	return nil, fmt.Errorf("cannot find expected security cert for %v %v: valid %v", network, addr, validCertFps)
}

type httpClientOpts struct {
	Timeout    time.Duration
	MayLogBody bool
}

// returns a new http.Client with a LoggedTransport, a Timeout and preservation
// of range requests across redirects
func newHTTPClient(opts *httpClientOpts) *http.Client {
	if opts == nil {
		opts = &httpClientOpts{}
	}

	return &http.Client{
		Transport: &LoggedTransport{
			Transport: &http.Transport{
				DialTLS: pinnedDialTLS,
			},

			Key:  "SNAPD_DEBUG_HTTP",
			body: opts.MayLogBody,
		},
		Timeout: opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return errors.New("stopped after 10 redirects")
			}
			// preserve the range header across redirects
			// to the CDN
			v := via[0].Header.Get("Range")
			req.Header.Set("Range", v)
			return nil
		},
	}
}
