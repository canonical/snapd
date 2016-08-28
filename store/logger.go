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
}

// RoundTrip is from the http.RoundTripper interface.
func (tr *LoggedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	flags := tr.getFlags()

	if flags.debugRequest() {
		buf, _ := httputil.DumpRequestOut(req, flags.debugBody())
		logger.Debugf("> %q", buf)
	}

	rsp, err := tr.Transport.RoundTrip(req)

	if err == nil && flags.debugResponse() {
		buf, _ := httputil.DumpResponse(rsp, flags.debugBody())
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

// returns a new http.Client with a LoggedTransport and a Timeout
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &LoggedTransport{
			Transport: http.DefaultTransport,
			Key:       "SNAPD_DEBUG_HTTP",
		},
		Timeout: 10 * time.Second,
	}
}
