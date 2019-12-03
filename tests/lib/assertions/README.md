# Generating model assertions

Signed model assertions for use in the spread tests can be generated using
`gendeveloper1model` tool. The assertion is signed by `developer1` key, which is
built-into the snapd binary used during the tests.

To build the tool:

```
$ export GO111MODULE=off
$ go install ./tests/lib/gendeveloper1model
```

Generating the assertions is done like this:

```
$ cd tests/lib/assertions
$ gendeveloper1model < developer1-pc-18.model.json > developer1-pc-18.model
```
