// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/juju/ratelimit"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdtool"
)

var downloadRetryStrategy = retry.LimitCount(7, retry.LimitTime(90*time.Second,
	retry.Exponential{
		Initial: 500 * time.Millisecond,
		Factor:  2.5,
	},
))

// Deltas enabled by default on classic, but allow opting in or out on both classic and core.
func useDeltas() bool {
	// only xdelta3 is supported for now, so check the binary exists here
	// TODO: have a per-format checker instead
	if _, err := getXdelta3Cmd(); err != nil {
		return false
	}

	return osutil.GetenvBool("SNAPD_USE_DELTAS_EXPERIMENTAL", true)
}

func (s *Store) cdnHeader() (string, error) {
	if s.noCDN {
		return "none", nil
	}

	if s.dauthCtx == nil {
		return "", nil
	}

	// set Snap-CDN from cloud instance information
	// if available

	// TODO: do we want a more complex retry strategy
	// where we first to send this header and if the
	// operation fails that way to even get the connection
	// then we retry without sending this?

	cloudInfo, err := s.dauthCtx.CloudInfo()
	if err != nil {
		return "", err
	}

	if cloudInfo != nil {
		cdnParams := []string{fmt.Sprintf("cloud-name=%q", cloudInfo.Name)}
		if cloudInfo.Region != "" {
			cdnParams = append(cdnParams, fmt.Sprintf("region=%q", cloudInfo.Region))
		}
		if cloudInfo.AvailabilityZone != "" {
			cdnParams = append(cdnParams, fmt.Sprintf("availability-zone=%q", cloudInfo.AvailabilityZone))
		}

		return strings.Join(cdnParams, " "), nil
	}

	return "", nil
}

type HashError struct {
	name           string
	sha3_384       string
	targetSha3_384 string
}

func (e HashError) Error() string {
	return fmt.Sprintf("sha3-384 mismatch for %q: got %s but expected %s", e.name, e.sha3_384, e.targetSha3_384)
}

type DownloadOptions struct {
	RateLimit           int64
	IsAutoRefresh       bool
	LeavePartialOnError bool
}

// Download downloads the snap addressed by download info and returns its
// filename.
// The file is saved in temporary storage, and should be removed
// after use to prevent the disk from running out of space.
func (s *Store) Download(ctx context.Context, name string, targetPath string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *DownloadOptions) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	if err := s.cacher.Get(downloadInfo.Sha3_384, targetPath); err == nil {
		logger.Debugf("Cache hit for SHA3_384 …%.5s.", downloadInfo.Sha3_384)
		return nil
	}

	if useDeltas() {
		logger.Debugf("Available deltas returned by store: %v", downloadInfo.Deltas)

		if len(downloadInfo.Deltas) == 1 {
			err := s.downloadAndApplyDelta(name, targetPath, downloadInfo, pbar, user, dlOpts)
			if err == nil {
				return nil
			}
			// We revert to normal downloads if there is any error.
			logger.Noticef("Cannot download or apply deltas for %s: %v", name, err)
		}
	}

	partialPath := targetPath + ".partial"
	w, err := os.OpenFile(partialPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	resume, err := w.Seek(0, os.SEEK_END)
	if err != nil {
		return err
	}
	defer func() {
		fi, _ := w.Stat()
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err == nil {
			return
		}
		if dlOpts == nil || !dlOpts.LeavePartialOnError || fi == nil || fi.Size() == 0 {
			os.Remove(w.Name())
		}
	}()
	if resume > 0 {
		logger.Debugf("Resuming download of %q at %d.", partialPath, resume)
	} else {
		logger.Debugf("Starting download of %q.", partialPath)
	}

	authAvail, err := s.authAvailable(user)
	if err != nil {
		return err
	}

	url := downloadInfo.AnonDownloadURL
	if url == "" || authAvail {
		url = downloadInfo.DownloadURL
	}

	if downloadInfo.Size == 0 || resume < downloadInfo.Size {
		err = download(ctx, name, downloadInfo.Sha3_384, url, user, s, w, resume, pbar, dlOpts)
		if err != nil {
			logger.Debugf("download of %q failed: %#v", url, err)
		}
	} else {
		// we're done! check the hash though
		h := crypto.SHA3_384.New()
		if _, err := w.Seek(0, os.SEEK_SET); err != nil {
			return err
		}
		if _, err := io.Copy(h, w); err != nil {
			return err
		}
		actualSha3 := fmt.Sprintf("%x", h.Sum(nil))
		if downloadInfo.Sha3_384 != actualSha3 {
			err = HashError{name, actualSha3, downloadInfo.Sha3_384}
		}
	}
	// If hashsum is incorrect retry once
	if _, ok := err.(HashError); ok {
		logger.Debugf("Hashsum error on download: %v", err.Error())
		logger.Debugf("Truncating and trying again from scratch.")
		err = w.Truncate(0)
		if err != nil {
			return err
		}
		_, err = w.Seek(0, os.SEEK_SET)
		if err != nil {
			return err
		}
		err = download(ctx, name, downloadInfo.Sha3_384, url, user, s, w, 0, pbar, nil)
		if err != nil {
			logger.Debugf("download of %q failed: %#v", url, err)
		}
	}

	if err != nil {
		return err
	}

	if err := os.Rename(w.Name(), targetPath); err != nil {
		return err
	}

	if err := w.Sync(); err != nil {
		return err
	}

	return s.cacher.Put(downloadInfo.Sha3_384, targetPath)
}

