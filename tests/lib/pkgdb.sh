#!/bin/bash

. "$TESTSLIB/quiet.sh"

debian_name_package() {
    case "$1" in
        xdelta3|curl|python3-yaml)
            echo "$1"
            ;;
    esac
}

distro_name_package() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            debian_name_package "$1"
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_install_local_package() {
    allow_downgrades=false
    while [ -n "$1" ]; do
        case "$1" in
            --allow-downgrades)
                allow_downgrades=true
                shift
                ;;
            *)
                break
        esac
    done

    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
                # relying on dpkg as apt(-get) does not support installation from local files in trusty.
                dpkg -i --force-depends --auto-deconfigure --force-depends-version "$@"
                apt-get -f install -y
            else
                flags="-y"
                if [ "$allow_downgrades" = "true" ]; then
                    flags="$flags --allow-downgrades"
                fi
                apt install "$flags" "$@"
            fi
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_install_package() {
    for pkg in "$@" ; do
        package_name=$(distro_name_package "$pkg")
        # When we could not find a different package name for the distribution
        # we're running on we try the package name given as last attempt
        if [ -z "$package_name" ]; then
            package_name="$pkg"
        fi

        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                apt-get install -y "$package_name"
                ;;
            *)
                echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
                exit 1
                ;;
        esac
    done
}

distro_purge_package() {
    for pkg in "$@" ; do
        package_name=$(distro_name_package "$pkg")
        # When we could not find a different package name for the distribution
        # we're running on we try the package name given as last attempt
        if [ -z "$package_name" ]; then
            package_name="$pkg"
        fi

        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                quiet apt-get remove -y --purge -y "$package_name"
                ;;
            *)
                echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
                exit 1
                ;;
        esac
    done
}

distro_update_package_db() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            quiet apt-get update
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_clean_package_cache() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            quiet apt-get clean
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}
