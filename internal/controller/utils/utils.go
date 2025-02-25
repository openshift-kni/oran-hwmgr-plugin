/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	typederrors "github.com/openshift-kni/oran-hwmgr-plugin/internal/typed-errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

// Resource operations
const (
	UPDATE = "Update"
	PATCH  = "Patch"
)

const (
	JobIdAnnotation         = "hwmgr-plugin.oran.openshift.io/jobId"
	DeletionJobIdAnnotation = "hwmgr-plugin.oran.openshift.io/deletionJobId"
)

func UpdateK8sCRStatus(ctx context.Context, c client.Client, object client.Object) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Status().Update(ctx, object); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("status update failed after retries: %w", err)
	}

	return nil
}

// CreateOrUpdateK8sCR creates/updates/patches an object.
func CreateOrUpdateK8sCR(ctx context.Context, c client.Client,
	newObject client.Object, ownerObject client.Object,
	operation string) (err error) {

	// Get the name and namespace of the object:
	key := client.ObjectKeyFromObject(newObject)

	// We can set the owner reference only for objects that live in the same namespace, as cross
	// namespace owners are forbidden. This also applies to non-namespaced objects like cluster
	// roles or cluster role bindings; those have empty namespaces, so the equals comparison
	// should also work.
	if ownerObject != nil && ownerObject.GetNamespace() == key.Namespace {
		err = controllerutil.SetControllerReference(ownerObject, newObject, c.Scheme())
		if err != nil {
			return fmt.Errorf("failed to set controller reference: %w", err)
		}
	}

	// Create an empty object of the same type of the new object. We will use it to fetch the
	// current state.
	objectType := reflect.TypeOf(newObject).Elem()
	oldObject := reflect.New(objectType).Interface().(client.Object)

	// If the newObject is unstructured, we need to copy the GVK to the oldObject.
	if unstructuredObj, ok := newObject.(*unstructured.Unstructured); ok {
		oldUnstructuredObj := oldObject.(*unstructured.Unstructured)
		oldUnstructuredObj.SetGroupVersionKind(unstructuredObj.GroupVersionKind())
	}

	err = c.Get(ctx, key, oldObject)

	// If there was an error obtaining the CR and the error was "Not found", create the object.
	// If any other occurred, return the error.
	// If the CR already exists, patch it or update it.
	if err != nil {
		if errors.IsNotFound(err) {
			err = c.Create(ctx, newObject)
			if err != nil {
				return fmt.Errorf("failed to create CR %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
			}
		} else {
			return fmt.Errorf("failed to get CR %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
		}
	} else {
		newObject.SetResourceVersion(oldObject.GetResourceVersion())
		if operation == PATCH {
			if err := c.Patch(ctx, newObject, client.MergeFrom(oldObject)); err != nil {
				return fmt.Errorf("failed to patch object %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
			}
			return nil
		} else if operation == UPDATE {
			if err := c.Update(ctx, newObject); err != nil {
				return fmt.Errorf("failed to update object %s/%s: %w", newObject.GetNamespace(), newObject.GetName(), err)
			}
			return nil
		}
	}

	return nil
}

func DoesK8SResourceExist(ctx context.Context, c client.Client, name, namespace string, obj client.Object) (resourceExists bool, err error) {
	err = c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)

	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		} else {
			return false, fmt.Errorf("failed to check existence of resource '%s' in namespace '%s': %w", name, namespace, err)
		}
	} else {
		return true, nil
	}
}

func GetConfigmap(ctx context.Context, c client.Client, name, namespace string) (*corev1.ConfigMap, error) {
	existingConfigmap := &corev1.ConfigMap{}
	cmExists, err := DoesK8SResourceExist(
		ctx, c, name, namespace, existingConfigmap)
	if err != nil {
		return nil, typederrors.NewConfigMapError(err, "failed to check configMap %s in the namespace %s", name, namespace)
	}

	if !cmExists {
		// Check if the configmap is missing
		return nil, typederrors.NewConfigMapError(nil,
			"the ConfigMap %s is not found in the namespace %s", name, namespace)
	}
	return existingConfigmap, nil
}

// GetConfigMapField attempts to retrieve the value of the field using the provided field name
func GetConfigMapField(cm *corev1.ConfigMap, fieldName string) (string, error) {
	data, ok := cm.Data[fieldName]
	if !ok {
		return data, typederrors.NewConfigMapError(nil, "the ConfigMap '%s' does not contain a field named '%s'", cm.Name, fieldName)
	}

	return data, nil
}

