# Build Test Snaps on Launchpad

`build-snap.py` creates and updates Launchpad recipes for snaps in
`tests/lib/snaps/store`. Use it when a test snap in that directory needs to be
updated in the Snap Store after changing its source or `snapcraft.yaml`.

Recipes are owned by the [`snappy-dev`](https://launchpad.net/~snappy-dev)
Launchpad team, build from the snapd repository, and publish to the `edge`
channel by default.

## Requirements

You need:

- Python 3.
- A Launchpad account that is a direct or indirect member of the
  [`snappy-dev`](https://launchpad.net/~snappy-dev) team. The script exits with
  an error if the authenticated account cannot create recipes owned by this
  team.
- A Git branch that exists in the Launchpad snapd repository. A local-only
  branch cannot be built; push it and wait for Launchpad to import it, or use
  an existing branch such as `master`.
- The `launchpadlib` and `PyYAML` Python packages.

On Ubuntu, install the packaged dependencies with:

```bash
sudo apt install python3-launchpadlib python3-yaml
```

Alternatively, use a virtual environment:

```bash
python3 -m venv .venv
source .venv/bin/activate
python3 -m pip install launchpadlib PyYAML
```

The first run opens a Launchpad authorization URL. Credentials are cached under
`$SNAP_USER_COMMON`, when set, or
`~/.local/share/snapd-test-snap-recipes`.

To reuse an existing credentials file created by `launchpadlib`, pass it with
`--credentials-file`:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --credentials-file ~/.config/launchpad/credentials
```

The supplied file must already exist. It contains authentication credentials,
so do not commit it and restrict access to your user, for example with
`chmod 600 ~/.config/launchpad/credentials`.

## Basic Usage

Run the script from the snapd repository root. `--snap` is the directory name
under `tests/lib/snaps/store`:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master
```

If `--branch` is omitted, the current local branch name is used. That branch
must also exist in `~snappy-dev/snapd/+git/snapd`; otherwise the script asks you
to push it before creating or updating the recipe.

Use `--dry-run` to inspect the intended operation without changing Launchpad:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --dry-run
```

## Updating a Test Snap

After modifying a snap under `tests/lib/snaps/store`, push the branch, update
the existing recipe, and request fresh builds:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --update-existing \
    --request-build
```

Use `--ensure` when the recipe should be created if missing but left unchanged
if it already exists:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --ensure
```

## Architectures

Without `--architecture`, Launchpad uses the recipe defaults. To build only one
architecture:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --architecture amd64 \
    --update-existing \
    --request-build
```

Multiple architectures can be comma-separated:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --architectures amd64,arm64 \
    --update-existing \
    --request-build
```

They can also be repeated:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --architecture amd64 \
    --architecture arm64 \
    --architecture armhf \
    --update-existing \
    --request-build
```

## Store Channels

The default Store channel is `edge`. Supply one or more channels with
`--channel`; as with architectures, values may be comma-separated or repeated:

```bash
python3 tests/utils/snaps-build/build-snap.py \
    --snap test-snapd-curl \
    --branch master \
    --channel edge,beta \
    --update-existing \
    --request-build
```

Run `python3 tests/utils/snaps-build/build-snap.py --help` for all options.
