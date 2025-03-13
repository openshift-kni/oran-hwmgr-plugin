<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# loopback-adaptor

The Loopback Adaptor for the O-Cloud Hardware Manager Plugin provides a test mechanism to mock the interactions between
the O-Cloud Manager and a generic hardware manager.

## Overview

The O-Cloud Hardware Manager Plugin monitors its own namespace for NodePool CRs. In order to process a NodePool CR, the
Plugin uses an adaptor layer, handing off the CR to the appropriate adaptor.

The Loopback Adapator uses a configmap, named `loopback-adaptor-nodelist`, to manage resources. The configmap includes
resource data defined by the user, with a list of hardware profile names and information about managed nodes, and is
also updated by the Loopback Adaptor to track allocated resources as NodePool CRs are processed. See
[examples/example-nodelist.yaml](examples/example-nodelist.yaml) for an example configmap. In addition, the
[examples/nodelist-generator.sh](examples/nodelist-generator.sh) script can be used to generate the configmap.

As free nodes are allocated to a NodePool request, these are tracked in the `allocations` field in the configmap and a
Node CR is created by the Loopback Adaptor, setting the node properties as defined in the configmap.

In addition, the Loopback Adaptor will create a `Secret` in its own namespace for each node it allocates, named
`<nodename>-bmc-secret`.

When a NodePool CR is deleted, the Plugin is triggered by a finalizer it added to the CR. In processing the deletion,
the Loopback Adaptor will delete any Node CRs that have been allocated for the NodePool and the corresponding
bmc-secret, then free the node(s) in the `loopback-adaptor-nodelist` configmap.

## Testing

### Install O-Cloud Manager

