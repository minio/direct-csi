#!/bin/bash

set -e

CSI_VERSION=$(git describe --tags --always --dirty)
export CSI_VERSION

echo "building direct-csi $CSI_VERSION"
CGO_ENABLED=0 go build -tags 'osusergo netgo static_build' -ldflags="-X github.com/minio/direct-csi/cmd.Version=$CSI_VERSION -extldflags=-static"

