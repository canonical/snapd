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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
)

type SnapOptions struct {
	Channel string `json:"channel,omitempty"`
	DevMode bool   `json:"devmode,omitempty"`
}

type actionData struct {
	Action   string `json:"action"`
	Name     string `json:"name,omitempty"`
	SnapPath string `json:"snap-path,omitempty"`
	*SnapOptions
}

// Install adds the snap with the given name from the given channel (or
// the system default channel if not).
func (client *Client) Install(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("install", name, options)
}

// Remove removes the snap with the given name.
func (client *Client) Remove(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("remove", name, options)
}

// Refresh refreshes the snap with the given name (switching it to track
// the given channel if given).
func (client *Client) Refresh(name string, options *SnapOptions) (changeID string, err error) {
	return client.doSnapAction("refresh", name, options)
}

func (client *Client) doSnapAction(actionName string, snapName string, options *SnapOptions) (changeID string, err error) {
	action := actionData{
		Action:      actionName,
		Name:        snapName,
		SnapOptions: options,
	}
	data, err := json.Marshal(&action)
	if err != nil {
		return "", fmt.Errorf("cannot marshal snap options: %s", err)
	}
	path := fmt.Sprintf("/v2/snaps/%s", snapName)
	return client.doAsync("POST", path, nil, nil, bytes.NewBuffer(data))
}

// InstallPath sideloads the snap with the given path, returning the UUID
// of the background operation upon success.
func (client *Client) InstallPath(path string, options *SnapOptions) (changeID string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open: %q", path)
	}

	action := actionData{
		Action:      "install",
		SnapPath:    path,
		SnapOptions: options,
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go sendSnapFile(path, f, pw, mw, &action)

	headers := map[string]string{
		"Content-Type": mw.FormDataContentType(),
	}

	return client.doAsync("POST", "/v2/snaps", nil, headers, pr)
}

func sendSnapFile(snapPath string, snapFile *os.File, pw *io.PipeWriter, mw *multipart.Writer, action *actionData) {
	defer snapFile.Close()

	if action.SnapOptions == nil {
		action.SnapOptions = &SnapOptions{}
	}
	errs := []error{
		mw.WriteField("action", action.Action),
		mw.WriteField("name", action.Name),
		mw.WriteField("snap-path", action.SnapPath),
		mw.WriteField("channel", action.Channel),
		mw.WriteField("devmode", strconv.FormatBool(action.DevMode)),
	}
	for _, err := range errs {
		if err != nil {
			pw.CloseWithError(err)
			return
		}
	}

	fw, err := mw.CreateFormFile("snap", filepath.Base(snapPath))
	if err != nil {
		pw.CloseWithError(err)
		return
	}

	_, err = io.Copy(fw, snapFile)
	if err != nil {
		pw.CloseWithError(err)
		return
	}

	mw.Close()
	pw.Close()
}