func downloadReqOpts(storeURL *url.URL, cdnHeader string, opts *DownloadOptions) *requestOptions {
	reqOptions := requestOptions{
		Method:       "GET",
		URL:          storeURL,
		ExtraHeaders: map[string]string{},
		// FIXME: use the new headers? with
		// APILevel: apiV2Endps,
	}
	if cdnHeader != "" {
		reqOptions.ExtraHeaders["Snap-CDN"] = cdnHeader
	}
	if opts != nil && opts.IsAutoRefresh {
		reqOptions.ExtraHeaders["Snap-Refresh-Reason"] = "scheduled"
	}

	return &reqOptions
}

var ratelimitReader = ratelimit.Reader

var download = downloadImpl

// download writes an http.Request showing a progress.Meter
func downloadImpl(ctx context.Context, name, sha3_384, downloadURL string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *DownloadOptions) error {
	if dlOpts == nil {
		dlOpts = &DownloadOptions{}
	}

	storeURL, err := url.Parse(downloadURL)
	if err != nil {
		return err
	}

	cdnHeader, err := s.cdnHeader()
	if err != nil {
		return err
	}

	var finalErr error
	var dlSize float64
	startTime := time.Now()
	for attempt := retry.Start(downloadRetryStrategy, nil); attempt.Next(); {
		reqOptions := downloadReqOpts(storeURL, cdnHeader, dlOpts)

		httputil.MaybeLogRetryAttempt(reqOptions.URL.String(), attempt, startTime)

		h := crypto.SHA3_384.New()

		if resume > 0 {
			reqOptions.ExtraHeaders["Range"] = fmt.Sprintf("bytes=%d-", resume)
			// seed the sha3 with the already local file
			if _, err := w.Seek(0, os.SEEK_SET); err != nil {
				return err
			}
			n, err := io.Copy(h, w)
			if err != nil {
				return err
			}
			if n != resume {
				return fmt.Errorf("resume offset wrong: %d != %d", resume, n)
			}
		}

		if cancelled(ctx) {
			return fmt.Errorf("The download has been cancelled: %s", ctx.Err())
		}
		var resp *http.Response
		cli := s.newHTTPClient(nil)
		resp, finalErr = s.doRequest(ctx, cli, reqOptions, user)

		if cancelled(ctx) {
			return fmt.Errorf("The download has been cancelled: %s", ctx.Err())
		}
		if finalErr != nil {
			if httputil.ShouldRetryAttempt(attempt, finalErr) {
				continue
			}
			break
		}
		if resume > 0 && resp.StatusCode != 206 {
			logger.Debugf("server does not support resume")
			if _, err := w.Seek(0, os.SEEK_SET); err != nil {
				return err
			}
			h = crypto.SHA3_384.New()
			resume = 0
		}
		if httputil.ShouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		}

		defer resp.Body.Close()

		switch resp.StatusCode {
		case 200, 206: // OK, Partial Content
		case 402: // Payment Required

			return fmt.Errorf("please buy %s before installing it.", name)
		default:
			return &DownloadError{Code: resp.StatusCode, URL: resp.Request.URL}
		}

		if pbar == nil {
			pbar = progress.Null
		}
		dlSize = float64(resp.ContentLength)
		pbar.Start(name, dlSize)
		mw := io.MultiWriter(w, h, pbar)
		var limiter io.Reader
		limiter = resp.Body
		if limit := dlOpts.RateLimit; limit > 0 {
			bucket := ratelimit.NewBucketWithRate(float64(limit), 2*limit)
			limiter = ratelimitReader(resp.Body, bucket)
		}
		_, finalErr = io.Copy(mw, limiter)
		pbar.Finished()
		if finalErr != nil {
			if httputil.ShouldRetryAttempt(attempt, finalErr) {
				// error while downloading should resume
				var seekerr error
				resume, seekerr = w.Seek(0, os.SEEK_END)
				if seekerr == nil {
					continue
				}
				// if seek failed, then don't retry end return the original error
			}
			break
		}

		if cancelled(ctx) {
			return fmt.Errorf("The download has been cancelled: %s", ctx.Err())
		}

		actualSha3 := fmt.Sprintf("%x", h.Sum(nil))
		if sha3_384 != "" && sha3_384 != actualSha3 {
			finalErr = HashError{name, actualSha3, sha3_384}
		}
		break
	}
	if finalErr == nil {
		// not using quantity.FormatFoo as this is just for debug
		dt := time.Since(startTime)
		r := dlSize / dt.Seconds()
		var p rune
		for _, p = range " kMGTPEZY" {
			if r < 1000 {
				break
			}
			r /= 1000
		}

		logger.Debugf("Download succeeded in %.03fs (%.0f%cB/s).", dt.Seconds(), r, p)
	}
	return finalErr
}

