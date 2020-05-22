#!/bin/sh
IMAGE_MOUNTPOINT=$1
mount --rbind /dev "$IMAGE_MOUNTPOINT/dev"
mount --rbind /proc "$IMAGE_MOUNTPOINT/proc"
mount --rbind /sys "$IMAGE_MOUNTPOINT/sys"

