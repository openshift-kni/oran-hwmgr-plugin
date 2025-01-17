#!/bin/bash

set -e

pushd api/hwmgr-plugin >/dev/null
go mod tidy
popd >/dev/null

pushd pkg/inventory-client >/dev/null
go mod tidy
popd >/dev/null

go mod vendor
go mod tidy

