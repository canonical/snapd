Before contributing you should sign [Canonical's contributor agreement][1],
it’s the easiest way for you to give us permission to use your contributions.

## Pull Requests and tests

We need to verify that the code functionality and quality is not degraded
by additions before merging any changes to snapd's codebase. For each PR
we run checks in three different groups: static, unit and spread.

Static test use several code analysis tools present in the GoLang ecosystem
(go vet, go lint and go fmt) to make sure that the code always aligns with
the standards. They also check the markdown format of documentation files.
All the existing unit tests are also executed, and the coverage info is
reported to coveralls. Regarding [spread](https://github.com/snapcore/spread),
we use it to verify the integrity of the product exercising it as a whole,
both from an end user standpoint (eg. all kind of interactions with the
snap tool from the command line) and from a more systemic approach (testing
upgrades, for instance).

We do not set as a requirement the addition of spread and unit tests for a PR
to be merged, but encourage the contributors to add them so that the expected
behaviour is explained and verified through the tests and the review process
can be made on the solid base of a working system after the addition of the
changes. If any tests need to be added for a PR to be merged it will be denoted
during the review process.

[1]: http://www.ubuntu.com/legal/contributors
