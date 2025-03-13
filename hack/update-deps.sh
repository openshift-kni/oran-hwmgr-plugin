#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

set -e

pushd api/hwmgr-plugin >/dev/null
go mod tidy
popd >/dev/null

pushd pkg/inventory-client >/dev/null
go mod tidy
popd >/dev/null

go mod vendor
go mod tidy

