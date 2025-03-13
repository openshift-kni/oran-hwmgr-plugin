<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Testing adaptors

The `dell-hwmgr` and `loopback` adaptors are tested via the corresponding `Gingko` test suites under this directory leveraging `Envtest`. No cluster is needed to run these test suites.

Use the `test` target to run both test suites from the project root

```console
$ make test
...
ok      github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/dell-hwmgr     17.292s coverage: [no statements]                                                                                                                                              
ok      github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/loopback       30.584s coverage: [no statements]
```

## Testing dependencies

Both adaptors depend on the `Node` and `Nodepool` CRDs which belong to the `oran-o2ims` git repository, respectively: [o2ims-hardwaremanagement.oran.openshift.io_nodes.yaml](https://github.com/openshift-kni/oran-o2ims/blob/main/bundle/manifests/o2ims-hardwaremanagement.oran.openshift.io_nodes.yaml) and [o2ims-hardwaremanagement.oran.openshift.io_nodepools.yaml](https://github.com/openshift-kni/oran-o2ims/blob/main/bundle/manifests/o2ims-hardwaremanagement.oran.openshift.io_nodepools.yaml).

When running these tests, both CRDs and the required `github.com/openshift-kni/oran-o2ims/api/hardwaremanagement` module must be in sync. The test engine is able to sync the CRDs parsing the `go.mod` file and checking out the associated CRDs for that module via git.

Thereby, we just need to indicate the proper module version in `go.mod` using the pseudo version format. Note that `replace` directives are supported as well:

```console
$ cat go.mod

module github.com/openshift-kni/oran-hwmgr-plugin
go 1.22.0
toolchain go1.22.7

require (
  ...
  github.com/openshift-kni/oran-o2ims/api/hardwaremanagement v0.0.0-20241119221834-27fcd2507c33  
  ...

)

replace github.com/openshift-kni/oran-o2ims/api/hardwaremanagement => github.com/abraham2512/oran-o2ims/api/hardwaremanagement v0.0.0-20241118203154-4af906dd6096
```

## Testing the dell-hwmgr adaptor

This adaptor has the particularity of leveraging auto generated code from the [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen?tab=readme-ov-file#generating-api-models) tool to run a 'fake' Dell server, processing the server open api file. This server mocks the rest responses on a test basis.

That generated code is 'api driven' then, generated using the openapi config file. Use the `dell-api` target from the project root to generate this extra Golang code.

```console
$ make dell-api
...
```

This will generate:

- the http infra. See the generated [server.go](test/adaptors/dell-hwmgr/dell-server/generated/server.go).
- the data structures (rest data model) used in the rest dialog with the dell server. See the generated [types.go](https://github.com/openshift-kni/oran-hwmgr-plugin/tree/main/adaptors/dell-hwmgr/generated).

In case that open api file is changed, we might need to adapt our logic accordingly, so it is important to understand how we are dealing with the generated files to arrange our tests.

Let's assume the openapi has changed by any reason and follow these steps to update our tests:

1. Run `make dell-api` to generate the new Golang logic.

2. On the http infra side, examine the new contents of [server.go](test/adaptors/dell-hwmgr/dell-server/generated/server.go) to see any update. As explained before, this corresponds to the http infra the server uses , which can be quite complex,  but for our simple test intend we only need to focus on the interface exposed by that server for the rest endpoints. It is implemented in [dellserver.go](test/adaptors/dell-hwmgr/dell-server/dellserver.go)
New changes might be needed to satisfy that interface again. Besides, provide the new functions you might want to mock in the tests later as part of the interface implementation in that file.That is likely the  only reason to  implement an interface function; otherwise, if it is not mocked, leave it empty.

3. On the data model side, examine the new contents of [types.go](https://github.com/openshift-kni/oran-hwmgr-plugin/tree/main/adaptors/dell-hwmgr/generated) to see any update.

4. Finally, adapt the Golang tests under `dell-hwmgr` to leverage all this new generated logic.

5. Run the tests again to check if these new changes have been properly adapted in the `dell-hwmgr` testsuite:

```console
$ make test
...  
ok      github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/dell-hwmgr     17.292s coverage: [no statements]
```
