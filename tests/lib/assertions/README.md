# Generating model assertions

## Test keys and developer accounts

Signed model assertions for use in the spread tests can be generated using
`gendeveloper1` tool. The assertion is signed by `developer1` key, which is
built-into the snapd binary used during the tests. h

To build the tool:

```
$ go install ./tests/lib/gendeveloper1
```

Generating the assertions is done like this:

```
$ cd tests/lib/assertions
$ gendeveloper1 sign-model < developer1-pc-18.model.json > developer1-pc-18.model
```

The GPG of developer1 can be obtained with:

```
$ gendeveloper1 show-key
```

## Valid keys and developer accounts

Some tests, particularly remodel, require valid model assertions that are signed
by developer account keys known to the store. For instance,
`valid-for-testing-*.model` files are such assertions. There is a corresponding
`*.json` file for each of the assertions. The models were signed by one of snapd
developers using their private keys.

When there is a need to regenerate the assertions and sign them again using a
different set of keys be sure to update both `authority-id` and `brand-id` in
each of the json files. Then follow the procedure outlined in the Ubuntu Core
docs https://ubuntu.com/core/docs/custom-images#heading--signing which boils
down to:

```
$ snap create-key my-models
$ snapcraft register-key
$ snap sign -k my-models < valid-for-testing-some-file.json > valid-for-testing-some-file.model
```

The value for `authority-id` and `brand-id`, is the same as the `developer-id`
which can be obtained by running:

```
$ snapcraft login
$ snapcraft whoami
email:        <your-email>@...
developer-id: <your-developer-id>
```
