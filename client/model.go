// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"

	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
)

type remodelData struct {
	NewModel string `json:"new-model"`
	Offline  bool   `json:"offline,omitempty"`
}

// RemodelOpts defines options to be used when remodeling the system.
type RemodelOpts struct {
	// Offline indicates whether the remodel should be done offline. If true,
	// the remodel will be attempted to be done without contacting the store.
	Offline bool
}

// Remodel tries to remodel the system with the given assertion data
func (client *Client) Remodel(b []byte, opts RemodelOpts) (changeID string, err error) {
	data, err := json.Marshal(&remodelData{
		NewModel: string(b),
		Offline:  opts.Offline,
	})
	if err != nil {
		return "", fmt.Errorf("cannot marshal remodel data: %v", err)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", "/v2/model", nil, headers, bytes.NewReader(data))
}

// RemodelWithLocalSnaps tries to remodel the system with the given model
// assertion and local snaps and assertion files. Remodeling using this method
// will ensure that snapd does not contact the store.
func (client *Client) RemodelWithLocalSnaps(
	model []byte, snapPaths, assertPaths []string) (changeID string, err error) {

	// Check if all files exist before starting the go routine
	snapFiles, err := checkAndOpenFiles(snapPaths)
	if err != nil {
		return "", err
	}
	assertsFiles, err := checkAndOpenFiles(assertPaths)
	if err != nil {
		return "", err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go sendRemodelFiles(model, snapPaths, snapFiles, assertsFiles, pw, mw)

	headers := map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}

	_, changeID, err = client.doAsyncFull("POST", "/v2/model", nil, headers, pr, doNoTimeoutAndRetry)
	return changeID, err
}

func checkAndOpenFiles(paths []string) ([]*os.File, error) {
	var files []*os.File
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			for _, openFile := range files {
				openFile.Close()
			}
			return nil, fmt.Errorf("cannot open %q: %w", path, err)
		}

		files = append(files, f)
	}

	return files, nil
}

func createAssertionPart(name string, mw *multipart.Writer) (io.Writer, error) {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"`, name))
	h.Set("Content-Type", asserts.MediaType)
	return mw.CreatePart(h)
}

func sendRemodelFiles(model []byte, paths []string, files, assertFiles []*os.File, pw *io.PipeWriter, mw *multipart.Writer) {
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()

	w, err := createAssertionPart("new-model", mw)
	if err != nil {
		pw.CloseWithError(err)
		return
	}
	_, err = w.Write(model)
	if err != nil {
		pw.CloseWithError(err)
		return
	}

	for _, file := range assertFiles {
		if err := sendPartFromFile(file,
			func() (io.Writer, error) {
				return createAssertionPart("assertion", mw)
			}); err != nil {
			pw.CloseWithError(err)
			return
		}
	}

	for i, file := range files {
		if err := sendPartFromFile(file,
			func() (io.Writer, error) {
				return mw.CreateFormFile("snap", filepath.Base(paths[i]))
			}); err != nil {
			pw.CloseWithError(err)
			return
		}
	}

	mw.Close()
	pw.Close()
}

func sendPartFromFile(file *os.File, writeHeader func() (io.Writer, error)) error {
	fw, err := writeHeader()
	if err != nil {
		return err
	}

	_, err = io.Copy(fw, file)
	if err != nil {
		return err
	}

	return nil
}

// CurrentModelAssertion returns the current model assertion
func (client *Client) CurrentModelAssertion() (*asserts.Model, error) {
	assert, err := currentAssertion(client, "/v2/model")
	if err != nil {
		return nil, err
	}
	modelAssert, ok := assert.(*asserts.Model)
	if !ok {
		return nil, fmt.Errorf("unexpected assertion type (%s) returned", assert.Type().Name)
	}
	return modelAssert, nil
}

// CurrentSerialAssertion returns the current serial assertion
func (client *Client) CurrentSerialAssertion() (*asserts.Serial, error) {
	assert, err := currentAssertion(client, "/v2/model/serial")
	if err != nil {
		return nil, err
	}
	serialAssert, ok := assert.(*asserts.Serial)
	if !ok {
		return nil, fmt.Errorf("unexpected assertion type (%s) returned", assert.Type().Name)
	}
	return serialAssert, nil
}

// helper function for getting assertions from the daemon via a REST path
func currentAssertion(client *Client, path string) (asserts.Assertion, error) {
	q := url.Values{}

	response, cancel, err := client.rawWithTimeout(context.Background(), "GET", path, q, nil, nil, nil)
	if err != nil {
		fmt := "failed to query current assertion: %w"
		return nil, xerrors.Errorf(fmt, err)
	}
	defer cancel()
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, parseError(response)
	}

	dec := asserts.NewDecoder(response.Body)

	// only decode a single assertion - we can't ever get more than a single
	// assertion through these endpoints by design
	assert, err := dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("failed to decode assertions: %v", err)
	}

	return assert, nil
}
