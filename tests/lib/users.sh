#!/bin/bash

set -e -x

# remove_user_with_group remove the given username from the system and also
# cleans the group
remove_user_with_group() {
    REMOVE_USER="$1"

    if [ -e /var/lib/extrausers/passwd ]; then
        userdel --extrausers --force ${REMOVE_USER} || true
    else
        userdel --force ${REMOVE_USER} || true
        # some systems do not set "USERGROUPS_ENAB yes" so we need to cleanup
        # the group manually. Use "-f" (force) when available, older versions
        # do not have it.
        if groupdel -h | grep force; then
            groupdel -f ${REMOVE_USER} || true
        else
            groupdel ${REMOVE_USER} || true
        fi
    fi
    # ensure the ${REMOVE_USER} user really got deleted    
    not getent passwd ${REMOVE_USER}
    not getent group ${REMOVE_USER}
}
