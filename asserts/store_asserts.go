package asserts

import (
	"fmt"
	"net/url"
)

// Store holds a store assertion, defining the configuration needed to connect
// a device to the store.
type Store struct {
	assertionBase
	address *url.URL
}

// Store returns the identifying name of the operator's store.
func (store *Store) Store() string {
	return store.HeaderString("store")
}

// OperatorID returns the account id of the store's operator.
func (store *Store) OperatorID() string {
	return store.HeaderString("operator-id")
}

// Address returns the URL of the store's API.
func (store *Store) Address() *url.URL {
	return store.address
}

// Location returns a summary of the store's location/purpose.
func (store *Store) Location() string {
	return store.HeaderString("location")
}

func (store *Store) checkConsistency(db RODatabase, acck *AccountKey) error {
	// Will be applied to a system's snapd so must be signed by a trusted authority.
	if !db.IsTrustedAccount(store.AuthorityID()) {
		return fmt.Errorf("store assertion %q is not signed by a directly trusted authority: %s",
			store.Store(), store.AuthorityID())
	}

	_, err := db.Find(AccountType, map[string]string{"account-id": store.OperatorID()})
	if err != nil {
		if err == ErrNotFound {
			return fmt.Errorf(
				"store assertion %q does not have a matching account assertion for the operator %q",
				store.Store(), store.OperatorID())
		}
		return err
	}

	return nil
}

// Prerequisites returns references to this store's prerequisite assertions.
func (store *Store) Prerequisites() []*Ref {
	return []*Ref{
		{AccountType, []string{store.OperatorID()}},
	}
}

// checkAddressURL validates the input URL address and returns a full URL.
func checkAddressURL(headers map[string]interface{}) (*url.URL, error) {
	address, err := checkOptionalString(headers, "address")
	if err != nil {
		return nil, err
	}

	if address == "" {
		return nil, nil
	}

	errWhat := `"address" header`

	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("%s must be a valid URL: %s", errWhat, address)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf(`%s scheme must be "https" or "http": %s`, errWhat, address)
	}
	if u.Host == "" {
		return nil, fmt.Errorf(`%s must have a host: %s`, errWhat, address)
	}
	if u.RawQuery != "" {
		return nil, fmt.Errorf(`%s must not have a query: %s`, errWhat, address)
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf(`%s must not have a fragment: %s`, errWhat, address)
	}

	return u, nil
}

func assembleStore(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmptyString(assert.headers, "operator-id")
	if err != nil {
		return nil, err
	}

	address, err := checkAddressURL(assert.headers)
	if err != nil {
		return nil, err
	}

	_, err = checkOptionalString(assert.headers, "location")
	if err != nil {
		return nil, err
	}

	return &Store{assertionBase: assert, address: address}, nil
}
