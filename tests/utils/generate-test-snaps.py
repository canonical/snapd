#!/usr/bin/env python3
"""Generate an inventory of snaps referenced by the spread test suite."""

import argparse
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import re
import shlex
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import Request, urlopen


CLASSIFICATIONS = (
    "Defined in a test directory",
    "Defined in tests/lib/snaps",
    "Defined in tests/lib/snaps/store",
    "Not defined under tests",
)

MANIFEST_NAMES = {"snap.yaml", "snap.yaml.in", "snapcraft.yaml"}
SNAP_COMMANDS = "install|download|refresh|remove|revert|switch|info"
OPTIONS_WITH_VALUES = {
    "--basename",
    "--channel",
    "--cohort",
    "--hold",
    "--quota-group",
    "--revision",
    "--target-directory",
    "--transaction",
    "--validation-set",
}
SCRIPT_KEYS = {
    "debug",
    "debug-each",
    "execute",
    "prepare",
    "prepare-each",
    "restore",
    "restore-each",
}
SHELL_DELIMITERS = set("|&;<>()")
SHELL_KEYWORDS = {"do", "done", "else", "fi", "then"}
OWNER_CLASSIFICATIONS = {CLASSIFICATIONS[2], CLASSIFICATIONS[3]}
STORE_INFO_URL = "https://api.snapcraft.io/v2/snaps/info/{}"


def is_external(path):
    """Return whether path belongs to a vendored external test tree."""
    return "external" in path.parts


def is_snap_manifest(path):
    """Return whether path is a supported snap manifest filename."""
    return path.name in MANIFEST_NAMES or (
        path.name.startswith("snapcraft_") and path.suffix == ".yaml"
    )


def discover_definitions(tests_dir):
    """Map snap names to local manifest paths below tests_dir."""
    definitions = defaultdict(set)
    name_pattern = re.compile(r'^name:\s*["\']?([a-z0-9][a-z0-9.-]*)', re.MULTILINE)

    for path in tests_dir.rglob("*"):
        if not path.is_file() or is_external(path) or not is_snap_manifest(path):
            continue
        match = name_pattern.search(path.read_text(errors="ignore"))
        if match:
            definitions[match.group(1)].add(path)

    return definitions


def add_name(names, name):
    """Add name to names when it has valid snap-name syntax."""
    if re.fullmatch(r"[a-z0-9][a-z0-9.-]*", name):
        names.add(name)


def names_from_task_environments(tests_dir, names):
    """Add literal snap names declared by spread task environments to names."""
    snap_variant = re.compile(r"^\s*SNAP/[^:]+:\s*([a-z0-9][a-z0-9.-]*)\s*$", re.MULTILINE)
    snap_list = re.compile(r"^\s*SNAPS:\s*([^#\n]+)", re.MULTILINE)

    for path in tests_dir.rglob("task.yaml"):
        if is_external(path):
            continue
        content = path.read_text(errors="ignore")
        for match in snap_variant.finditer(content):
            add_name(names, match.group(1))
        for match in snap_list.finditer(content):
            for name in match.group(1).split():
                add_name(names, name)


def shell_lines(path):
    """Yield executable shell lines from a task definition or shell script."""
    content = path.read_text(errors="ignore")
    if path.name == "task.yaml":
        active = False
        key_pattern = re.compile(r"^([A-Za-z][\w-]*):(?:\s*[>|])?\s*$")
        for line in content.splitlines():
            match = key_pattern.match(line)
            if match:
                active = match.group(1) in SCRIPT_KEYS
                continue
            if active:
                yield line
    elif path.suffix == ".sh" or content.startswith(
        ("#!/bin/sh", "#!/bin/bash", "#!/usr/bin/env bash")
    ):
        yield from content.splitlines()


def names_from_shell_line(raw_line):
    """Extract literal snap arguments from supported snap commands in one line."""
    try:
        lexer = shlex.shlex(
            raw_line,
            posix=True,
            punctuation_chars="".join(sorted(SHELL_DELIMITERS)),
        )
        lexer.whitespace_split = True
        lexer.commenters = "#"
        tokens = list(lexer)
    except ValueError:
        return set()

    commands = SNAP_COMMANDS.split("|")
    names = set()
    for index, token in enumerate(tokens[:-1]):
        if token != "snap" or tokens[index + 1] not in commands:
            continue

        skip_next = False
        arguments = tokens[index + 2:]
        for argument_index, argument in enumerate(arguments):
            if argument and all(character in SHELL_DELIMITERS for character in argument):
                break
            if argument in SHELL_KEYWORDS:
                break
            if skip_next:
                skip_next = False
                continue

            argument = argument.strip("[]{};,")
            if argument in OPTIONS_WITH_VALUES:
                skip_next = True
                continue
            if (
                argument.isdigit()
                or argument.startswith("-")
                or "=" in argument
                or any(character in argument for character in "$/*\\")
                or argument.endswith(".snap")
            ):
                continue
            add_name(names, argument)

    return names


