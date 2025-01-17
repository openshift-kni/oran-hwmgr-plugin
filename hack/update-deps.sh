#!/bin/bash

set -e

pushd api/hwmgr-plugin >/dev/null
go mod tidy
popd >/dev/null

go mod vendor
go mod tidy

