#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

function usage {
    cat <<EOF >&2
Paramaters:
    -n <namespace>
    -p <package name>
    -c <channel>
    -i <image ref>
    -m <OwnNamespace | AllNamespaces>
EOF
    exit 1
}

function generateCatalogSource {
    cat <<EOF
---
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  annotations:
    target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
  name: ${PACKAGE}
  namespace: openshift-marketplace
spec:
  displayName: ${PACKAGE}
  image: ${CATALOG_IMG}
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 1h
EOF
}

function generateNamespace {
    cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
  annotations:
    workload.openshift.io/allowed: management
EOF
}

function generateOperatorGroup {
    if [ "${INSTALL_MODE}" = "OwnNamespace" ]; then
    cat <<EOF
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ${PACKAGE}
  namespace: ${NAMESPACE}
spec:
  targetNamespaces:
  - ${NAMESPACE}
EOF
    else
    cat <<EOF
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ${PACKAGE}
  namespace: ${NAMESPACE}
EOF
    fi
}

function generateSubscription {
    cat <<EOF
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: ${PACKAGE}
  namespace: ${NAMESPACE}
spec:
  channel: ${CHANNEL}
  name: ${PACKAGE}
  source: ${PACKAGE}
  sourceNamespace: openshift-marketplace
EOF
}

#
# Command-line processing
#
declare PACKAGE=
declare NAMESPACE=
declare CHANNEL=
declare CATALOG_IMG=
declare INSTALL_MODE=

while getopts ":n:p:i:c:m:" opt; do
    case "${opt}" in
        n)
            NAMESPACE="${OPTARG}"
            ;;
        p)
            PACKAGE="${OPTARG}"
            ;;
        i)
            CATALOG_IMG="${OPTARG}"
            ;;
        c)
            CHANNEL="${OPTARG}"
            ;;
        m)
            INSTALL_MODE="${OPTARG}"
            ;;
        *)
            usage
            ;;
    esac
done

if [ -z "${NAMESPACE}" ] || [ -z "${PACKAGE}" ] || [ -z "${CATALOG_IMG}" ] || [ -z "${CHANNEL}" ] || [ -z "${INSTALL_MODE}" ]; then
    usage
fi

generateCatalogSource
generateNamespace
generateOperatorGroup
generateSubscription