def names_from_snap_commands(tests_dir, names):
    """Add literal snap names used by shell commands below tests_dir to names."""

    for path in tests_dir.rglob("*"):
        if not path.is_file() or is_external(path) or path.suffix in {".assert", ".snap"}:
            continue
        try:
            lines = shell_lines(path)
        except OSError:
            continue

        for raw_line in lines:
            names.update(names_from_shell_line(raw_line))


def classify(name, definitions, repo_root):
    """Classify a snap by the highest-precedence location of its definition."""
    paths = {
        path.relative_to(repo_root).as_posix()
        for path in definitions.get(name, set())
    }
    if any(not path.startswith("tests/lib/snaps/") for path in paths):
        return CLASSIFICATIONS[0]
    if any(
        path.startswith("tests/lib/snaps/")
        and not path.startswith("tests/lib/snaps/store/")
        for path in paths
    ):
        return CLASSIFICATIONS[1]
    if any(path.startswith("tests/lib/snaps/store/") for path in paths):
        return CLASSIFICATIONS[2]
    return CLASSIFICATIONS[3]


def lookup_owner(name):
    """Fetch a snap's publisher from the Snap Store information endpoint."""
    request = Request(
        STORE_INFO_URL.format(quote(name, safe="")),
        headers={"Snap-Device-Series": "16"},
    )
    try:
        with urlopen(request, timeout=20) as response:
            payload = json.load(response)
    except HTTPError as error:
        if error.code == 404:
            return {"status": "not-found"}
        raise

    publisher = payload.get("snap", {}).get("publisher") or {}
    username = publisher.get("username")
    if not username:
        return {"status": "unknown"}
    return {
        "status": "found",
        "username": username,
        "display-name": publisher.get("display-name") or username,
    }


def load_owner_cache(path):
    """Load cached Snap Store owner records, or return an empty cache."""
    if not path.exists():
        return {}
    return json.loads(path.read_text())


def discover_owners(names, cache_path, refresh=False):
    """Resolve owners concurrently and persist newly fetched records.

    Cached records are reused unless refresh is true. The returned mapping is
    restricted to the requested names even if the cache contains other snaps.
    """
    owners = load_owner_cache(cache_path)
    pending = sorted(names if refresh else names - owners.keys())

    if pending:
        with ThreadPoolExecutor(max_workers=12) as executor:
            results = executor.map(lookup_owner, pending)
            owners.update(zip(pending, results))
        cache_path.parent.mkdir(parents=True, exist_ok=True)
        cache_path.write_text(json.dumps(owners, indent=2, sort_keys=True) + "\n")

    return {name: owners[name] for name in names}


def format_owner(owner):
    """Format a cached owner record for Markdown or TSV output."""
    if owner["status"] == "not-found":
        return "not found in Snap Store"
    if owner["status"] != "found":
        return "unknown"
    if owner["display-name"] == owner["username"]:
        return f"`{owner['username']}`"
    return f"{owner['display-name']} (`{owner['username']}`)"


def discover_usage(tests_dir, repo_root, names):
    """Map each snap name to test directories containing a static reference."""
    usage = {name: set() for name in names}
    task_roots = {
        path.parent
        for path in tests_dir.rglob("task.yaml")
        if not is_external(path)
    }
    owner_by_directory = {}
    for task_root in task_roots:
        owner_by_directory[task_root] = task_root

    # Longest-first alternation avoids matching a shorter snap name prefix.
    name_pattern = re.compile(
        r"(?<![a-z0-9.-])((?:"
        + "|".join(re.escape(name) for name in sorted(names, key=len, reverse=True))
        + r"))(?![a-z0-9.-])"
    )

    for path in tests_dir.rglob("*"):
        if not path.is_file() or is_external(path):
            continue
        owner = None
        current = path.parent
        while current != tests_dir.parent:
            if current in owner_by_directory:
                owner = current
                break
            current = current.parent
        if owner is None:
            continue
        try:
            content = path.read_text(errors="ignore")
        except OSError:
            continue
        for match in name_pattern.finditer(content):
            usage[match.group(1)].add(owner.relative_to(repo_root).as_posix())

    return usage


def discover_external_urls(store_dir):
    """Map snap directory names to URLs declared in their url files."""
    external_urls = {}
    for path in store_dir.glob("*/url"):
        url = path.read_text().strip()
        if url:
            external_urls[path.parent.name] = url
    return external_urls


