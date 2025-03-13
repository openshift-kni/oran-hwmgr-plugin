#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

# Additional flags to pass to the `ginkgo` command.
ginkgo_flags:=

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 4.18.0

PACKAGE_NAME ?= oran-hwmgr-plugin

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# oran.openshift.io/oran-hwmgr-plugin-bundle:$VERSION and oran.openshift.io/oran-hwmgr-plugin-catalog:$VERSION.
IMAGE_NAME ?= oran-hwmgr-plugin
IMAGE_TAG_BASE ?= quay.io/openshift-kni/$(IMAGE_NAME)

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.38.0

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(VERSION)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28.3
ENVTEST_VERSION = release-0.19
# HWMGR_PLUGIN_NAMESPACE refers to the namespace of the hardware manager plugin.
HWMGR_PLUGIN_NAMESPACE ?= oran-hwmgr-plugin

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# IMAGE_PULL_POLICY sets the value that is patched into the CSV for the manager container imagePullPolicy.
# If the IMAGE_TAG_BASE is a user repo, the default value is updated to imagePullPolicy=Always.
ifneq (,$(findstring openshift-kni,$(IMAGE_TAG_BASE)))
IMAGE_PULL_POLICY ?= IfNotPresent
else
IMAGE_PULL_POLICY ?= Always
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

default: help

# Source directories
SOURCE_DIRS := $(shell find . -maxdepth 1 -type d ! -name "vendor" ! -name "." ! -name ".*")

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: deps-update controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: deps-update go-generate controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

##@ Build
.PHONY: go-generate
go-generate:
	go generate ./...
	cd pkg/inventory-client && go generate ./...

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: manifests generate fmt vet ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: docker-build ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize kubectl ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize kubectl ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize kubectl ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	@$(KUBECTL) create configmap env-config --from-literal=imagePullPolicy=$(IMAGE_PULL_POLICY) --dry-run=client -o yaml > config/manager/env-config.yaml
	cd config/manager \
		&& $(KUSTOMIZE) edit set image controller=${IMG} \
		&& $(KUSTOMIZE) edit add transformer imagePullPolicyReplacement.yaml
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize kubectl ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= $(LOCALBIN)/kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
OAPI_CODEGEN ?= $(LOCALBIN)/oapi-codegen

## Tool Versions
KUSTOMIZE_VERSION ?= v5.2.1
CONTROLLER_TOOLS_VERSION ?= v0.15.0
OAPI_CODEGEN_VERSION ?= v2.4.1

.PHONY: kubectl
kubectl: $(KUBECTL) ## Use envtest to download kubectl
$(KUBECTL): $(LOCALBIN) envtest
	if [ ! -f $(KUBECTL) ]; then \
		KUBEBUILDER_ASSETS=$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path); \
		ln -sf $${KUBEBUILDER_ASSETS}/kubectl $(KUBECTL); \
	fi

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
ifeq ($(shell uname -s),Linux)
	@chmod -R u+w $(LOCALBIN)
endif
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
OPERATOR_SDK_VERSION_INSTALLED = $(shell $(OPERATOR_SDK) version 2>/dev/null | sed 's/^operator-sdk version: "\([^"]*\).*/\1/')
operator-sdk: ## Download operator-sdk locally if necessary.
ifneq ($(OPERATOR_SDK_VERSION),$(OPERATOR_SDK_VERSION_INSTALLED))
	@echo "Previously installed operator-sdk: $(OPERATOR_SDK_VERSION_INSTALLED)"
	@echo "Downloading operator-sdk $(OPERATOR_SDK_VERSION)"
	@{ \
		set -e ;\
		mkdir -p $(dir $(OPERATOR_SDK)) ;\
		OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
		curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
		chmod +x $(OPERATOR_SDK) ;\
	}
endif

.PHONY: oapi-codegen
oapi-codegen: $(OAPI_CODEGEN) ## Download oapi-codegen locally if necessary. If wrong version is installed, it will be overwritten.
$(OAPI_CODEGEN): $(LOCALBIN)
	test -s $(LOCALBIN)/oapi-codegen && $(LOCALBIN)/oapi-codegen --version | grep -q $(OAPI_CODEGEN_VERSION) || \
	GOBIN=$(LOCALBIN) go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)

.PHONY: bundle
bundle: operator-sdk manifests kustomize kubectl ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests --apis-dir api -q
	@$(KUBECTL) create configmap env-config --from-literal=imagePullPolicy=$(IMAGE_PULL_POLICY) --dry-run=client -o yaml > config/manager/env-config.yaml
	cd config/manager \
		&& $(KUSTOMIZE) edit set image controller=$(IMG) \
		&& $(KUSTOMIZE) edit add transformer imagePullPolicyReplacement.yaml
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	@rm bundle/manifests/oran-hwmgr-plugin-env-config_v1_configmap.yaml ## Clean up the temporary file for bundle validate
	$(OPERATOR_SDK) bundle validate ./bundle
	sed -i '/^[[:space:]]*createdAt:/d' bundle/manifests/oran-hwmgr-plugin.clusterserviceversion.yaml