func ExtractDataFromConfigMap[T any](cm *corev1.ConfigMap, expectedKey string) (T, error) {
	var validData T

	// Find the expected key is present in the configmap data
	defaults, err := GetConfigMapField(cm, expectedKey)
	if err != nil {
		return validData, err
	}

	// Parse the YAML data into a map
	err = yaml.Unmarshal([]byte(defaults), &validData)
	if err != nil {
		return validData, typederrors.NewConfigMapError(
			err, "the value of key %s from ConfigMap %s is not in a valid YAML string: %s",
			expectedKey, cm.GetName(), err.Error())
	}
	return validData, nil
}

// GetSecret attempts to retrieve a Secret object for the given name
func GetSecret(ctx context.Context, c client.Client, name, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	exists, err := DoesK8SResourceExist(ctx, c, name, namespace, secret)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, typederrors.NewSecretError(nil, "the Secret '%s' is not found in the namespace '%s'", name, namespace)
	}
	return secret, nil
}

// GetSecretField attempts to retrieve the value of the field using the provided field name
func GetSecretField(secret *corev1.Secret, fieldName string) (string, error) {
	encoded, ok := secret.Data[fieldName]
	if !ok {
		return "", typederrors.NewSecretError(nil, "the Secret '%s' does not contain a field named '%s'", secret.Name, fieldName)
	}

	return string(encoded), nil
}

func GetAdaptorIdFromHwMgrId(hwMgrId string) string {
	fields := strings.Split(hwMgrId, ",")
	if len(fields) == 0 {
		return ""
	} else {
		return fields[0]
	}
}

func GetJobId(object client.Object) string {
	annotations := object.GetAnnotations()
	if annotations == nil {
		return ""
	}

	return annotations[JobIdAnnotation]
}

func SetJobId(object client.Object, jobId string) {
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[JobIdAnnotation] = jobId
	object.SetAnnotations(annotations)
}

func ClearJobId(object client.Object) {
	annotations := object.GetAnnotations()
	if annotations != nil {
		delete(annotations, JobIdAnnotation)
	}
}

func GetDeletionJobId(object client.Object) string {
	annotations := object.GetAnnotations()
	if annotations == nil {
		return ""
	}

	return annotations[DeletionJobIdAnnotation]
}

func SetDeletionJobId(object client.Object, jobId string) {
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[DeletionJobIdAnnotation] = jobId
	object.SetAnnotations(annotations)
}

func ClearDeletionJobId(object client.Object) {
	annotations := object.GetAnnotations()
	if annotations != nil {
		delete(annotations, DeletionJobIdAnnotation)
	}
}

//
// Reconciler utilities
//

func DoNotRequeue() ctrl.Result {
	return ctrl.Result{Requeue: false}
}

func RequeueWithLongInterval() ctrl.Result {
	return RequeueWithCustomInterval(5 * time.Minute)
}

func RequeueWithMediumInterval() ctrl.Result {
	return RequeueWithCustomInterval(1 * time.Minute)
}

func RequeueWithShortInterval() ctrl.Result {
	return RequeueWithCustomInterval(15 * time.Second)
}

func RequeueWithCustomInterval(interval time.Duration) ctrl.Result {
	return ctrl.Result{RequeueAfter: interval}
}

func RequeueImmediately() ctrl.Result {
	return ctrl.Result{Requeue: true}
}

//
// Retry utilities
//

func isConflictOrRetriable(err error) bool {
	return errors.IsConflict(err) || errors.IsInternalError(err) || errors.IsServiceUnavailable(err) || net.IsConnectionRefused(err)
}

func isConflictOrRetriableOrNotFound(err error) bool {
	return isConflictOrRetriable(err) || errors.IsNotFound(err)
}

func RetryOnConflictOrRetriable(backoff wait.Backoff, fn func() error) error {
	// nolint: wrapcheck
	return retry.OnError(backoff, isConflictOrRetriable, fn)
}

func RetryOnConflictOrRetriableOrNotFound(backoff wait.Backoff, fn func() error) error {
	// nolint: wrapcheck
	return retry.OnError(backoff, isConflictOrRetriableOrNotFound, fn)
}
