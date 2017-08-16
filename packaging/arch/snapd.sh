# Expand the $PATH to include /snaps/bin which is what snappy applications
# use
PATH=$PATH:/var/lib/snapd/snap/bin

if [ -z "$XDG_DATA_DIRS" ]; then
    XDG_DATA_DIRS=/usr/local/share/:/usr/share/:/var/lib/snapd/desktop
else
    XDG_DATA_DIRS="$XDG_DATA_DIRS":/var/lib/snapd/desktop
fi
export XDG_DATA_DIRS