def render(groups, usage, owners, external_urls):
    """Render classified snaps, usage references, and applicable owners as Markdown."""
    lines = [
        "# Test snaps",
        "",
        "Inventory of statically identifiable snaps used by the spread tests. "
        "Dynamic names discovered at runtime, such as arbitrary results from "
        "`snap find`, cannot be enumerated.",
        "",
        "The requested `tests/list/snaps` paths do not exist in this repository. "
        "The corresponding paths are `tests/lib/snaps` and `tests/lib/snaps/store`.",
        "",
        "A snap with definitions in more than one location is classified by this "
        "precedence: test directory, `tests/lib/snaps`, then `tests/lib/snaps/store`.",
        "",
        "`Used by` lists test directories containing a static reference to the snap "
        "name. This is source-based inventory rather than runtime tracing, so "
        "dynamically constructed names may be absent and generic names such as "
        "`core` or `snapd` may have many references.",
        "",
        "`Owner` is the Snap Store publisher returned by the store API. It is listed "
        "only for store fixtures and external snaps; locally packed snaps do not "
        "need an owner. Results are cached in `tests/utils/test-snap-owners.json`.",
        "",
        "`External url` is read from a snap directory's `url` file under "
        "`tests/lib/snaps/store`.",
        "",
        "## Summary",
        "",
        "| Classification | Count |",
        "|---|---:|",
    ]
    for classification in CLASSIFICATIONS:
        lines.append(f"| {classification} | {len(groups[classification])} |")
    lines.append(f"| **Total** | **{sum(len(group) for group in groups.values())}** |")

    for classification in CLASSIFICATIONS:
        lines.extend(("", f"## {classification}", ""))
        for name in groups[classification]:
            references = sorted(usage[name])
            used_by = (
                ", ".join(f"`{reference}`" for reference in references)
                if references
                else "no static test reference found"
            )
            lines.extend((f"- `{name}`", f"  - Used by: {used_by}"))
            if classification in OWNER_CLASSIFICATIONS:
                lines.append(f"  - Owner: {format_owner(owners[name])}")
            if name in external_urls:
                lines.append(f"  - External url: {external_urls[name]}")
            lines.append("")

    return "\n".join(lines).rstrip() + "\n"


def discover_inventory(repo_root):
    """Discover and classify snap names and their spread test usage."""
    tests_dir = repo_root / "tests"
    definitions = discover_definitions(tests_dir)
    names = set(definitions)
    names_from_task_environments(tests_dir, names)
    names_from_snap_commands(tests_dir, names)

    groups = {classification: [] for classification in CLASSIFICATIONS}
    for name in sorted(names):
        groups[classify(name, definitions, repo_root)].append(name)

    usage = discover_usage(tests_dir, repo_root, names)
    return groups, usage


def generate(repo_root, owner_cache, refresh_owners=False):
    """Generate the complete Markdown inventory, resolving required owners."""
    groups, usage = discover_inventory(repo_root)
    external_urls = discover_external_urls(repo_root / "tests/lib/snaps/store")
    owner_names = {
        name
        for classification in OWNER_CLASSIFICATIONS
        for name in groups[classification]
    }
    owners = discover_owners(owner_names, owner_cache, refresh=refresh_owners)
    return render(groups, usage, owners, external_urls)


def main():
    """Parse command-line options and write the requested inventory output."""
    repo_root = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(description="Generate the spread test snap inventory")
    parser.add_argument(
        "--output",
        type=Path,
        default=repo_root / "test-snaps.md",
        help="output Markdown file (default: test-snaps.md in the repository root)",
    )
    parser.add_argument(
        "--external-only",
        action="store_true",
        help="print external snap names and owners as TSV; do not write Markdown",
    )
    parser.add_argument(
        "--owner-cache",
        type=Path,
        default=Path(__file__).with_name("test-snap-owners.json"),
        help="Snap Store owner cache",
    )
    parser.add_argument(
        "--refresh-owners",
        action="store_true",
        help="refresh all Snap Store owners instead of using cached values",
    )
    args = parser.parse_args()
    if args.external_only:
        groups, _ = discover_inventory(repo_root)
        names = set(groups[CLASSIFICATIONS[3]])
        owners = discover_owners(names, args.owner_cache, refresh=args.refresh_owners)
        for name in sorted(names):
            print(f"{name}\t{format_owner(owners[name])}")
        return
    args.output.write_text(
        generate(repo_root, args.owner_cache, refresh_owners=args.refresh_owners)
    )


if __name__ == "__main__":
    main()