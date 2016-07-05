# snap-assert: A Low-level Tool for Assertion Signing

`snap-assert` is a low-level tool intended to be called by higher-level and more task specific tools when they need to create and sign specific assertions on behalf of the user:

`snap-assert [OPTIONS] [<assert-type>] [<statement>]`

## Keys and Signing

`snap-assert` will use and invoke for signing a local GnuPG setup with its available keys. The GnuPG setup used will be the default one for the user (`~/.gnupg/`) unless `--gpp-homedir` is specified or the `GNUPGHOME` envvar is set.

Keys for use need to be RSA and be at least 4096 bits long.

## Selecting Keys and Identifying the Signer

The signer identifier (the `authority-id` header value in the resulting assertion) can be specified with `--authority-id` while the key can be with `--key-id` giving a long key id. Otherwise the same can be achieved with`--account-key` to specify a file with the account key assertion for the intended key from which all the key and signer identifying information can be extracted.

## Input (aka Statement) for the Assertion

For clarity the type of assertion to create must be given as the first positional argument on the command line. After that optionally an input file called here the _statement_; if it is omitted or `-` is passed stdin will be used.

The remaining values for the headers and body of the assertion will come from the _statement_. It can be formatted either as YAML or JSON (YAML is the default, otherwise `--format yaml|json` helps selecting this).

For assertions without body the _statement_ can just be a flat mapping of header names to header values. For assertions with a body, _statement_ can have two top level entries:

* `headers` containing again a mapping from header names to values
* `body` with the assertion body text

In the end in the assertion header values will be text, but abstractely the value of some headers have specific simple types, in these cases it is possible and recommended to use those types (as supported by JSON and YAML) for the header values in _statement_, `snap-assert` will convert to string appropriately:

* integers (as for `revision`or `snap-revision`)
* bool values (turned into `yes` or `no`)
* null value (turned into empty)
* list of strings (turned into a string by joining with commas and line-wrapping as appropriate)

The assertion revision can be specified on the command line with `--revision` taking precedence or directly in _statement_. Same applies to `authority-id`.