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
	"io"
	"net/http"
	"net/url"

	"golang.org/x/net/context"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var (
	HardLinkCount = hardLinkCount
	ApiURL        = apiURL
	Download      = download

	UseDeltas  = useDeltas
	ApplyDelta = applyDelta

	GetCurrentSnap    = currentSnap
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
)

// MockDefaultRetryStrategy mocks the retry strategy used by several store requests
func MockDefaultRetryStrategy(t *testutil.BaseTest, strategy retry.Strategy) {
	originalDefaultRetryStrategy := defaultRetryStrategy
	defaultRetryStrategy = strategy
	t.AddCleanup(func() {
		defaultRetryStrategy = originalDefaultRetryStrategy
	})
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

func MockDownload(f func(ctx context.Context, name, sha3_384, downloadURL string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter) error) (restore func()) {
	origDownload := download
	download = f
	return func() {
		download = origDownload
	}
}

func MockApplyDelta(f func(name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error) (restore func()) {
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

func (sto *Store) SetDeltaFormat(dfmt string) {
	sto.deltaFormat = dfmt
}

func (sto *Store) DownloadDelta(deltaName string, downloadInfo *snap.DownloadInfo, w io.ReadWriteSeeker, pbar progress.Meter, user *auth.UserState) error {
	return sto.downloadDelta(deltaName, downloadInfo, w, pbar, user)
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
