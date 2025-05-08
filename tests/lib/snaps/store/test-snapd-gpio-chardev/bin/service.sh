#!/bin/bash -exu

if [ ! -d "$SNAP_COMMON/gpiochips" ]; then
    echo "no chips found under $SNAP_COMMON/gpiochips, exiting..."
    exit 0
fi

for gpiochip_file in "$SNAP_COMMON"/gpiochips/*; do
    i=0
    gpiochip="$(basename "$gpiochip_file")"
    while read -r pin_value; do
        echo "setting gpiochip line: /dev/snap/gpio-chardev/$SNAP_INSTANCE_NAME/$gpiochip $i=$pin_value"
        gpioset -t0 --chip "/dev/snap/gpio-chardev/$SNAP_INSTANCE_NAME/$gpiochip" "$i"="$pin_value"
        i=$((i+1))
    done <"$gpiochip_file"
done
