/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"fmt"

	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	typederrors "github.com/openshift-kni/oran-hwmgr-plugin/internal/typed-errors"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// convertBiosSettingsToHostFirmware converts BiosSettings to HostFirmwareSettings CR
func convertBiosSettingsToHostFirmware(bmh metal3v1alpha1.BareMetalHost, biosSettings pluginv1alpha1.Bios) metal3v1alpha1.HostFirmwareSettings {
	return metal3v1alpha1.HostFirmwareSettings{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		},
		Spec: metal3v1alpha1.HostFirmwareSettingsSpec{
			Settings: biosSettings.Attributes, // Copy attributes directly
		},
	}
}

func (a *Adaptor) createHostFirmwareSettings(ctx context.Context, hfs *metal3v1alpha1.HostFirmwareSettings) error {
	if err := a.Client.Create(ctx, hfs); err != nil {
		a.Logger.InfoContext(ctx, "Failed to create HostFirmwareSettings", slog.String("HFS", hfs.Name))
		return fmt.Errorf("failed to create HostFirmwareSettings: %w", err)
	}
	return nil
}

func (a *Adaptor) updateHostFirmwareSettings(ctx context.Context, name types.NamespacedName, settings metal3v1alpha1.HostFirmwareSettings) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		existingHFS := &metal3v1alpha1.HostFirmwareSettings{}

		if err := a.Get(ctx, name, existingHFS); err != nil {
			return fmt.Errorf("failed to fetch BMH %s/%s: %w", name.Namespace, name.Name, err)
		}
		existingHFS.Spec.Settings = settings.Spec.Settings
		return a.Client.Update(ctx, existingHFS)
	})
}

func (a *Adaptor) IsBiosUpdateRequired(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, biosSettings pluginv1alpha1.Bios) (bool, error) {
	hfs := convertBiosSettingsToHostFirmware(*bmh, biosSettings)

	existingHFS, err := a.getOrCreateHostFirmwareSettings(ctx, &hfs)
	if err != nil {
		return false, err
	}

	if err := a.validateBiosSettings(ctx, existingHFS, hfs.Spec.Settings); err != nil {
		return false, fmt.Errorf("hfs %s/%s: %w", existingHFS.Namespace, existingHFS.Name, err)
	}

	return a.checkAndUpdateFirmwareSettings(ctx, existingHFS, &hfs)
}

// Retrieves existing HostFirmwareSettings or creates a new one if not found.
func (a *Adaptor) getOrCreateHostFirmwareSettings(ctx context.Context, hfs *metal3v1alpha1.HostFirmwareSettings) (*metal3v1alpha1.HostFirmwareSettings, error) {
	existingHFS := &metal3v1alpha1.HostFirmwareSettings{}
	err := a.Client.Get(ctx, types.NamespacedName{Name: hfs.Name, Namespace: hfs.Namespace}, existingHFS)

	if err != nil {
		if errors.IsNotFound(err) {
			if err := a.createHostFirmwareSettings(ctx, hfs); err != nil {
				a.Logger.InfoContext(ctx, "Failed to create HostFirmwareSettings", slog.String("HFS", hfs.Name))
				return nil, fmt.Errorf("failed to create HostFirmwareSettings: %w", err)
			}
			a.Logger.InfoContext(ctx, "Successfully created HostFirmwareSettings", slog.String("HFS", hfs.Name))
			return hfs.DeepCopy(), nil
		}
		a.Logger.InfoContext(ctx, "Failed to get HostFirmwareSettings", slog.String("HFS", hfs.Name))
		return nil, fmt.Errorf("failed to get HostFirmwareSettings %s/%s: %w", hfs.Namespace, hfs.Name, err)
	}

	return existingHFS, nil
}

