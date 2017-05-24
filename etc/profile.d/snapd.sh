# set XDG_DATA_DIRS for snapd provisioned desktop files.
if [ "${XDG_DATA_DIRS#*snapd}" = "${XDG_DATA_DIRS}" ]; then
    XDG_DATA_DIRS="${XDG_DATA_DIRS:-/usr/local/share:/usr/share}:/var/lib/snapd/desktop"
fi

export XDG_DATA_DIRS
