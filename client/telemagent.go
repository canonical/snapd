// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)


func (c *Client) DeviceSession() (deviceSession []string, err error) {
	_, err = c.doSync("GET", "/v2/devicesession", nil, nil, nil, &deviceSession)

	if len(deviceSession) != 1 {
		err = errors.New("number of macaroons found not equal to 1")
	}

	return
}

// Login logs user in.
func (client *Client) CheckEmail(email, password, otp string) (bool, error) {
	postData := loginData{
		Email:    email,
		Password: password,
		Otp:      otp,
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(postData); err != nil {
		return false, err
	}

	var user User
	if _, err := client.doSync("POST", "/v2/login", nil, nil, &body, &user); err != nil {
		return false, err
	}

	return true, nil
}

func (c *Client) Associate(email string, password string, otp string, isLogged bool) error {
	url := os.Getenv("TELEMGW_SERVICE_URL")

	if !isLogged {
		isEmailOk, err := c.CheckEmail(email, password, otp)
		if err != nil {
			return err
		}
		
		if !isEmailOk {
			return errors.New("email or password are incorrect")
		}
	}


	var payload struct {
		Email     string `json:"email"`
		Macaroon  string  `json:"macaroon"`
	}

	macaroon, err := c.DeviceSession()
	if err != nil {
		return err
	}

	payload.Macaroon = macaroon[0]
	payload.Email = email

	jsonBytes, err := json.Marshal(payload)
		if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBytes))
    req.Header.Set("Content-Type", "application/json")
	if err != nil {
        return err
    }

    client := &http.Client{
		Timeout: 10 * time.Second,
	}
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

	
    body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
    }
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("could not associate device: %s", string(body))
	}

	return nil
}