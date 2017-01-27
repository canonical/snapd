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

package store

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"gopkg.in/retry.v1"
)

// the LimitTime should be slightly more than 3 times of our http.Client
// Timeout value
var defaultRetryStrategy = retry.LimitCount(5, retry.LimitTime(33*time.Second,
	retry.Exponential{
		Initial: 100 * time.Millisecond,
		Factor:  2.5,
	},
))

func maybeLogRetryAttempt(url string, attempt *retry.Attempt, startTime time.Time) {
	if osutil.GetenvBool("SNAPPY_TESTING") || attempt.Count() > 1 {
		logger.Debugf("Retyring %s, attempt %d, delta time=%v ms", url, attempt.Count(), time.Since(startTime))
	}

}

func logRetryTime(startTime time.Time, url string, attempt *retry.Attempt, resp *http.Response, err error) {
	var status string
	delta := time.Since(startTime) / time.Millisecond
	if err != nil {
		status = err.Error()
	} else if resp != nil {
		status = fmt.Sprintf("%d", resp.StatusCode)
	}
	logger.Debugf("The retry loop for %s finished after %d retries, delta time=%v ms, status: %s", url, attempt.Count(), delta, status)
}

func shouldRetryHttpResponse(attempt *retry.Attempt, resp *http.Response) bool {
	return (resp.StatusCode == 500 || resp.StatusCode == 503) && attempt.More()
}

func shouldRetryError(attempt *retry.Attempt, err error) bool {
	if !attempt.More() {
		return false
	}
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}
	return err == io.ErrUnexpectedEOF || err == io.EOF
}
