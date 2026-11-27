# Pull request review cap

The `pr-review-cap.yaml` workflow limits the number of pull requests awaiting
approval. Members of the `snapd` and `ubuntu-core-devs` teams share the limit
configured in `PR_REVIEW_DEVELOPMENT_TEAM_LIMIT`. Other contributors may have
one pull request awaiting approval at a time.

A pull request no longer consumes a slot after it has two effective approvals
from distinct reviewers and no outstanding changes request. The hourly
reconciliation updates labels after review-state changes and reports no
admission decisions, so it never returns an existing pull request to draft.

## Repository configuration

Configure these settings before enabling the workflow:

- Actions secret `PR_REVIEW_CAP_GITHUB_TOKEN`: a token with `read:org` access
  to the `canonical` organization and write access to pull requests, issues,
  and Actions in `canonical/snapd`. An organization-level secret restricted to
  this repository is preferred.
- Actions variable `PR_REVIEW_DEVELOPMENT_TEAM_LIMIT`: a positive integer.
- The workflow creates the `In review` and `Waiting for review slot` labels if
  they do not already exist.

The workflow loads the token only through the Actions secret and executes the
policy script from the trusted default branch. It must not check out or execute
code from a pull request branch.
