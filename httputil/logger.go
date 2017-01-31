// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package httputil

import (
	"errors"
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

type ClientOpts struct {
	Timeout    time.Duration
	MayLogBody bool
}

// NewHTTPCLient returns a new http.Client with a LoggedTransport, a
// Timeout and preservation of range requests across redirects
func NewHTTPClient(opts *ClientOpts) *http.Client {
	if opts == nil {
		opts = &ClientOpts{}
	}

	return &http.Client{
		Transport: &LoggedTransport{
			Transport: http.DefaultTransport,
			Key:       "SNAPD_DEBUG_HTTP",
			body:      opts.MayLogBody,
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
