#!/usr/bin/env python3

import argparse
import os
import pathlib
import subprocess
import sys

import yaml
from launchpadlib.credentials import AuthorizeRequestTokenWithURL
from launchpadlib.launchpad import Launchpad


SNAPD_REPO = "https://github.com/canonical/snapd.git"
SNAPD_LP_REPO_PATH = "~snappy-dev/snapd/+git/snapd"
DEFAULT_PATH = "tests/lib/snaps/store"
DEFAULT_CHANNEL = "edge"
DEFAULT_STORE_SERIES = "16"
DEFAULT_BUILD_POCKET = "Updates"
RECIPE_OWNER = "snappy-dev"
LAUNCHPAD_URL = "https://launchpad.net"


def launchpad_login(credentials_file=None):
    """Log in to Launchpad using explicit or tool-managed credentials."""
    state_dir = pathlib.Path(
        os.environ.get(
            "SNAP_USER_COMMON",
            pathlib.Path.home() / ".local/share/snapd-test-snap-recipes",
        )
    )
    state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    if credentials_file is None:
        credentials_file = state_dir / "credentials"

    authorization_engine = AuthorizeRequestTokenWithURL(
        service_root="production",
        application_name="snapd-test-snap-recipes",
    )
    return Launchpad.login_with(
        service_root="production",
        version="devel",
        launchpadlib_dir=str(state_dir / "cache"),
        credentials_file=str(credentials_file),
        authorization_engine=authorization_engine,
    )


def read_snap_name(snapcraft_yaml):
    """Return the snap name declared in a snapcraft.yaml file."""
    with open(snapcraft_yaml, "r", encoding="utf-8") as f:
        data = yaml.safe_load(f)

    if not isinstance(data, dict) or "name" not in data:
        raise RuntimeError(f"{snapcraft_yaml}: no 'name:' found")

    return data["name"]


