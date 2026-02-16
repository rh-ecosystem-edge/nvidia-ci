#!/usr/bin/env bash

set -e

GINKGO_PACKAGE='github.com/onsi/ginkgo/v2'

GOPATH="${GOPATH:-/root/go}"
export PATH=$PATH:$GOPATH/bin

mkdir -p "${GOPATH}/bin"

echo "Building ginkgo tool from vendor"
go build -mod=vendor -o "${GOPATH}/bin/ginkgo" "${GINKGO_PACKAGE}/ginkgo"