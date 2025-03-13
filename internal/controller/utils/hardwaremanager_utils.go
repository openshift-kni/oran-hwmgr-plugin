/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LogMessagesAnnotation = "hwmgr-plugin.oran.openshift.io/logMessages"
	LogMessagesEnabled    = "enabled"
)

func GetHardwareManagerValidationCondition(hwmgr *pluginv1alpha1.HardwareManager) *metav1.Condition {
	return meta.FindStatusCondition(
		hwmgr.Status.Conditions,
		string(pluginv1alpha1.ConditionTypes.Validation))
}

func IsHardwareManagerValidationCompleted(hwmgr *pluginv1alpha1.HardwareManager) bool {
	validationCondition := GetHardwareManagerValidationCondition(hwmgr)
	if validationCondition != nil && validationCondition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}

func IsHardwareManagerValidationFailed(hwmgr *pluginv1alpha1.HardwareManager) bool {
	validationCondition := GetHardwareManagerValidationCondition(hwmgr)
	if validationCondition != nil && validationCondition.Reason == string(pluginv1alpha1.ConditionReasons.Failed) {
		return true
	}

	return false
}

func IsHardwareManagerLogMessagesEnabled(hwmgr *pluginv1alpha1.HardwareManager) bool {
	annotations := hwmgr.GetAnnotations()
	if annotations == nil {
		return false
	}

	return annotations[LogMessagesAnnotation] == LogMessagesEnabled
}

func UpdateHardwareManagerStatusCondition(
	ctx context.Context,
	c client.Client,
	hwmgr *pluginv1alpha1.HardwareManager,
	conditionType pluginv1alpha1.ConditionType,
	conditionReason pluginv1alpha1.ConditionReason,
	conditionStatus metav1.ConditionStatus,
	message string) error {

	SetStatusCondition(&hwmgr.Status.Conditions,
		string(conditionType),
		string(conditionReason),
		conditionStatus,
		message)

	if err := UpdateK8sCRStatus(ctx, c, hwmgr); err != nil {
		return fmt.Errorf("failed to update hwmgr status %s: %w", hwmgr.Name, err)
	}

	return nil
}
