import importlib.util
import pathlib
import unittest


SCRIPT = pathlib.Path(__file__).with_name("pr-review-cap.py")
SPEC = importlib.util.spec_from_file_location("pr_review_cap", SCRIPT)
pr_review_cap = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(pr_review_cap)


def review(author, state, submitted_at):
    return {
        "author": {"login": author},
        "state": state,
        "submittedAt": submitted_at,
    }


def pull_request(number, author, draft=False, reviews=None):
    return {
        "number": number,
        "author": {"login": author},
        "isDraft": draft,
        "reviews": reviews or [],
    }


class ReviewStateTests(unittest.TestCase):
    def test_two_distinct_approvals_are_approved(self):
        reviews = [
            review("alice", "APPROVED", "2026-01-01T00:00:00Z"),
            review("bob", "APPROVED", "2026-01-02T00:00:00Z"),
        ]
        self.assertTrue(pr_review_cap.is_approved(reviews))

    def test_comments_and_repeated_approval_do_not_add_approvals(self):
        reviews = [
            review("alice", "APPROVED", "2026-01-01T00:00:00Z"),
            review("alice", "COMMENTED", "2026-01-02T00:00:00Z"),
            review("alice", "APPROVED", "2026-01-03T00:00:00Z"),
        ]
        self.assertFalse(pr_review_cap.is_approved(reviews))

    def test_changes_requested_blocks_other_approvals(self):
        reviews = [
            review("alice", "APPROVED", "2026-01-01T00:00:00Z"),
            review("bob", "APPROVED", "2026-01-02T00:00:00Z"),
            review("carol", "CHANGES_REQUESTED", "2026-01-03T00:00:00Z"),
        ]
        self.assertFalse(pr_review_cap.is_approved(reviews))

    def test_later_approval_clears_reviewers_changes_request(self):
        reviews = [
            review("alice", "CHANGES_REQUESTED", "2026-01-01T00:00:00Z"),
            review("alice", "APPROVED", "2026-01-02T00:00:00Z"),
            review("bob", "APPROVED", "2026-01-03T00:00:00Z"),
        ]
        self.assertTrue(pr_review_cap.is_approved(reviews))

    def test_dismissed_approval_does_not_count(self):
        reviews = [
            review("alice", "APPROVED", "2026-01-01T00:00:00Z"),
            review("alice", "DISMISSED", "2026-01-02T00:00:00Z"),
            review("bob", "APPROVED", "2026-01-03T00:00:00Z"),
        ]
        self.assertFalse(pr_review_cap.is_approved(reviews))


class AdmissionTests(unittest.TestCase):
    def test_development_team_shares_limit(self):
        candidate = pull_request(3, "dev-c")
        prs = [
            pull_request(1, "dev-a"),
            pull_request(2, "dev-b"),
            candidate,
            pull_request(4, "external"),
        ]
        members = {"dev-a": True, "dev-b": True, "dev-c": True, "external": False}
        decision = pr_review_cap.decide_admission(candidate, prs, members, 2)
        self.assertFalse(decision.admitted)
        self.assertEqual(decision.outcome, "development_team_cap_reached")

    def test_approved_pr_does_not_consume_development_team_slot(self):
        approvals = [
            review("reviewer-a", "APPROVED", "2026-01-01T00:00:00Z"),
            review("reviewer-b", "APPROVED", "2026-01-02T00:00:00Z"),
        ]
        candidate = pull_request(2, "dev-b")
        prs = [pull_request(1, "dev-a", reviews=approvals), candidate]
        members = {"dev-a": True, "dev-b": True}
        decision = pr_review_cap.decide_admission(candidate, prs, members, 1)
        self.assertTrue(decision.admitted)

    def test_external_contributor_may_have_one_active_review(self):
        candidate = pull_request(2, "external")
        prs = [pull_request(1, "external"), candidate]
        members = {"external": False}
        decision = pr_review_cap.decide_admission(candidate, prs, members, 10)
        self.assertFalse(decision.admitted)
        self.assertEqual(decision.blocking_prs, (1,))

    def test_approved_external_pr_does_not_consume_slot(self):
        approvals = [
            review("reviewer-a", "APPROVED", "2026-01-01T00:00:00Z"),
            review("reviewer-b", "APPROVED", "2026-01-02T00:00:00Z"),
        ]
        candidate = pull_request(2, "external")
        prs = [pull_request(1, "external", reviews=approvals), candidate]
        members = {"external": False}
        decision = pr_review_cap.decide_admission(candidate, prs, members, 10)
        self.assertTrue(decision.admitted)

    def test_draft_pr_does_not_consume_slot(self):
        candidate = pull_request(2, "external")
        prs = [pull_request(1, "external", draft=True), candidate]
        members = {"external": False}
        decision = pr_review_cap.decide_admission(candidate, prs, members, 10)
        self.assertTrue(decision.admitted)

    def test_candidate_does_not_count_itself(self):
        candidate = pull_request(1, "dev")
        members = {"dev": True}
        decision = pr_review_cap.decide_admission(candidate, [candidate], members, 1)
        self.assertTrue(decision.admitted)

    def test_draft_candidate_is_not_an_active_review(self):
        candidate = pull_request(1, "external", draft=True)
        self.assertFalse(pr_review_cap.is_active_review(candidate))


class FakeGitHub:
    def __init__(self, responses):
        self.responses = responses

    def api_json_with_status(self, endpoint):
        return self.responses[endpoint]


class MembershipTests(unittest.TestCase):
    def test_active_member_of_either_team_is_development_team(self):
        github = FakeGitHub({
            "orgs/canonical/teams/snapd/memberships/alice": (404, {}),
            "orgs/canonical/teams/ubuntu-core-devs/memberships/alice": (
                200,
                {"state": "active"},
            ),
        })
        cap = pr_review_cap.ReviewCap(
            github, "canonical", ["snapd", "ubuntu-core-devs"], 10
        )
        self.assertTrue(cap.team_member("alice"))

    def test_absent_from_both_teams_is_external(self):
        github = FakeGitHub({
            "orgs/canonical/teams/snapd/memberships/alice": (404, {}),
            "orgs/canonical/teams/ubuntu-core-devs/memberships/alice": (404, {}),
        })
        cap = pr_review_cap.ReviewCap(
            github, "canonical", ["snapd", "ubuntu-core-devs"], 10
        )
        self.assertFalse(cap.team_member("alice"))

    def test_membership_failure_does_not_classify_as_external(self):
        github = FakeGitHub({
            "orgs/canonical/teams/snapd/memberships/alice": (403, {}),
        })
        cap = pr_review_cap.ReviewCap(
            github, "canonical", ["snapd", "ubuntu-core-devs"], 10
        )
        with self.assertRaises(pr_review_cap.GitHubError):
            cap.team_member("alice")


if __name__ == "__main__":
    unittest.main()
