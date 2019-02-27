#!/bin/sh
filename="$1"
editor_history="$2"

echo "Editing $filename"

echo "$filename" >> "$editor_history"