// DownloadStream will copy the snap from the request to the io.Reader
func (s *Store) DownloadStream(ctx context.Context, name string, downloadInfo *snap.DownloadInfo, resume int64, user *auth.UserState) (io.ReadCloser, int, error) {
	// XXX: coverage of this is rather poor
	if path := s.cacher.GetPath(downloadInfo.Sha3_384); path != "" {
		logger.Debugf("Cache hit for SHA3_384 …%.5s.", downloadInfo.Sha3_384)
		file, err := os.OpenFile(path, os.O_RDONLY, 0600)
		if err != nil {
			return nil, 0, err
		}
		if resume == 0 {
			return file, 200, nil
		}
		_, err = file.Seek(resume, os.SEEK_SET)
		if err != nil {
			return nil, 0, err
		}
		return file, 206, nil
	}

	authAvail, err := s.authAvailable(user)
	if err != nil {
		return nil, 0, err
	}

	downloadURL := downloadInfo.AnonDownloadURL
	if downloadURL == "" || authAvail {
		downloadURL = downloadInfo.DownloadURL
	}

	storeURL, err := url.Parse(downloadURL)
	if err != nil {
		return nil, 0, err
	}

	cdnHeader, err := s.cdnHeader()
	if err != nil {
		return nil, 0, err
	}

	resp, err := doDownloadReq(ctx, storeURL, cdnHeader, resume, s, user)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.StatusCode, nil
}

var doDownloadReq = doDownloadReqImpl

func doDownloadReqImpl(ctx context.Context, storeURL *url.URL, cdnHeader string, resume int64, s *Store, user *auth.UserState) (*http.Response, error) {
	reqOptions := downloadReqOpts(storeURL, cdnHeader, nil)
	if resume > 0 {
		reqOptions.ExtraHeaders["Range"] = fmt.Sprintf("bytes=%d-", resume)
	}
	cli := s.newHTTPClient(nil)
	return s.doRequest(ctx, cli, reqOptions, user)
}

