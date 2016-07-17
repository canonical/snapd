# snap sign: A Low-level Tool for Assertion Signing

`snap sign` is a low-level tool intended to be called by higher-level and more task specific tools when they need to create and sign specific assertions on behalf of the user:

`snap sign [OPTIONS]`

## Keys and Signing

`snap sign` will use and invoke for signing a local GnuPG setup with its available keys. The GnuPG setup used will be the default one for the user (`~/.gnupg/`) unless `--gpp-homedir` is specified or the `GNUPGHOME` envvar is set.

Keys for use need to be RSA and be at least 4096 bits long.

## Selecting Keys

`--key-id` can be used to select the GnuPG key by long key id. Otherwise the same can be achieved with`--account-key` to specify a file with the `account-key` assertion for the intended key from which all the key and signer identifying information can be extracted.

## Input for the Assertion

The input for the assertion is taken through stdin.

It is expected to be a YAML mapping from names to the string values for the headers. For assertions with a body this can be specified with a "body" pseudo-header.
