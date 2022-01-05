#!/bin/bash
set -e

# debugging if anything fails is tricky as dh-golang eats up all output
# uncomment the lines below to get a useful trace if you have to touch
# this again (my advice is: DON'T)
#set -x
#logfile=/tmp/mkversions.log
#exec >> $logfile 2>&1
#echo "env: $(set)"
#echo "mkversion.sh run from: $0"
#echo "pwd: $(pwd)"

# we have two directories we need to care about:
# - our toplevel pkg builddir which is where "mkversion.sh" is located
#   and where "snap-confine" expects its cmd/VERSION file
# - the GO_GENERATE_BUILDDIR which may be the toplevel pkg dir. but
#   during "dpkg-buildpackage" it will become a different _build/ dir
#   that dh-golang creates and that only contains a subset of the
#   files of the toplevel buildir. 
PKG_BUILDDIR=$(dirname "$0")
GO_GENERATE_BUILDDIR="${GO_GENERATE_BUILDDIR:-$(pwd)}"

# run from "go generate" adjust path
if [ "$GOPACKAGE" = "snapdtool" ]; then
    GO_GENERATE_BUILDDIR="$(pwd)/.."
fi


show_help() {
    echo "mkversion.sh [OPTIONS]"
    echo ""
    echo "mkversion.sh detects and sets the version of snapd in both the source code and in a static info file included"
    echo ""
    echo "-o, --output-only                  Prevents writing or modifying the info file or version.go in the source tree"
    echo "-i, --ignore-debian-rules-changes  Only treats the git tree as dirty if the changes are not those that debian/rules files perform to support trusty with libseccomp"
    echo "-s, --set-version <version>        Disables the auto-detection and forces the version to be the specified version"
    echo ""
}

# parse options before the positional arguments
# this is adapted from https://stackoverflow.com/a/14203146/10102404
POSITIONAL_ARGS=()

OUTPUT_ONLY=false
IGNORE_DEBIAN_RULES_CHANGES=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -o|--output-only)
            OUTPUT_ONLY="true"
            shift # past argument
            ;;
        -i|--ignore-debian-rules-changes)
            IGNORE_DEBIAN_RULES_CHANGES=true
            shift # past argument
            ;;
        -s|--set-version)
            version_from_user="$2"
            shift # past argument
            shift # past value
            ;;
        -*)
            echo "Unknown option $1"
            show_help
            exit 1
            ;;
        *)
            POSITIONAL_ARGS+=("$1") # save positional arg
            shift # past argument
            ;;
    esac
done

set -- "${POSITIONAL_ARGS[@]}" # restore positional parameters

DIRTY=false

# Let's try to derive the version from git.
if command -v git >/dev/null; then
    # don't include --dirty here as we independently track whether the tree is
    # dirty and append that last, including it here will make dirty trees 
    # directly on top of tags show up with version_from_git as 2.46-dirty which
    # will not match 2.46 from the changelog and then result in a final version
    # like 2.46+git2.46.2.46 which is silly and unhelpful
    # tracking the dirty independently like this will produce instead 2.46-dirty
    # for a dirty tree on top of a tag, and 2.46+git83.g1671726-dirty for a 
    # commit not directly on top of a tag
    version_from_git="$(git describe --always | sed -e 's/-/+git/;y/-/./' )"

    # check if we are using a dirty tree
    if git describe --always --dirty | grep -q dirty; then
        # HACK: we call mkversion.sh from debian/rules which will mutate some of
        # the tree because we cannot build the same code using go modules for
        # both trusty and non-trusty (this is related to seccomp)
        # however this mutation should not be treated as a dirty tree, we should
        # essentially "ignore" those changes, so we need to compare the diff to
        # see if the diff is just those changes and if so then don't treat the
        # tree as being dirty

        if [ "$IGNORE_DEBIAN_RULES_CHANGES" = "true" ]; then
            # to do this, we attempt to apply the inverse patch of the debian 
            # changes and if after applying that patch we have a clean tree, then
            # we do not treat the tree as dirty
            pushd "$PKG_BUILDDIR" > /dev/null
            if git apply "$PKG_BUILDDIR/undo-mkversion-debian-rules-changes.patch" > /dev/null 2>&1; then
                git describe --always --dirty
                # the undoing patch was clean - check if we still have changes
                if git describe --always --dirty | grep -q dirty; then
                    # there are more changes, it is still dirty
                    DIRTY=true
                fi

                # re-do it to go back to the dirty changes and mark as clean
                git apply "$PKG_BUILDDIR/mkversion-debian-rules-changes.patch"
            else
                # patch failed to apply, just leave as dirty
                DIRTY=true
            fi
            popd > /dev/null
        else
            # if not in debian/rules mode then just always treat it as dirty
            DIRTY=true
        fi
    fi
fi

# at this point we maybe in _build/src/github etc where we have no
# debian/changelog (dh-golang only exports the sources here)
# switch to the real source dir for the changelog parsing
if command -v dpkg-parsechangelog >/dev/null; then
    version_from_changelog="$(cd "$PKG_BUILDDIR"; dpkg-parsechangelog --show-field Version)";
fi

# select version based on priority
if [ -n "$version_from_user" ]; then
    # version from user always wins
    v="$version_from_user"
    o="user"
elif [ -n "$version_from_git" ]; then
    v="$version_from_git"
    o="git"
elif [ -n "$version_from_changelog" ]; then
    v="$version_from_changelog"
    o="changelog"
else
    echo "Cannot generate version"
    exit 1
fi

# if we don't have a user provided version and if the version is not
# a release (i.e. the git tag does not match the debian changelog
# version) then we need to construct the version similar to how we do
# it in a packaging recipe. We take the debian version from the changelog
# and append the git revno and commit hash. A simpler approach would be
# to git tag all pre/rc releases.
if [ -z "$version_from_user" ] && [ "$version_from_git" != "" ] && \
       [ -n "$version_from_changelog" ] && [ "$version_from_git" != "$version_from_changelog" ]; then
    # if the changelog version has "git" in it and we also have a git version
    # directly, that is a bad changelog version, so fail, otherwise the below
    # code will produce a duplicated git info
    if echo "$version_from_changelog" | grep -q git; then
        echo "Cannot generate version, there is a version from git and the changelog has a git version"
        exit 1
    else
        revno=$(git describe --always --abbrev=7|cut -d- -f2)
        commit=$(git describe --always --abbrev=7|cut -d- -f3)
        v="${version_from_changelog}+git${revno}.${commit}"
        o="changelog+git"
    fi
fi

# append dirty at the end if we had a dirty tree
if [ "$DIRTY" = "true" ]; then
    v="$v-dirty"
fi

if [ "$OUTPUT_ONLY" = true ]; then
    echo "$v"
    exit 0
fi

echo "*** Setting version to '$v' from $o." >&2

cat <<EOF > "$GO_GENERATE_BUILDDIR/snapdtool/version_generated.go"
package snapdtool

// generated by mkversion.sh; do not edit

func init() {
	Version = "$v"
}
EOF

cat <<EOF > "$PKG_BUILDDIR/cmd/VERSION"
$v
EOF

cat <<EOF > "$PKG_BUILDDIR/data/info"
VERSION=$v
SNAPD_APPARMOR_REEXEC=0
EOF
