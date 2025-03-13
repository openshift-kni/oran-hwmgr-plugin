<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# dell-hwmgr

The Dell Hardware Manager Adaptor for O-Cloud Hardware Manager Plugin provides the interactions between the O-Cloud
Manager and one or more configured Dell Hardware Managers.

## Overview

The Dell Hardware Manager Adaptor requires the `.spec.dellData` field to be set in the `HardwareManager` CR, providing
the necessary configuration information to establish an authenticated connection to the hardware manager instance. The
Plugin will interact with the hardware manager while processing a `NodePool` CR.

## Configuration

The `dellData` of the `HardwareManager` CR provides the following information:

- apiUrl: The address for the hardware manager.
- authSecret: The name of the secret in the Plugin namespace that provides the username and password to be used when
  requesting a token.

The secret follows the `kubernetes.io/basic-auth` type format, with `username` and `password` data fields, along with the `client-id` field.

Example:

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: dell-1
  namespace: oran-hwmgr-plugin
type: kubernetes.io/basic-auth
data:
  client-id: bXljbGllbnQ=
  username: YWRtaW4=
  password: bm90cmVhbA==
---
apiVersion: hwmgr-plugin.oran.openshift.io/v1alpha1
kind: HardwareManager
metadata:
  name: dell-1
  namespace: oran-hwmgr-plugin
spec:
  adaptorId: dell-hwmgr
  dellData:
    authSecret: dell-1
    apiUrl: https://myserver.example.com:443/
```

If the Plugin is able to establish an authenticated connection to the hardware manager, a `Validation` condition is set
to True on the `HardwareManager` CR to indicate that the CR has been validated and authentication was successful. If
not, the `Validation` field is set to False with a message indicating that authentication has failed.

```console
$ oc get -n oran-hwmgr-plugin hwmgr
NAME           AGE   REASON      STATUS   DETAILS
dell-1         16h   Completed   True     Authentication passed
dell-badauth   16h   Failed      False    Authentication failure - failed to get token for dell-badauth: token request failed with status 401 Unauthorized (401), message={"code":401,"message":"internal error occurred while creating token","details":{"domain":"IAM","reason":"Unauthorized","metadata":{"DTIASErrorCode":"DELL-DTIAS-IAM-20016","DTIASErrorMessage":"internal error occurred while creating token","HTTPErrorCode":"401","ManagedServiceError":"Invalid user credentials","Resolution":"contact administrator with the error for further assistance."}}}
$ oc get -n oran-hwmgr-plugin hwmgr -o yaml
apiVersion: v1
items:
- apiVersion: hwmgr-plugin.oran.openshift.io/v1alpha1
  kind: HardwareManager
  metadata:
    creationTimestamp: "2024-10-21T21:38:42Z"
    generation: 1
    name: dell-1
    namespace: oran-hwmgr-plugin
    resourceVersion: "514800972"
    uid: ca721c06-98af-468a-b51f-d0d0a15a629f
  spec:
    adaptorId: dell-hwmgr
    dellData:
      apiUrl: https://myserver.example.com:443/
      authSecret: dell-1
      clientId: ccpapi
  status:
    conditions:
    - lastTransitionTime: "2024-10-21T21:38:42Z"
      message: Authentication passed
      reason: Completed
      status: "True"
      type: Validation
- apiVersion: hwmgr-plugin.oran.openshift.io/v1alpha1
  kind: HardwareManager
  metadata:
    creationTimestamp: "2024-10-21T21:39:13Z"
    generation: 1
    name: dell-badauth
    namespace: oran-hwmgr-plugin
    resourceVersion: "514801993"
    uid: 292d5726-28b3-4cef-873c-ace0bad22f55
  spec:
    adaptorId: dell-hwmgr
    dellData:
      apiUrl: https://myserver.example.com:443/
      authSecret: dell-badauth
      clientId: ccpapi
  status:
    conditions:
    - lastTransitionTime: "2024-10-21T21:39:13Z"
      message: 'Authentication failure - failed to get token for dell-badauth: token
        request failed with status 401 Unauthorized (401), message={"code":401,"message":"internal
        error occurred while creating token","details":{"domain":"IAM","reason":"Unauthorized","metadata":{"DTIASErrorCode":"DELL-DTIAS-IAM-20016","DTIASErrorMessage":"internal
        error occurred while creating token","HTTPErrorCode":"401","ManagedServiceError":"Invalid
        user credentials","Resolution":"contact administrator with the error for further
        assistance."}}}'
      reason: Failed
      status: "False"
      type: Validation
kind: List
metadata:
  resourceVersion: ""
```

## Debug

Message tracing, which logs the JSON request and response data for interactions with the hardware manager, can be
enabled by setting an annotation on the HardwareManager CR, setting
`hwmgr-plugin.oran.openshift.io/logMessages=enabled`. Any other value will disable the tracing.

```console
# Add the annotation to target HardwareManager
oc annotate -n oran-hwmgr-plugin HardwareManager <hwmgr> hwmgr-plugin.oran.openshift.io/logMessages=enabled

# Remove the annotation
oc annotate -n oran-hwmgr-plugin HardwareManager <hwmgr> hwmgr-plugin.oran.openshift.io/logMessages-
```