def current_branch(path):
    """Return the current Git branch, rejecting a detached HEAD."""
    branch = subprocess.run(
        ["git", "-C", str(path), "branch", "--show-current"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if not branch:
        raise RuntimeError("cannot determine current branch from detached HEAD")
    return branch


def git_ref_path(branch):
    """Convert a branch name to the ref path expected by Launchpad."""
    if branch.startswith("refs/") or branch == "HEAD":
        return branch
    return f"refs/heads/{branch}"


def get_git_ref(git_repository, branch):
    """Resolve a branch in a Launchpad Git repository."""
    ref_path = git_ref_path(branch)
    git_ref = git_repository.getRefByPath(path=ref_path)
    if git_ref is None:
        raise RuntimeError(
            f"Git branch {ref_path!r} was not found in "
            f"{SNAPD_LP_REPO_PATH}; push the branch before creating "
            "the recipe"
        )
    return git_ref


def get_existing_snap(lp, owner, name):
    """Return an owner's snap recipe, or None when it does not exist."""
    try:
        return lp.snaps.getByName(
            name=name,
            owner=owner,
        )
    except Exception as e:
        # Launchpad returns HTTP 404 for a non-existing snap.
        if "404" in str(e):
            return None
        raise


def check_create_permission(lp, owner):
    """Ensure the authenticated user may create recipes for owner.

    Launchpad permits creation when the registrant is the owner or a direct
    or indirect participant in the owner team.
    """
    registrant = lp.me
    if registrant is None:
        raise PermissionError("Launchpad authentication is required")

    if registrant.self_link == owner.self_link:
        return

    if any(
        participant.self_link == registrant.self_link
        for participant in owner.participants
    ):
        return

    raise PermissionError(
        f"Launchpad user ~{registrant.name} is not a member of "
        f"~{owner.name} and cannot create recipes owned by that team"
    )


def get_processors(lp, architectures):
    """Resolve architecture names to Launchpad processor resources."""
    if architectures is None:
        return None

    processors = []
    for architecture in architectures:
        processor = lp.processors.getByName(name=architecture)
        if processor is None:
            raise RuntimeError(f"unknown architecture: {architecture}")
        processors.append(processor)
    return processors


def parse_list(values, item_name):
    """Parse repeated, comma-separated options into a unique list."""
    if values is None:
        return None

    items = [
        item.strip()
        for value in values
        for item in value.split(",")
        if item.strip()
    ]
    if not items:
        raise ValueError(f"at least one {item_name} must be specified")
    return list(dict.fromkeys(items))


def create_snap(
    lp,
    owner,
    name,
    build_path,
    channels,
    store_series,
    git_ref,
    build_archive,
    processors=None,
):
    """Create and return a Launchpad snap recipe with automatic builds."""
    print(f"Creating {name}...")

    recipe = {
        "name": name,
        "owner": owner,
        "git_ref": git_ref,
        "build_path": build_path,
        "auto_build": True,
        "auto_build_archive": build_archive,
        "auto_build_pocket": DEFAULT_BUILD_POCKET,
        "store_upload": True,
        "store_series": store_series,
        "store_name": name,
        "store_channels": channels,
    }
    if processors is not None:
        recipe["processors"] = processors

    snap = lp.snaps.new(
        **recipe,
    )

    print(f"  created: {snap.web_link}")
    return snap


def update_snap(
    snap, build_path, branch, channels, processors=None, dry_run=False
):
    """Apply changed recipe settings and report whether any differ."""
    changed = False

    desired = {
        "git_path": git_ref_path(branch),
        "build_path": build_path,
        "auto_build": True,
        "store_upload": True,
        "store_name": snap.name,
        "store_channels": channels,
    }
    if processors is not None:
        desired["processors"] = processors

    # git_repository_url is read-only after creation, so we don't
    # attempt to modify it here.

    for key, value in desired.items():
        try:
            current = getattr(snap, key)
        except AttributeError:
            continue

        if current != value:
            prefix = "WOULD UPDATE " if dry_run else ""
            print(f"  {prefix}{key}: {current!r} -> {value!r}")
            if not dry_run:
                setattr(snap, key, value)
            changed = True

    if changed and not dry_run:
        snap.lp_save()

    return changed


def request_builds(snap, build_archive, dry_run=False):
    """Request recipe builds from the archive, or describe a dry run."""
    if dry_run:
        print(f"  WOULD REQUEST BUILDS from {build_archive.web_link}")
        return None

    print("  Requesting builds...")
    request = snap.requestBuilds(
        archive=build_archive,
        pocket=DEFAULT_BUILD_POCKET,
    )
    print(f"  requested: {request.web_link}")
    return request


def main():
    """Parse command-line options and create, update, or build a recipe."""
    parser = argparse.ArgumentParser(
        description=(
            "Create/update Launchpad Snap recipes for snapd test snaps."
        )
    )

    parser.add_argument(
        "--root-dir",
        "--root",
        dest="root_dir",
        default=DEFAULT_PATH,
        help="Directory containing the test snap directories",
    )

    parser.add_argument(
        "--snap",
        required=True,
        help="Snap directory to process under --root-dir",
    )

    parser.add_argument(
        "--branch",
        help="Git branch in canonical/snapd (default: current branch)",
    )

    parser.add_argument(
        "--channel",
        "--channels",
        action="append",
        dest="channels",
        help=(
            "Snap Store channels to release to, comma-separated or repeated "
            f"(default: {DEFAULT_CHANNEL})"
        ),
    )

    parser.add_argument(
        "--store-series",
        default=DEFAULT_STORE_SERIES,
        help=f"Snap Store series (default: {DEFAULT_STORE_SERIES})",
    )

    parser.add_argument(
        "--architecture",
        "--architectures",
        action="append",
        dest="architectures",
        help=(
            "Target architectures to build, comma-separated or repeated "
            "(default: Launchpad recipe defaults)"
        ),
    )

    existing_recipe_group = parser.add_mutually_exclusive_group()

    existing_recipe_group.add_argument(
        "--ensure",
        action="store_true",
        help="Create missing recipes and reuse existing recipes unchanged",
    )

    existing_recipe_group.add_argument(
        "--update-existing",
        action="store_true",
        help="Update existing Launchpad recipes",
    )

    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Only show what would be done",
    )

    parser.add_argument(
        "--request-build",
        action="store_true",
        help="Request builds after creating, ensuring, or updating the recipe",
    )

    parser.add_argument(
        "--credentials-file",
        type=pathlib.Path,
        help="Load Launchpad credentials from this file",
    )

    args = parser.parse_args()

    try:
        channels = parse_list(args.channels, "channel") or [DEFAULT_CHANNEL]
        architectures = parse_list(args.architectures, "architecture")
    except ValueError as e:
        parser.error(str(e))

    credentials_file = args.credentials_file
    if credentials_file is not None:
        credentials_file = credentials_file.expanduser()
        if not credentials_file.is_file():
            print(
                f"ERROR: credentials file does not exist: {credentials_file}",
                file=sys.stderr,
            )
            return 1

    root_dir = pathlib.Path(args.root_dir)

    if not root_dir.is_dir():
        print(
            f"ERROR: directory does not exist: {root_dir}", file=sys.stderr
        )
        return 1

    try:
        branch = args.branch or current_branch(root_dir)
    except (RuntimeError, subprocess.CalledProcessError) as e:
        print(f"ERROR: {e}", file=sys.stderr)
        return 1

    snap_dir = root_dir / args.snap
    snapcraft_yaml = snap_dir / "snapcraft.yaml"
    if not snapcraft_yaml.is_file():
        print(f"ERROR: snap not found: {snapcraft_yaml}", file=sys.stderr)
        return 1

    try:
        name = read_snap_name(snapcraft_yaml)
    except RuntimeError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        return 1

    relative_path = snap_dir.as_posix()

    print("Logging into Launchpad...")

    lp = launchpad_login(credentials_file)

    owner = lp.people[RECIPE_OWNER]

    print(f"Owner: {owner.web_link}")
    print(f"Repository: {SNAPD_REPO}")
    print(f"Branch: {branch}")
    print(f"Channels: {', '.join(channels)}")
    if architectures is not None:
        print(f"Architectures: {', '.join(architectures)}")
    print()

    print(f"[{name}]")
    print(f"  source: {relative_path}")

    try:
        processors = get_processors(lp, architectures)
        store_series = lp.snappy_serieses.getByName(name=args.store_series)
        if store_series is None:
            raise RuntimeError(f"unknown store series: {args.store_series}")
        git_repository = lp.git_repositories.getByPath(
            path=SNAPD_LP_REPO_PATH
        )
        if git_repository is None:
            raise RuntimeError(
                f"Launchpad Git repository not found: {SNAPD_LP_REPO_PATH}"
            )
        git_ref = get_git_ref(git_repository, branch)
        build_archive = lp.distributions["ubuntu"].main_archive
        snap = get_existing_snap(lp, owner, name)

        if snap is None:
            check_create_permission(lp, owner)
            if args.dry_run:
                print(
                    f"  WOULD CREATE "
                    f"{LAUNCHPAD_URL}/~{RECIPE_OWNER}/+snap/{name}"
                )
                if args.request_build:
                    request_builds(None, build_archive, dry_run=True)
                return 0

            snap = create_snap(
                lp=lp,
                owner=owner,
                name=name,
                build_path=relative_path,
                channels=channels,
                store_series=store_series,
                git_ref=git_ref,
                build_archive=build_archive,
                processors=processors,
            )
        else:
            print(f"  exists: {snap.web_link}")

            if args.update_existing:
                if update_snap(
                    snap,
                    relative_path,
                    branch,
                    channels,
                    processors,
                    args.dry_run,
                ):
                    print("  would update" if args.dry_run else "  updated")
                else:
                    print("  already configured")
            elif args.ensure:
                print("  using existing recipe")

        if args.request_build:
            request_builds(snap, build_archive, args.dry_run)
    except Exception as e:
        print(f"  ERROR: {e}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