The O-Cloud Hardware Manager Plugin uses the NodePool and Node CRDs that are defined by the O-Cloud Manager. Please
consult the documentation in the [openshift-kni/oran-o2ims](https://github.com/openshift-kni/oran-o2ims) repository for
information on deploying the O-Cloud Manager to your cluster.

To run the Plugin as a standalone without the O-Cloud Manager, the CRDs can be installed from the repo by running:
`make install`

### Deploy O-Cloud Hardware Manager Plugin

The O-Cloud Hardware Manager Plugin can be deployed to your cluster by first building the image and then running the `deploy` make target:

```console
$ make IMAGE_TAG_BASE=quay.io/$USER/oran-hwmgr-plugin VERSION=latest docker-build docker-push
$ make IMAGE_TAG_BASE=quay.io/$USER/oran-hwmgr-plugin VERSION=latest deploy
```

To watch the Loopback Adaptor logs, run the following command:

```console
oc logs -n oran-hwmgr-plugin -l control-plane=controller-manager -f
```

The [examples/nodelist-generator.sh](examples/nodelist-generator.sh) script can be used to generate a `loopback-adaptor-nodelist` configmap with
user-defined hardware profiles and any number of nodes.

Example test NodePool CRs can also be found in the [examples](examples) folder.

```console
$ ./examples/nodelist-generator.sh \
    --resourcepool profile-spr-single-processor-64G:dummy-sp-64g:5 \
	--resourcepool profile-spr-dual-processor-128G:dummy-dp-128g:3 \
	| oc create -f -
configmap/loopback-adaptor-nodelist created
```

The HardwareManager CR for the loopback adaptor can be installed via [../../examples/loopback-1.yaml](../../examples/loopback-1.yaml)

```console
$ oc create -f ../../examples/loopback-1.yaml
hardwaremanager.hwmgr-plugin.oran.openshift.io/loopback-1 created
```

Similarly, we can create a NodePool CR:

```console
$ oc create -f examples/np1.yaml
nodepool.o2ims-hardwaremanagement.oran.openshift.io/np1 created

$ oc get nodepools.o2ims-hardwaremanagement.oran.openshift.io -n oran-hwmgr-plugin np1 -o yaml
apiVersion: o2ims-hardwaremanagement.oran.openshift.io/v1alpha1
kind: NodePool
metadata:
  creationTimestamp: "2024-09-18T17:29:00Z"
  finalizers:
  - oran-hwmgr-plugin/nodepool-finalizer
  generation: 1
  name: np1
  namespace: oran-hwmgr-plugin
  resourceVersion: "15851"
  uid: 00be582c-3026-4421-bb92-b394415cff6b
spec:
  cloudID: testcloud-1
  location: ottawa
  nodeGroup:
  - hwProfile: profile-spr-single-processor-64G
    name: controller
    size: 1
    resourcePoolId: xyz-master
    role: master
  - hwProfile: profile-spr-dual-processor-128G
    name: worker
    size: 0
    resourcePoolId: xyz-worker
    role: worker
  site: building-1
status:
  conditions:
  - lastTransitionTime: "2024-09-18T17:29:21Z"
    message: Created
    reason: Completed
    status: "True"
    type: Provisioned
  properties:
    nodeNames:
    - dummy-sp-64g-1

```

At this moment, the loopback adaptor will reconcile this CR, creating nodes with the info provided by loopback-adaptor-nodelist CM with the hardware profile specified by the NodePool CR.

We can examine logs and Node objects to confirm this:

```console
oc logs -n oran-hwmgr-plugin -l control-plane=controller-manager -f
...
2024/10/24 11:04:38 INFO Creating node: controller=adaptors adaptor=loopback cloudID=testcloud-1 "nodegroup name"=master nodename=dummy-sp-64g-0
2024/10/24 11:04:38 INFO Updating node: controller=adaptors adaptor=loopback nodename=dummy-sp-64g-0
2024/10/24 11:04:38 INFO Adding info to node controller=adaptors adaptor=loopback nodename=dummy-sp-64g-0 info="{HwProfile:profile-spr-single-processor-64G BMC:0xc00052c570 Interfaces:[0xc00052c5d0] Hostname:dummy-sp-64g-0.localhost}"
2024/10/24 11:04:38 INFO nodegroup is fully allocated controller=adaptors adaptor=loopback nodegroup=worker
2024/10/24 11:04:38 INFO Allocating node for CheckNodePoolProgress request: controller=adaptors adaptor=loopback cloudID=testcloud-1 "nodegroup name"=worker

$ oc get nodes.o2ims-hardwaremanagement.oran.openshift.io -n oran-hwmgr-plugin
NAME             AGE     STATE
dummy-sp-64g-0   3m26s   Completed

$ oc get nodes.o2ims-hardwaremanagement.oran.openshift.io -n oran-hwmgr-plugin dummy-sp-64g-0 -o yaml
apiVersion: o2ims-hardwaremanagement.oran.openshift.io/v1alpha1
kind: Node
metadata:
  creationTimestamp: "2024-10-24T11:04:38Z"
  generation: 1
  name: dummy-sp-64g-0
  namespace: oran-hwmgr-plugin
  resourceVersion: "125600522"
  uid: 32250756-d8e4-462e-b955-1c3b89335d31
spec:
  groupName: master
  hwProfile: profile-spr-single-processor-64G
  nodePool: testcloud-1
status:
  bmc:
    address: idrac-virtualmedia+https://192.168.2.0/redfish/v1/Systems/System.Embedded.1
    credentialsName: dummy-sp-64g-0-bmc-secret
  conditions:
  - lastTransitionTime: "2024-10-24T11:04:38Z"
    message: Provisioned
    reason: Completed
    status: "True"
    type: Provisioned
  interfaces:
  - label: bootable-interface
    macAddress: c6:b6:13:a0:02:00
    name: eth0

$ oc get configmap -n oran-hwmgr-plugin loopback-adaptor-nodelist -o yaml
apiVersion: v1
data:
  allocations: |
    clouds:
    - cloudID: testcloud-1
      nodegroups:
        master:
        - dummy-sp-64g-1
  resources: |
    resourcepools:
      - master
      - worker
    nodes:
      dummy-dp-128g-0:
        poolID: worker
        bmc:
          address: "idrac-virtualmedia+https://192.168.1.0/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:01:00"
      dummy-dp-128g-1:
        poolID: worker
        bmc:
          address: "idrac-virtualmedia+https://192.168.1.1/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:01:01"
      dummy-dp-128g-2:
        poolID: worker
        bmc:
          address: "idrac-virtualmedia+https://192.168.1.2/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:01:02"
      dummy-sp-64g-0:
        poolID: master
        bmc:
          address: "idrac-virtualmedia+https://192.168.2.0/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:02:00"
      dummy-sp-64g-1:
        poolID: master
        bmc:
          address: "idrac-virtualmedia+https://192.168.2.1/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:02:01"
      dummy-sp-64g-2:
        poolID: master
        bmc:
          address: "idrac-virtualmedia+https://192.168.2.2/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:02:02"
      dummy-sp-64g-3:
        poolID: master
        bmc:
          address: "idrac-virtualmedia+https://192.168.2.3/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:02:03"
      dummy-sp-64g-4:
        poolID: master
        bmc:
          address: "idrac-virtualmedia+https://192.168.2.4/redfish/v1/Systems/System.Embedded.1"
          username-base64: YWRtaW4=
          password-base64: bXlwYXNz
        interfaces:
          - name: eth0
            label: bootable-interface
            macAddress: "c6:b6:13:a0:02:04"
kind: ConfigMap
metadata:
  creationTimestamp: "2024-09-18T17:28:38Z"
  name: loopback-adaptor-nodelist
  namespace: oran-hwmgr-plugin

  resourceVersion: "15829"
  uid: e6dfe91c-0713-46d2-b3e5-b883c3d8b8c5

```

### Clean up O-Cloud Hardware Manager Plugin

We cannot call `make undeploy` directly to clean up all the resources created in `oran-hwmgr-plugin` namespace while the Nodepool exists:

```console
$ oc get nodepools.o2ims-hardwaremanagement.oran.openshift.io -n oran-hwmgr-plugin np1 -o yaml
apiVersion: o2ims-hardwaremanagement.oran.openshift.io/v1alpha1
kind: NodePool
metadata:
  ...
  finalizers:
  - oran-hwmgr-plugin/nodepool-finalizer
```

We will firstly delete the Nodepool and then undeploy (execute the make command from the project root):

```console
$ oc delete -f examples/np1.yaml
$ make undeploy
```

Additionally, we might remove the CRDs installed from the O-Cloud Manager,uninstalling them executing this from the corresponding project root:

```
$ make uninstall
...
customresourcedefinition.apiextensions.k8s.io "nodepools.o2ims-hardwaremanagement.oran.openshift.io" deleted
customresourcedefinition.apiextensions.k8s.io "nodes.o2ims-hardwaremanagement.oran.openshift.io" deleted
...
```
