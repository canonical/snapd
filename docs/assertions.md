# Snapd assertions

A summary of the supported assertions of snapd.

## Account

Account holds an account assertion, which ties a name for an to its
identifier and provides the authority's confidence in the name's
validity.

## AccountKey

AccountKey holds an account-key assertion, asserting a public key
belonging to the account.

## Model

Model holds a model assertion, which is a statement by a brand
about the properties of a device model.

## Serial

Serial holds a serial assertion, which is a statement binding a
device identity with the device public key.

## SnapDeclaration

SnapDeclaration holds a snap-declaration assertion, declaring a
snap binding its identifying snap-id to a name, asserting its
publisher and its other properties.

## SnapBuild

SnapBuild holds a snap-build assertion, asserting the properties of a snap
at the time it was built by the developer.

## SnapRevision

SnapRevision holds a snap-revision assertion, which is a statement by the
store acknowledging the receipt of a build of a snap and labeling it with a
snap revision.

## System-user

SystemUser holds a system-user assertion which allows creating local
system users.

The system-user assertion has the following form:
```
type:           system-user
authority-id:   account-id   // Owner of the key, must be the brand
brand-id:       account-id   // Assertion will only work on models of this brand
email:          string       // Email of user
series:         list         // List of series which should accept this assertion
models:         list         // Models which should accept this assertion
name:           string       // Optional person’s name, for context and for gecos
username:       string       // Local system username for the user
password:       string       // Password for local system user, encoded and salted with
                                an algorithm hard to brute-force ($6$rounds=...$...)
ssh-keys:       list         // SSH keys to authorize for connection
since:          utc-datetime
until:          utc-datetime // Required!
revision:       integer

signature       authority-sig
```

## Validation

Validation holds a validation assertion, describing that a combination of
(snap-id, approved-snap-id, approved-revision) has been validated for
the series, meaning updating to that revision of approved-snap-id
has been approved by the owner of the gating snap with snap-id.
