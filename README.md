<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# oran-hwmgr-plugin

O-Cloud Hardware Manager Plugin

## HardwareManager Configuration

The `HardwareManager` CRD provides configuration information for an instance of a hardware manager. For a given hardware manager, create a `HardwareManager` CR that selects the appropriate adaptorId, as well as providing the configuration data for that adaptor. The name of this CR would then be used as the `hwMgrId` in the `NodePool` CR, in order to specify which hardware manager instance should be used to handle the request.

For example, if using the Dell hardware manager, create a CR and corresponding secret that specifies the `dell-hwmgr` adaptor with configuration data to allow communication to the hardware manager.

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

If using the loopback adaptor for testing, specify `loopback` as the adaptorId:

```yaml
---
apiVersion: hwmgr-plugin.oran.openshift.io/v1alpha1
kind: HardwareManager
metadata:
  name: loopback-1
  namespace: oran-hwmgr-plugin
spec:
  adaptorId: loopback
  loopbackData:
    additionalInfo: "This is a test string"
```

### Deploying operator from catalog

To deploy from catalog, first build the operator, bundle, and catalog images, pushing to your repo:

```console
$ make IMAGE_TAG_BASE=quay.io/${MY_REPO}/oran-hwmgr-plugin docker-build docker-push bundle-build bundle-push catalog-build catalog-push
```

You can then use the `catalog-deploy` target to generate the catalog and subscription resources and deploy the operator:

```console
$ make IMAGE_TAG_BASE=quay.io/${MY_REPO}/oran-hwmgr-plugin VERSION=4.18.0 catalog-deploy
hack/generate-catalog-deploy.sh \
        --package oran-hwmgr-plugin \
        --namespace oran-hwmgr-plugin \
        --catalog-image quay.io/${MY_REPO}/oran-hwmgr-plugin-catalog:v4.18.0 \
        --channel alpha \
        --install-mode OwnNamespace \
        | oc create -f -
catalogsource.operators.coreos.com/oran-hwmgr-plugin created
namespace/oran-hwmgr-plugin created
operatorgroup.operators.coreos.com/oran-hwmgr-plugin created
subscription.operators.coreos.com/oran-hwmgr-plugin created
```

To undeploy and clean up the installed resources, use the `catalog-undeploy` target:

```console
$ make IMAGE_TAG_BASE=quay.io/${MY_REPO}/oran-hwmgr-plugin catalog-undeploy
hack/catalog-undeploy.sh --package oran-hwmgr-plugin --namespace oran-hwmgr-plugin --crd-search "plugin.*oran.openshift.io"
subscription.operators.coreos.com "oran-hwmgr-plugin" deleted
clusterserviceversion.operators.coreos.com "oran-hwmgr-plugin.v4.18.0" deleted
customresourcedefinition.apiextensions.k8s.io "hardwaremanagers.hwmgr-plugin.oran.openshift.io" deleted
namespace "oran-hwmgr-plugin" deleted
clusterrole.rbac.authorization.k8s.io "oran-hwmgr-plugin-metrics-reader" deleted
catalogsource.operators.coreos.com "oran-hwmgr-plugin" deleted
```

## Loopback Adaptor

See [adaptors/loopback/README.md](adaptors/loopback/README.md) for information about the Loopback Adaptor.

## Dell Hardware Manager Adaptor

See [adaptors/dell-hwmgr/README.md](adaptors/dell-hwmgr/README.md) for information about the Dell Hardware Manager Adaptor.
