#!/usr/bin/env bash
# Copyright 2025 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
set -e

if [ "$#" -lt 2 ]; then
    echo "Usage: $0 <header_template_file> <file1.go> [file2.go ...]"
    exit 1
fi

header_file="$1"
shift

if [ ! -f "$header_file" ]; then
    echo "Error: Header file '$header_file' not found."
    exit 1
fi

header_content=$(cat "$header_file")

replace_header_in_file() {
    local file="$1"
    local tmp_file
    tmp_file=$(mktemp)

    awk '
        BEGIN { removing=1 }
        removing && /DO NOT EDIT/ { removing=0 }
        !removing { print }
    ' "$file" > "$tmp_file"

    {
        echo "$header_content"
        cat "$tmp_file"
    } > "$file"

    rm "$tmp_file"
    echo "Replaced header in: $file"
}

for file in "$@"; do
    replace_header_in_file "$file"
done