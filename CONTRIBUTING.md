# Contributing to snapd

We are an open source project and welcome community contributions, suggestions,
fixes and constructive feedback.

If you'd like to contribute, you will first need to sign the Canonical
contributor agreement. This is the easiest way for you to give us permission to
use your contributions. In effect, you’re giving us a licence, but you still
own the copyright — so you retain the right to modify your code and use it in
other projects.

The agreement can be found, and signed, here:
https://ubuntu.com/legal/contributors

If you have any questions, please reach out to us on our forum:
https://forum.snapcraft.io/c/snapd/5

## Contributor guidelines

Contributors can help us by observing the following guidelines:

- Commit messages should be well structured.
- Commit emails should not include non-ASCII characters.
- Several smaller PRs are better than one large PR.
- Try not to mix potentially controversial and trivial changes together.
  (Proposing trivial changes separately makes landing them easier and
  makes reviewing controversial changes simpler)
- Do not [force push][git-force] a PR after it has received reviews. It is
  acceptable to force push when a PR is ready to merge, however.
- Try to write tests to cover the contributed changes (see below)

For further details on our coding conventions, including how to format a PR,
see [CODING.md](CODING.md).

## Pull requests and tests

Before merging any changes into the snapd codebase, we need to verify that the
proposed functionality and code quality does not degrade the functionality and
quality requirement we've set for the project.

For each PR, we run checks in three different groups: static, unit and spread.

Static tests use several code analysis tools present in the golang ecosystem
(go vet, go lint and go fmt) to make sure that the code always aligns with
the standards. They also check the markdown format of documentation files.

All the existing unit tests are also executed, and the coverage info is
reported to coveralls.

