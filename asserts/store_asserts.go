package asserts

import "fmt"

// EnterpriseStore holds an enterprise-store assertion, defining the
// configuration needed to connect a device to the enterprise store.
type EnterpriseStore struct {
	assertionBase
}

// OperatorID returns the account id of the enterprise store's operator.
func (estore *EnterpriseStore) OperatorID() string {
	return estore.HeaderString("operator-id")
}

// Store returns the name of the operator's enterprise store.
func (estore *EnterpriseStore) Store() string {
	return estore.HeaderString("store")
}

// Address returns the address of the enterprise store's API.
func (estore *EnterpriseStore) Address() string {
	return estore.HeaderString("address")
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

func assembleEnterpriseStore(assert assertionBase) (Assertion, error) {
	// TODO:
	// - check address looks sane?
	// - convert address to full URL?
	_, err := checkNotEmptyString(assert.headers, "address")
	if err != nil {
		return nil, err
	}

	return &EnterpriseStore{assertionBase: assert}, nil
}