.PHONY: bundle-build
bundle-build: bundle docker-push ## Build the bundle image.
	$(CONTAINER_TOOL) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: bundle-build ## Push the bundle image.
	$(CONTAINER_TOOL) push $(BUNDLE_IMG)

.PHONY: bundle-check
bundle-check: bundle
	hack/check-git-tree.sh

.PHONY: bundle-run
bundle-run: # Install bundle on cluster using operator sdk.
	oc create ns $(HWMGR_PLUGIN_NAMESPACE)
	$(OPERATOR_SDK) --security-context-config restricted -n $(HWMGR_PLUGIN_NAMESPACE) run bundle $(BUNDLE_IMG)

.PHONY: bundle-upgrade
bundle-upgrade: # Upgrade bundle on cluster using operator sdk.
	$(OPERATOR_SDK) run bundle-upgrade $(BUNDLE_IMG)

.PHONY: bundle-clean
bundle-clean: # Uninstall bundle on cluster using operator sdk.
	$(OPERATOR_SDK) cleanup oran-hwmgr-plugin -n $(HWMGR_PLUGIN_NAMESPACE)
	oc delete ns $(HWMGR_PLUGIN_NAMESPACE)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.28.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog
catalog: opm ## Build a catalog.
	@mkdir -p catalog
	hack/generate-catalog-index.sh --opm $(OPM) --name $(PACKAGE_NAME) --channel alpha --version $(VERSION)
	$(OPM) render --output=yaml $(BUNDLE_IMG) > catalog/$(PACKAGE_NAME).yaml
	$(OPM) validate catalog

.PHONY: catalog-build
catalog-build: catalog ## Build a catalog image.
	$(CONTAINER_TOOL) build -f catalog.Dockerfile -t $(CATALOG_IMG) .

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(CONTAINER_TOOL) push ${CATALOG_IMG}

# Deploy from catalog image.
.PHONY: catalog-deploy
catalog-deploy: ## Deploy from catalog image.
	hack/generate-catalog-deploy.sh \
		--package $(PACKAGE_NAME) \
		--namespace $(HWMGR_PLUGIN_NAMESPACE) \
		--catalog-image $(CATALOG_IMG) \
		--channel alpha \
		--install-mode OwnNamespace \
		| oc create -f -

# Undeploy from catalog image.
.PHONY: catalog-undeploy
catalog-undeploy: ## Undeploy from catalog image.
	hack/catalog-undeploy.sh --package $(PACKAGE_NAME) --namespace $(HWMGR_PLUGIN_NAMESPACE) --crd-search "plugin.*oran.openshift.io"

.PHONY: test tests
test tests: manifests generate fmt vet envtest ## Run tests.
ifeq ($(shell uname -s),Linux)
	@chmod -R u+w $(LOCALBIN)
endif
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e:
	go test ./test/e2e/ -v -ginkgo.v

.PHONY: fmt
fmt: ## Run go fmt against code.
	@echo "Run fmt"
	gofmt -s -l -w $(SOURCE_DIRS)

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint:
	hack/golangci-lint.sh

.PHONY: deps-update
deps-update:	controller-gen kustomize
	hack/update-deps.sh

.PHONY: ci-job
ci-job: deps-update generate fmt vet lint test bundle-check

.PHONY: scorecard-test
scorecard-test: operator-sdk
	@test -n "$(KUBECONFIG)" || (echo "The environment variable KUBECONFIG must not empty" && false)
	oc create ns $(HWMGR_PLUGIN_NAMESPACE) --dry-run=client -o yaml | oc apply -f -
	$(OPERATOR_SDK) scorecard bundle -o text --kubeconfig "$(KUBECONFIG)" -n $(HWMGR_PLUGIN_NAMESPACE) --pod-security=restricted

## Adaptors
DELL_ADAPTOR_DIR ?= adaptors/dell-hwmgr
DELL_OPENAPI ?= $(DELL_ADAPTOR_DIR)/dell-oapi.yaml
DELL_TEST_ADAPTOR_DIR ?= test/adaptors/dell-hwmgr/dell-server

.PHONY: dell-api
dell-api: oapi-codegen
	@mkdir -p $(DELL_ADAPTOR_DIR)/generated
	$(OAPI_CODEGEN) -package=api -generate types $(DELL_OPENAPI) >$(DELL_ADAPTOR_DIR)/generated/types.go
	$(OAPI_CODEGEN) -package=api -generate client $(DELL_OPENAPI) >$(DELL_ADAPTOR_DIR)/generated/client.go
	@mkdir -p $(DELL_TEST_ADAPTOR_DIR)/generated
	$(OAPI_CODEGEN) -package=api -generate types,gorilla $(DELL_OPENAPI) >$(DELL_TEST_ADAPTOR_DIR)/generated/server.go
	go mod tidy
	go mod vendor
