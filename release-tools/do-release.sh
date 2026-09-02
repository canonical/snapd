#!/bin/bash -e

# TODO: programatically figure these out from the most recent tag as the prev
# version, and the new version as an input to this script most likely

PREV_VERSION=2.55.1
NEW_VERSION=2.55.2
MAJOR_VERSION=2.55

# the git branch
RELEASE_BRANCH="release/${MAJOR_VERSION}"
# the snap branch
BETA_RELEASE_BRANCH="beta/${MAJOR_VERSION}"

if [ "$(echo ${NEW_VERSION} | awk -F. '{print NF-1}')" = "1" ]; then
    IS_MINOR=0
else
    IS_MINOR=1
fi

# the release person's remote name for their fork of snapd on github that 
# branches can be pushed to
# TODO: probably need to prompt/be an input for this too
PERSONAL_FORK=ijohnson

# the launchpad bug which is used for SRU'ing the new version of snapd back to
# older releases of Ubuntu
# TODO: figure out the LP bug automatically
# it's either the same LP bug for the previous release if this is a new minor 
# version, or it's a new bug we have to file if this is a new major version
LP_SRU_BUG=1965808

#####################################################################
# Generate the changelog and packaging changes
#####################################################################

# checkout the release branch
git checkout "${RELEASE_BRANCH}"

# generate the changelog

~/git/snappy-dch/snappy-dch.py "${PREV_VERSION}" > "${NEW_VERSION}-changelog.txt"

# TODO: prompt the release czar to edit the changelog as necessary since it 
# usually needs some fiddling

# do the packaging changes
./release-tools/changelog.py "${NEW_VERSION}" "${LP_SRU_BUG}" "${NEW_VERSION}-changelog.txt"

# TODO: double check that the right files are changed with diffstat

# TODO: make a new branch and commit the packaging changes

# commit message is of the format:

echo "
release: ${NEW_VERSION}

Update changelogs with the ${NEW_VERSION} changes."

# TODO: open a PR with the changelog available

# TODO: wait for that PR to be merged

#####################################################################
# Tag the new release and push it to github
#####################################################################


# now that it's merged we can tag the release and push the tag
git tag -s "${NEW_VERSION}"

# use this as the message:
echo "tagging package snapd version ${NEW_VERSION}"

# push the tag to origin
git push origin "${NEW_VERSION}"

#####################################################################
# Trigger launchpad things for the git branch
#####################################################################


if [ "$IS_MINOR" = "1" ]; then
    # TODO: trigger the code import for the LP release branch to trigger the 
    # snapd snap to start building using LP API
    true
else
    # TODO: create a new snap build recipe for the snapd snap using LP API
    # TODO: create a new snap build recipe for the core snap using LP API

    true
fi

#####################################################################
# Build the package and push to the snappy-dev/image PPA
#####################################################################


# install package dependencies so we can build the package
sudo apt build-dep -y ./

# build the package
gbp buildpackage -S --git-ignore-branch --git-no-purge --git-ignore-new

pushd ../build-area > /dev/null

# now push the source package
dput ppa:snappy-dev/image "snapd_${NEW_VERSION}_source.changes"

#####################################################################
# Generate the changelog and packaging changes
#####################################################################


# generate the vendor tarballs for the github release page
../snapd/release-tools/repack-debian-tarball.sh "./snapd_${NEW_VERSION}.tar.xz"

# TODO: create the github release page and attach the vendor tarballs to the 
# release


#####################################################################
# Build the focal/jammy versions of snapd debian package and push to the PPA
#####################################################################


(
    # build the focal version of the xenial package
    pushd "snapd-${NEW_VERSION}" > /dev/null

    # change the packaging for focal to build the xenial source pkg
    sed -i '0,/xenial/s//focal/' ./packaging/ubuntu-16.04/changelog
    sed -i "0,/${NEW_VERSION}/s//${NEW_VERSION}+20.04/" ./packaging/ubuntu-16.04/changelog
    dpkg-buildpackage -S

    popd > /dev/null

    # now push the focal source package
    dput ppa:snappy-dev/image "snapd_${NEW_VERSION}+20.04_source.changes"
)

(
    # build the jammy version of the xenial package
    pushd "snapd-$NEW_VERSION" > /dev/null

    # change the packaging for jammy to build the xenial source pkg
    sed -i '0,/focal/s//jammy/' ./packaging/ubuntu-16.04/changelog
    sed -i "0,/${NEW_VERSION}+20.04/s//${NEW_VERSION}+22.04/" ./packaging/ubuntu-16.04/changelog
    dpkg-buildpackage -S

    popd > /dev/null

    # now push the jammy source package
    dput ppa:snappy-dev/image "snapd_${NEW_VERSION}+22.04_source.changes"
)

# go back to the original snapd git dir
popd > /dev/null


# TODO: wait for the launchpad deb builds of snapd for focal and xenial to be
# successful, notify / retry as necessary if they fail

# TODO: trigger the beta core snap build after the deb builds are successful

#####################################################################
# Create PR merging changelog back to master to ensure edge builds of core snap
# have right version number
#####################################################################


# create a new branch merging the changelog from the release/ branch back to 
# master
git checkout master
git checkout -b release-${NEW_VERSION}-changelog
git merge ${RELEASE_BRANCH}

# TODO: double check that the right files are changed
git push release-${NEW_VERSION}-changelog ${PERSONAL_FORK}


# TODO: create a new PR to master with the release branch changelog changes

#####################################################################
# Wait for snap builds to finish and move them to beta channel proper
#####################################################################


# TODO: can we check on the core snap getting stuck in the review queue somehow?

for snap_name in snapd core; do
    while true; do
        missing=0
        for snap_arch in armhf arm64 amd64 s390x ppc64el; do
            if [ "$(snapcraft status ${snap_name} --arch ${snap_arch} | grep ${BETA_RELEASE_BRANCH} | awk '{print $2}' | sort | uniq)" != "${NEW_VERSION}" ]; then
                missing=1
                break
            fi
        done

        if [ "$missing" = "0" ]; then
            # got them all
            # TODO: snapcraft promot no longer works for the snapd snap 
            # since there is a riscv64 architecture snap published for 
            # snapd, but we are not yet actively building the riscv64 
            # architecture for releases (it's a hand built thing ATM), so 
            # instead we need to loop over and release each revision 
            # individually
            if [ "${snap_name}" = "snapd" ]; then
                for snap_arch in armhf arm64 amd64 s390x ppc64el; do
                    rev="$(snapcraft status ${snap_name} --arch ${snap_arch} | grep ${BETA_RELEASE_BRANCH} | awk '{print $3}')"
                    snapcraft release snapd "$rev" beta
                done
            else
                # the core snap does not have riscv64 so here we can use
                # promote
                # TODO: ask snapcraft team again about making snapcraft 
                # promote non-experimental and use the --yes option here
                snapcraft promote "${snap_name}" --from-channel="$BETA_RELEASE_BRANCH" --to-channel=beta
            fi
            
            break
        fi

        # otherwise wait to check again
        echo "waiting for ${snap_name} snap to be published to beta release branch for all architectures"
        sleep 60 
    done
done
