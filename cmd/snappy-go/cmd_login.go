package main

import (
	"bufio"
	"fmt"
	"os"

	"code.google.com/p/go.crypto/ssh/terminal"

	"launchpad.net/snappy/snappy"
)

type cmdLogin struct {
	Positional struct {
		UserName string `positional-arg-name:"userid" description:"Username for the login"`
	} `positional-args:"yes" required:"yes"`
}

const shortLoginHelp = `Log into the store`

const longLoginHelp = `This command logs the given username into the store`

func init() {
	var cmdLoginData cmdLogin
	_, _ = parser.AddCommand("login",
		shortLoginHelp,
		longLoginHelp,
		&cmdLoginData)
}

func requestStoreTokenWith2faRetry(username, password, tokenName string) (*snappy.StoreToken, error) {
	// first try without otp
	token, err := snappy.RequestStoreToken(username, password, tokenName, "")

	// check if we need 2fa
	if err == snappy.ErrAuthenticationNeeds2fa {
		fmt.Print("2fa code: ")
		reader := bufio.NewReader(os.Stdin)
		// the browser shows it as well (and Sergio wants to see it ;)
		otp, _, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		return snappy.RequestStoreToken(username, password, tokenName, string(otp))
	}

	return token, err
}

func (x *cmdLogin) Execute(args []string) error {
	const tokenName = "snappy login token"

	username := x.Positional.UserName
	fmt.Print("Password: ")
	password, err := terminal.ReadPassword(0)
	fmt.Print("\n")
	if err != nil {
		return err
	}

	token, err := requestStoreTokenWith2faRetry(username, string(password), tokenName)
	if err != nil {
		return err
	}

	return snappy.WriteStoreToken(*token)
}
