#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

go install; \
    rm -Rf paktxt-test; \
    mkdir paktxt-test; \
    ~/go/bin/paktxt pack -e README.md -o paktxt-test.txt; \
    ~/go/bin/paktxt unpack -w paktxt-test -i paktxt-test.txt