// Validates the BIOS settings against the firmware schema.
func (a *Adaptor) validateBiosSettings(ctx context.Context, existingHFS *metal3v1alpha1.HostFirmwareSettings, newSettings map[string]intstr.IntOrString) error {
	if existingHFS.Status.FirmwareSchema == nil {
		return fmt.Errorf("failed to get FirmwareSchema from HFS: %+v", existingHFS)
	}
	if existingHFS.Status.FirmwareSchema.Name == "" || existingHFS.Status.FirmwareSchema.Namespace == "" {
		return fmt.Errorf("firmwareSchema name or namespace is nil: %+v", existingHFS.Status.FirmwareSchema)
	}

	firmwareSchema := &metal3v1alpha1.FirmwareSchema{}
	if err := a.Client.Get(ctx, client.ObjectKey{Name: existingHFS.Status.FirmwareSchema.Name,
		Namespace: existingHFS.Status.FirmwareSchema.Namespace}, firmwareSchema); err != nil {
		return fmt.Errorf("failed to get FirmwareSchema %s/%s: %w", existingHFS.Status.FirmwareSchema,
			existingHFS.Status.FirmwareSchema.Name, err)
	}

	validationErrors := validSettings(existingHFS, firmwareSchema, newSettings)
	if len(validationErrors) != 0 {
		return typederrors.NewInputError("invalid BIOS settings: %+v", validationErrors)
	}

	return nil
}

// Checks if BIOS settings have changed and updates if necessary.
func (a *Adaptor) checkAndUpdateFirmwareSettings(ctx context.Context, existingHFS, hfs *metal3v1alpha1.HostFirmwareSettings) (bool, error) {
	if isChangeDetected(ctx, a.Logger, hfs.Spec.Settings, existingHFS.Status.Settings) {
		a.Logger.InfoContext(ctx, "Updating existing HostFirmwareSettings", slog.String("HFS", hfs.Name))

		if err := a.updateHostFirmwareSettings(ctx, types.NamespacedName{Name: hfs.Name, Namespace: hfs.Namespace}, *hfs); err != nil {
			a.Logger.InfoContext(ctx, "Failed to update HostFirmwareSettings", slog.String("HFS", hfs.Name))
			return false, fmt.Errorf("failed to update HostFirmwareSettings: %w", err)
		}

		a.Logger.InfoContext(ctx, "Successfully updated HostFirmwareSetting", slog.String("HFS", hfs.Name))
		return true, nil
	}

	a.Logger.InfoContext(ctx, "No changes detected in HostFirmwareSettings", slog.String("HFS", hfs.Name))
	return false, nil
}

func validSettings(hfs *metal3v1alpha1.HostFirmwareSettings, schema *metal3v1alpha1.FirmwareSchema,
	newSettings map[string]intstr.IntOrString) []error {

	var validationErrors []error

	for name, val := range newSettings {

		// The setting must be in the Status
		if _, ok := hfs.Status.Settings[name]; !ok {
			validationErrors = append(validationErrors, fmt.Errorf("setting %s is not in the Status field", name))
			continue
		}

		// check validity of updated value
		if schema != nil {
			if err := schema.ValidateSetting(name, val, schema.Spec.Schema); err != nil {
				validationErrors = append(validationErrors, err)
			}
		}
	}

	return validationErrors
}

// isChangeDetected compares two maps (used to detect changes in BIOS attributes)
func isChangeDetected(ctx context.Context, logger *slog.Logger, a map[string]intstr.IntOrString, b map[string]string) bool {
	// Check if any Spec settings are different than Status
	changed := false
	for k, v := range a {
		if statusVal, ok := b[k]; ok {
			if v.String() != statusVal {
				logger.InfoContext(ctx, "spec value different than status", slog.String("name", k),
					slog.String("specvalue", v.String()), slog.String("statusvalue", statusVal))
				changed = true
				break
			}
		} else {
			// Spec setting is not in Status, this will be handled by validateHostFirmwareSettings
			changed = true
			break
		}
	}

	return changed
}
