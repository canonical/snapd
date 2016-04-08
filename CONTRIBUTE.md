Before contributing you should sign [Canonical's contributor agreement](http://www.ubuntu.com/legal/contributors), it’s the easiest way for you to give us permission to use your contributions.

## Pull Request management

Before merging any pull request to snappy's code base we need to verify that the code functionality and quality is not degraded by that addition. In order to do that there's a set of checks that we run for each PR, some of them using external services (TravisCI and Coveralls) and others using our own CI infrastructure (integration tests and autopkgtests). The checks based on external services are run for all the pull request. We are using the [GitHub Pull Request Builder Plugin](https://github.com/jenkinsci/ghprb-plugin/blob/master/README.md) for easing the management of PRs in relation with our internal infrastructure.

Depending on the afiliation of the GitHub user submitting a PR, the following actions may happen after receiving it:

* If the user belongs to the `ubuntu-core` organization or has been previously whitelisted, the internal downstream verification jobs will be triggered, their progress is reported in the PR's status section. Any of the users of the organization can retrigger the execution by posting a `retest this please` comment in the PR.
* For user's outside the `ubuntu-core` organization, the internal checks won't be triggered by default and the [snappy-m-o](https://github.com/snappy-m-o) user, managed by the ghrbp plugin, will post a comment "Can one of the admins verify this patch?". After this, an `ubuntu-core` admin can post one of these comments:
  * `add to whitelist`: the external user will be whitelisted and all the further PRs will be tested automatically.
  * `ok to test`: the tests will be triggered and all the subsequent commits will retrigger them only for this PR.
  * `test this please`: the tests will be triggered once.

Once the PR has been reviewed and is accepted by at least two `ubuntu-core` members it can be merged by admins with the `merge this please` comment. This command triggers the internal checks over the PR's branch merged with master, so that we make sure that after merging the tests keep passing. After a successful run the branch is merged.
