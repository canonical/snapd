// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

func MaybeLogRetryAttempt(url string, attempt *retry.Attempt, startTime time.Time) {
	if osutil.GetenvBool("SNAPD_DEBUG") || attempt.Count() > 1 {
		logger.Debugf("Retrying %s, attempt %d, elapsed time=%v", url, attempt.Count(), time.Since(startTime))
	}
}

func maybeLogRetrySummary(startTime time.Time, url string, attempt *retry.Attempt, resp *http.Response, err error) {
	if osutil.GetenvBool("SNAPD_DEBUG") || attempt.Count() > 1 {
		var status string
		if err != nil {
			status = err.Error()
		} else if resp != nil {
			status = fmt.Sprintf("%d", resp.StatusCode)
		}
		logger.Debugf("The retry loop for %s finished after %d retries, elapsed time=%v, status: %s", url, attempt.Count(), time.Since(startTime), status)
	}
}

func ShouldRetryHttpResponse(attempt *retry.Attempt, resp *http.Response) bool {
	if !attempt.More() {
		return false
	}
	return resp.StatusCode >= 500
}

func ShouldRetryError(attempt *retry.Attempt, err error) bool {
	if !attempt.More() {
		return false
	}
	if urlErr, ok := err.(*url.Error); ok {
		err = urlErr.Err
	}
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			logger.Debugf("Retrying because of: %s", netErr)
			return true
		}
	}
	// The CDN sometimes resets the connection (LP:#1617765), also
	// retry in this case
	if opErr, ok := err.(*net.OpError); ok {
		// peeling the onion
		if syscallErr, ok := opErr.Err.(*os.SyscallError); ok {
			if syscallErr.Err == syscall.ECONNRESET {
				logger.Debugf("Retrying because of: %s", opErr)
				return true
			}
			// FIXME: code below is not (unit) tested and
			// it is unclear if we need it with the new
			// opErr.Temporary() "if" below
			if opErr.Op == "dial" {
				logger.Debugf("Retrying because of: %#v (syscall error: %#v)", opErr, syscallErr.Err)
				return true
			}
			logger.Debugf("Encountered syscall error: %#v", syscallErr)
		}

		// If we are unable to talk to a DNS go1.9+ will set
		// opErr.IsTemporary - we also support go1.6 so we need to
		// add a workaround here. This block can go away once we
		// use go1.9+ only.
		if dnsErr, ok := opErr.Err.(*net.DNSError); ok {
			// The horror, the horror
			// TODO: stop Arch to use the cgo resolver
			// which requires the right side of the OR
			if strings.Contains(dnsErr.Err, "connection refused") || strings.Contains(dnsErr.Err, "Temporary failure in name resolution") {
				logger.Debugf("Retrying because of temporary net error (DNS): %#v", dnsErr)
				return true
			}
		}

		// Retry for temporary network errors (like dns errors in 1.9+)
		if opErr.Temporary() {
			logger.Debugf("Retrying because of temporary net error: %#v", opErr)
			return true
		}
		logger.Debugf("Encountered non temporary net.OpError: %#v", opErr)
	}

	if err == io.ErrUnexpectedEOF || err == io.EOF {
		logger.Debugf("Retrying because of: %s", err)
		return true
	}

	if osutil.GetenvBool("SNAPD_DEBUG") {
		logger.Debugf("Not retrying: %#v", err)
	}

	return false
}

// RetryRequest calls doRequest and read the response body in a retry loop using the given retryStrategy.
func RetryRequest(endpoint string, doRequest func() (*http.Response, error), readResponseBody func(resp *http.Response) error, retryStrategy retry.Strategy) (resp *http.Response, err error) {
	var attempt *retry.Attempt
	startTime := time.Now()
	for attempt = retry.Start(retryStrategy, nil); attempt.Next(); {
		MaybeLogRetryAttempt(endpoint, attempt, startTime)

		resp, err = doRequest()
		if err != nil {
			if ShouldRetryError(attempt, err) {
				continue
			}
			break
		}

		if ShouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		} else {
			err := readResponseBody(resp)
			resp.Body.Close()
			if err != nil {
				if ShouldRetryError(attempt, err) {
					continue
				} else {
					maybeLogRetrySummary(startTime, endpoint, attempt, resp, err)
					return nil, err
				}
			}
		}
		// break out from retry loop
		break
	}
	maybeLogRetrySummary(startTime, endpoint, attempt, resp, err)

	return resp, err
}
