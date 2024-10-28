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

## Loopback Adaptor

See [adaptors/loopback/README.md](adaptors/loopback/README.md) for information about the Loopback Adaptor.

## Dell Hardware Manager Adaptor

See [adaptors/dell-hwmgr/README.md](adaptors/dell-hwmgr/README.md) for information about the Dell Hardware Manager Adaptor.