// downloadDelta downloads the delta for the preferred format, returning the path.
func (s *Store) downloadDelta(deltaName string, downloadInfo *snap.DownloadInfo, w io.ReadWriteSeeker, pbar progress.Meter, user *auth.UserState, dlOpts *DownloadOptions) error {

	if len(downloadInfo.Deltas) != 1 {
		return errors.New("store returned more than one download delta")
	}

	deltaInfo := downloadInfo.Deltas[0]

	if deltaInfo.Format != s.deltaFormat {
		return fmt.Errorf("store returned unsupported delta format %q (only xdelta3 currently)", deltaInfo.Format)
	}

	authAvail, err := s.authAvailable(user)
	if err != nil {
		return err
	}

	url := deltaInfo.AnonDownloadURL
	if url == "" || authAvail {
		url = deltaInfo.DownloadURL
	}

	return download(context.TODO(), deltaName, deltaInfo.Sha3_384, url, user, s, w, 0, pbar, dlOpts)
}

func getXdelta3Cmd(args ...string) (*exec.Cmd, error) {
	if osutil.ExecutableExists("xdelta3") {
		return exec.Command("xdelta3", args...), nil
	}
	return snapdtool.CommandFromSystemSnap("/usr/bin/xdelta3", args...)
}

// applyDelta generates a target snap from a previously downloaded snap and a downloaded delta.
var applyDelta = func(name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
	snapBase := fmt.Sprintf("%s_%d.snap", name, deltaInfo.FromRevision)
	snapPath := filepath.Join(dirs.SnapBlobDir, snapBase)

	if !osutil.FileExists(snapPath) {
		return fmt.Errorf("snap %q revision %d not found at %s", name, deltaInfo.FromRevision, snapPath)
	}

	if deltaInfo.Format != "xdelta3" {
		return fmt.Errorf("cannot apply unsupported delta format %q (only xdelta3 currently)", deltaInfo.Format)
	}

	partialTargetPath := targetPath + ".partial"

	xdelta3Args := []string{"-d", "-s", snapPath, deltaPath, partialTargetPath}
	cmd, err := getXdelta3Cmd(xdelta3Args...)
	if err != nil {
		return err
	}

	if err := cmd.Run(); err != nil {
		if err := os.Remove(partialTargetPath); err != nil {
			logger.Noticef("failed to remove partial delta target %q: %s", partialTargetPath, err)
		}
		return err
	}

	if err := os.Chmod(partialTargetPath, 0600); err != nil {
		return err
	}

	bsha3_384, _, err := osutil.FileDigest(partialTargetPath, crypto.SHA3_384)
	if err != nil {
		return err
	}
	sha3_384 := fmt.Sprintf("%x", bsha3_384)
	if targetSha3_384 != "" && sha3_384 != targetSha3_384 {
		if err := os.Remove(partialTargetPath); err != nil {
			logger.Noticef("failed to remove partial delta target %q: %s", partialTargetPath, err)
		}
		return HashError{name, sha3_384, targetSha3_384}
	}

	if err := os.Rename(partialTargetPath, targetPath); err != nil {
		return osutil.CopyFile(partialTargetPath, targetPath, 0)
	}

	return nil
}

// downloadAndApplyDelta downloads and then applies the delta to the current snap.
func (s *Store) downloadAndApplyDelta(name, targetPath string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *DownloadOptions) error {
	deltaInfo := &downloadInfo.Deltas[0]

	deltaPath := fmt.Sprintf("%s.%s-%d-to-%d.partial", targetPath, deltaInfo.Format, deltaInfo.FromRevision, deltaInfo.ToRevision)
	deltaName := fmt.Sprintf(i18n.G("%s (delta)"), name)

	w, err := os.OpenFile(deltaPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		os.Remove(deltaPath)
	}()

	err = s.downloadDelta(deltaName, downloadInfo, w, pbar, user, dlOpts)
	if err != nil {
		return err
	}

	logger.Debugf("Successfully downloaded delta for %q at %s", name, deltaPath)
	if err := applyDelta(name, deltaPath, deltaInfo, targetPath, downloadInfo.Sha3_384); err != nil {
		return err
	}

	logger.Debugf("Successfully applied delta for %q at %s, saving %d bytes.", name, deltaPath, downloadInfo.Size-deltaInfo.Size)
	return nil
}

func (s *Store) CacheDownloads() int {
	return s.cfg.CacheDownloads
}

func (s *Store) SetCacheDownloads(fileCount int) {
	s.cfg.CacheDownloads = fileCount
	if fileCount > 0 {
		s.cacher = NewCacheManager(dirs.SnapDownloadCacheDir, fileCount)
	} else {
		s.cacher = &nullCache{}
	}
}
