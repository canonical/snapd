package asserts

import (
	"fmt"
	"net/url"
)

// EnterpriseStore holds an enterprise-store assertion, defining the
// configuration needed to connect a device to the enterprise store.
type EnterpriseStore struct {
	assertionBase
	address *url.URL
}

// OperatorID returns the account id of the enterprise store's operator.
func (estore *EnterpriseStore) OperatorID() string {
	return estore.HeaderString("operator-id")
}

// Store returns the identifying name of the operator's enterprise store.
func (estore *EnterpriseStore) Store() string {
	return estore.HeaderString("store")
}

// Address returns the URL of the enterprise store's API.
func (estore *EnterpriseStore) Address() *url.URL {
	return estore.address
}

func (estore *EnterpriseStore) checkConsistency(db RODatabase, acck *AccountKey) error {
	// Will be applied to a system's snapd so must be signed by a trusted authority.
	if !db.IsTrustedAccount(estore.AuthorityID()) {
		return fmt.Errorf("enterprise-store assertion for operator-id %q and store %q is not signed by a directly trusted authority: %s",
			estore.OperatorID(), estore.Store(), estore.AuthorityID())
	}

	_, err := db.Find(AccountType, map[string]string{"account-id": estore.OperatorID()})
	if err != nil {
		if err == ErrNotFound {
			return fmt.Errorf(
				"enterprise-store assertion for operator-id %q and store %q does not have a matching account assertion for the operator %q",
				estore.OperatorID(), estore.Store(), estore.OperatorID())
		}
		return err
	}

	return nil
}

// Prerequisites returns references to this enterprise-store's prerequisite
// assertions.
func (estore *EnterpriseStore) Prerequisites() []*Ref {
	return []*Ref{
		{AccountType, []string{estore.OperatorID()}},
	}
}

// checkAddressURL validates the input URL address and returns a full URL
func checkAddressURL(headers map[string]interface{}) (*url.URL, error) {
	address, err := checkNotEmptyString(headers, "address")
	if err != nil {
		return nil, err
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

func assembleEnterpriseStore(assert assertionBase) (Assertion, error) {
	address, err := checkAddressURL(assert.headers)
	if err != nil {
		return nil, err
	}

	return &EnterpriseStore{assertionBase: assert, address: address}, nil
}
