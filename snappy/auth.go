package snappy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"
)

var (
	ubuntuoneAPIBase  = "https://login.ubuntu.com/api/v2"
	ubuntuoneOauthAPI = ubuntuoneAPIBase + "/tokens/oauth"
)

type StoreToken struct {
	OpenID      string `json:"openid"`
	TokenName   string `json:"token_name"`
	DateUpdated string `json:"date_updated"`
	DateCreated string `json:"date_created"`
	Href        string `json:"href"`

	TokenKey       string `json:"token_key"`
	TokenSecret    string `json:"token_secret"`
	ConsumerSecret string `json:"consumer_secret"`
	ConsumerKey    string `json:"consumer_key"`
}

// RequestStoreToken requests a token for accessing the ubuntu store
func RequestStoreToken(username, password, tokenName, otp string) (*StoreToken, error) {
	data := map[string]string{
		"email":      username,
		"password":   password,
		"token_name": tokenName,
	}
	if otp != "" {
		data["otp"] = otp
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", ubuntuoneOauthAPI, strings.NewReader(string(jsonData)))
	req.Header.Set("content-type", "application/json")
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == 403:
		return nil, errors.New("invalid credentials")
	case resp.StatusCode != 200 && resp.StatusCode != 201:
		return nil, fmt.Errorf("failed to get store token: %v (%v)", resp.StatusCode, resp)
	}

	var token StoreToken
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&token); err != nil {
		return nil, err
	}

	return &token, nil
}

func storeTokenFilename() string {
	homeDir, _ := helpers.CurrentHomeDir()
	return filepath.Join(homeDir, ".config", "snappy", "sso.json")
}

// WriteStoreToken takes the token and stores it on the filesystem for
// later reading via ReadStoreToken()
func WriteStoreToken(token StoreToken) error {
	targetFile := storeTokenFilename()
	if err := helpers.EnsureDir(filepath.Dir(targetFile), 0700); err != nil {
		return err
	}
	outStr, err := json.MarshalIndent(token, "", " ")
	if err != nil {
		return nil
	}

	return helpers.AtomicWriteFile(targetFile, []byte(outStr), 0600)
}

// ReadStoreToken reads a token previously write via WriteStoreToken
func ReadStoreToken() (*StoreToken, error) {
	targetFile := storeTokenFilename()
	tokenContent, err := ioutil.ReadFile(targetFile)
	if err != nil {
		return nil, err
	}

	var readStoreToken StoreToken
	if err := json.Unmarshal(tokenContent, &readStoreToken); err != nil {
		return nil, err
	}

	return &readStoreToken, nil
}

// FIXME: replace with a real oauth1 library - or wait until oauth2 becomes
// available
//
// minimal oauth v1 signature
func makeOauthPlaintextSignature(req *http.Request, token *StoreToken) string {
	s := fmt.Sprintf(`OAuth oauth_version="1.0", oauth_signature_method="PLAINTEXT", oauth_consumer_key="%s", oauth_token="%s", oauth_signature="%s%26%s"`, token.ConsumerKey, token.TokenKey, token.ConsumerSecret, token.TokenSecret)
	return s
}