We use [spread](https://github.com/snapcore/spread) to verify the
integrity of the product, exercising it as a whole, both from an end user
standpoint (eg. all kinds of interactions with the snap tool from the command
line) and from a more systemic approach (testing upgrades, for instance).

Spread and unit tests are not strictly a requirement for a PR to be submitted,
but we do strongly encourage contributors to include them. We rarely merge code
without tests although we may occasionally write them ourselves on behalf of
a contributor.

Unit tests help us understand expected behaviour, verified through the tests
and review process, which ensures we're building on the solid base of a tested
and working system.

If any tests need to be added for a PR to be merged it will be denoted
during the review process.

See [Testing](HACKING.md#user-content-testing) for further details on running
tests.

## Pull request guidelines

Contributions are submitted through a [pull request][pull-request] created from
a [fork][fork] of the `snapd` repository (under your GitHub account).

GitHub's documentation outlines the [process][github-pr], but for a more
concise and informative version try [this GitHub gist][pr-gist].

### Linear git history

We strive to keep a [linear git history][linear-git]. This makes it easier to
inspect the history, keep related commits next to each other, and make tools
like [git bisect][git-bisect] work intuitively.

### Labels

We add [GitHub labels][github-labels] to a PR for both organisational purposes
and to alter specific CI behaviour. Only project maintainers can add labels.

The following labels are commonly used:

- `Simple 🙂`: informs potential reviewers the PR can be reviewed quickly.
- `Test robustness`: either fixes tests, adds tests, or otherwise improves our
  test suite.
- `Documentation`: is used to denote a PR that requires typically small
  documentation changes, either internally (to this repository) or externally.
- `Needs documentation`: not to be confused with the above. This label needs to
  be added when a PR introduces new features which need to be documented for
  our users, or if the PR changes the behaviour of already documented
  features (though this should almost never happen).
  * Our user-facing documentation can be found here: https://snapcraft.io/docs
  * The PR description must explain any required documentation changes.
  * For internal documentation in this repository, it's expected that
    documentation changes are delivered in the same branch.
    Please don't abuse this tag.
- `Needs Samuele review`: Samuele (@pedronis) is our architect, and this label
  will summon his attention. Do not use it unless you want @pedronis to review
  your branch. If making big or deep changes, then ping Samuele in advance. The
  tag will then be added if necessary. When requesting a quick high-level green
  light about a chosen approach use a [draft PR][github-draft] to avoid the risk
  of other reviewers wasting time on something that has not been agreed upon.
- `Needs security review`: similar to above, but with a security focus. If your
  changes touch code in snap-confine or code related to AppArmor, Seccomp,
  Cgroup management, then someone from the security team will be alerted and
  will review your code.
- `Run nested`: instructs our CI system to run our container-based
  [nested tests][nested-tests]. These tests are usually skipped to save time,
  but they're useful to test a PR  against certain operating system traits that
  might otherwise be missed.
- `Skip spread`: instructs our CI system to not run any spread tests. Only unit
  tests will be executed. Use this when a PR only changes code in the unit tests.
  Do not use this flag if any production code changes.

### Pull request updates

Feel free to [rebase][github-rebase], rework commits, and [force
push][git-force] to your branch while a PR is waiting for its first review.

However, if you are still making significant changes during this waiting
phase, it's a good idea to keep the PR as a [draft][github-draft]. This stops
reviewers from looking at code you may not be confident about. Set the PR as
"Ready for review" when you do feel confident.

During the review process, reviewers will point out defects or suggest
alternative implementations.

After the first review, please treat your already pushed commits as immutable
and submit any requested changes as additional commits. This helps reviewers to
see exactly what has changed since the last review without requesting them to
review all the changes.

Two approvals are required for a PR to be merged. A PR can then be merged into the main branch.

After approval, you can rework the branch history as you see fit. Consider
squashing commits from the original PR with those made during the review
process, for example. Commit messages should follow the format described in
[CODING.md](CODING.md). A [force push][git-force] will be required if you
rework the history.

Start a [rebase][github-rebase] from the original parent commit of your first
commit. Ensure you do not rebase on top of the current main as this means
changes from the _main_ branch will be shown in the GitHub UI as part of your
changes, making the verification more confusing.

Merge using Github's [Squash and Merge][github-squash-merge] or [Rebase and merge][github-rebase-merge],
never [Create a merge commit][github-merge-commit].
* [Squash and Merge][github-squash-merge] is preferred because it simplifies cherry-picking of PR
content.
  * Also for single commits
  * This merge will use the title as commit message so double check that it is accurate and concise
* [Rebase and merge][github-rebase-merge] is required when it is important to be able to distinguish
different parts of a solution in the future.
  * Keep commits to a minimum
  * Squash uninteresting commits such as review improvements after review approval

[1]: http://www.ubuntu.com/legal/contributors
[pull-request]: https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/creating-a-pull-request-from-a-fork
[fork]: https://docs.github.com/en/get-started/quickstart/fork-a-repo#forking-a-repository
[github-pr]: https://docs.github.com/en/github/collaborating-with-pull-requests
[pr-gist]: https://gist.github.com/Chaser324/ce0505fbed06b947d962
[linear-git]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/defining-the-mergeability-of-pull-requests/about-protected-branches#require-linear-history
[git-bisect]: https://git-scm.com/docs/git-bisect
[github-draft]: https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/about-pull-requests#draft-pull-requests
[github-labels]: https://docs.github.com/en/issues/using-labels-and-milestones-to-track-work/managing-labels
[nested-tests]: https://github.com/snapcore/snapd/tree/master/tests/nested
[github-rebase]: https://docs.github.com/en/get-started/using-git/about-git-rebase
[git-force]: https://git-scm.com/docs/git-push#Documentation/git-push.txt---force
[github-rebase-merge]: https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/incorporating-changes-from-a-pull-request/about-pull-request-merges#rebase-and-merge-your-commits
[github-squash-merge]: https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/incorporating-changes-from-a-pull-request/about-pull-request-merges#squash-and-merge-your-commits
[github-merge-commit]: https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/incorporating-changes-from-a-pull-request/about-pull-request-merges#merge-your-commits
