#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

go install; \
    rm -Rf build; \
    mkdir build; \
    ~/go/bin/paktxt pack -e README.md -o build.txt; \
    ~/go/bin/paktxt unpack -w build -i build.txt