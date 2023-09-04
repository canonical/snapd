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
	"context"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"github.com/juju/ratelimit"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var (
	HardLinkCount = hardLinkCount
	ApiURL        = apiURL
	Download      = download

	ApplyDelta = applyDelta

	AuthLocation      = authLocation
	AuthURL           = authURL
	StoreURL          = storeURL
	StoreDeveloperURL = storeDeveloperURL
	MustBuy           = mustBuy

	RequestStoreMacaroon     = requestStoreMacaroon
	DischargeAuthCaveat      = dischargeAuthCaveat
	RefreshDischargeMacaroon = refreshDischargeMacaroon
	RequestStoreDeviceNonce  = requestStoreDeviceNonce
	RequestDeviceSession     = requestDeviceSession
	LoginCaveatID            = loginCaveatID

	JsonContentType  = jsonContentType
	SnapActionFields = snapActionFields

	Cancelled = cancelled
)

func MockSnapdtoolCommandFromSystemSnap(f func(name string, args ...string) (*exec.Cmd, error)) (restore func()) {
	old := commandFromSystemSnap
	commandFromSystemSnap = f
	return func() {
		commandFromSystemSnap = old
	}
}

// MockDefaultRetryStrategy mocks the retry strategy used by several store requests
func MockDefaultRetryStrategy(t *testutil.BaseTest, strategy retry.Strategy) {
	originalDefaultRetryStrategy := defaultRetryStrategy
	defaultRetryStrategy = strategy
	t.AddCleanup(func() {
		defaultRetryStrategy = originalDefaultRetryStrategy
	})
}

func MockDownloadRetryStrategy(t *testutil.BaseTest, strategy retry.Strategy) {
	originalDownloadRetryStrategy := downloadRetryStrategy
	downloadRetryStrategy = strategy
	t.AddCleanup(func() {
		downloadRetryStrategy = originalDownloadRetryStrategy
	})
}

func MockConnCheckStrategy(t *testutil.BaseTest, strategy retry.Strategy) {
	originalConnCheckStrategy := connCheckStrategy
	connCheckStrategy = strategy
	t.AddCleanup(func() {
		connCheckStrategy = originalConnCheckStrategy
	})
}

func MockDownloadSpeedParams(measureWindow time.Duration, minSpeed float64) (restore func()) {
	oldSpeedMeasureWindow := downloadSpeedMeasureWindow
	oldSpeedMin := downloadSpeedMin
	downloadSpeedMeasureWindow = measureWindow
	downloadSpeedMin = minSpeed
	return func() {
		downloadSpeedMeasureWindow = oldSpeedMeasureWindow
		downloadSpeedMin = oldSpeedMin
	}
}

func IsTransferSpeedError(err error) (ok bool, speed float64) {
	de, ok := err.(*transferSpeedError)
	if !ok {
		return false, 0
	}
	return true, de.Speed
}

func (w *TransferSpeedMonitoringWriter) MeasuredWindowsCount() int {
	return w.measuredWindows
}

func (cm *CacheManager) CacheDir() string {
	return cm.cacheDir
}

func (cm *CacheManager) Cleanup() error {
	return cm.cleanup()
}

func (cm *CacheManager) Count() int {
	return cm.count()
}

func MockOsRemove(f func(name string) error) func() {
	oldOsRemove := osRemove
	osRemove = f
	return func() {
		osRemove = oldOsRemove
	}
}

func MockDownload(f func(ctx context.Context, name, sha3_384, downloadURL string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *DownloadOptions) error) (restore func()) {
	origDownload := download
	download = f
	return func() {
		download = origDownload
	}
}

func MockDoDownloadReq(f func(ctx context.Context, storeURL *url.URL, cdnHeader string, resume int64, s *Store, user *auth.UserState) (*http.Response, error)) (restore func()) {
	orig := doDownloadReq
	doDownloadReq = f
	return func() {
		doDownloadReq = orig
	}
}

func MockApplyDelta(f func(s *Store, name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error) (restore func()) {
	origApplyDelta := applyDelta
	applyDelta = f
	return func() {
		applyDelta = origApplyDelta
	}
}

func (sto *Store) MockCacher(obs downloadCache) (restore func()) {
	oldCacher := sto.cacher
	sto.cacher = obs
	return func() {
		sto.cacher = oldCacher
	}
}

func MockHttputilNewHTTPClient(f func(opts *httputil.ClientOptions) *http.Client) (restore func()) {
	old := httputilNewHTTPClient
	httputilNewHTTPClient = f
	return func() {
		httputilNewHTTPClient = old
	}
}

func (sto *Store) SetDeltaFormat(dfmt string) {
	sto.deltaFormat = dfmt
}

func (sto *Store) DownloadDelta(deltaName string, downloadInfo *snap.DownloadInfo, w io.ReadWriteSeeker, pbar progress.Meter, user *auth.UserState, dlOpts *DownloadOptions) error {
	return sto.downloadDelta(deltaName, downloadInfo, w, pbar, user, dlOpts)
}

func (sto *Store) DoRequest(ctx context.Context, client *http.Client, reqOptions *requestOptions, user *auth.UserState) (*http.Response, error) {
	return sto.doRequest(ctx, client, reqOptions, user)
}

func (sto *Store) Client() *http.Client {
	return sto.client
}

func (sto *Store) DetailFields() []string {
	return sto.detailFields
}

func (sto *Store) DecorateOrders(snaps []*snap.Info, user *auth.UserState) error {
	return sto.decorateOrders(snaps, user)
}

func (sto *Store) SessionLock() {
	sto.auth.(*deviceAuthorizer).sessionMu.Lock()
}

func (sto *Store) SessionUnlock() {
	sto.auth.(*deviceAuthorizer).sessionMu.Unlock()
}

func (sto *Store) FindFields() []string {
	return sto.findFields
}

func (sto *Store) UseDeltas() bool {
	return sto.useDeltas()
}

func (sto *Store) Xdelta3Cmd(args ...string) *exec.Cmd {
	return sto.xdelta3CmdFunc(args...)
}

func (cfg *Config) SetBaseURL(u *url.URL) error {
	return cfg.setBaseURL(u)
}

func NewHashError(name, sha3_384, targetSha3_384 string) HashError {
	return HashError{name, sha3_384, targetSha3_384}
}

func NewRequestOptions(mth string, url *url.URL) *requestOptions {
	return &requestOptions{
		Method: mth,
		URL:    url,
	}
}

func MockRatelimitReader(f func(r io.Reader, bucket *ratelimit.Bucket) io.Reader) (restore func()) {
	oldRatelimitReader := ratelimitReader
	ratelimitReader = f
	return func() {
		ratelimitReader = oldRatelimitReader
	}
}

func MockRequestTimeout(d time.Duration) (restore func()) {
	old := requestTimeout
	requestTimeout = d
	return func() {
		requestTimeout = old
	}
}

type (
	ErrorListEntryJSON   = errorListEntry
	SnapActionResultJSON = snapActionResult
)

var ReportFetchAssertionsError = reportFetchAssertionsError
