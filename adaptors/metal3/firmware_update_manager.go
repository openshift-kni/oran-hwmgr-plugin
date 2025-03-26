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
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

// validateFirmwareUpdateSpec checks that the BIOS and firmware URLs are valid
func validateFirmwareUpdateSpec(spec pluginv1alpha1.HardwareProfileSpec) error {

	if spec.BiosFirmware.Version != "" {
		if spec.BiosFirmware.URL == "" {
			return fmt.Errorf("missing BIOS firmware URL for version: %v", spec.BiosFirmware.Version)
		}
		if !utils.IsValidURL(spec.BiosFirmware.URL) {
			return fmt.Errorf("invalid BIOS firmware URL: %v", spec.BiosFirmware.URL)
		}
	}
	if spec.BmcFirmware.Version != "" {
		if spec.BmcFirmware.URL == "" {
			return fmt.Errorf("missing BMC firmware URL for version: %v", spec.BmcFirmware.Version)
		}
		if !utils.IsValidURL(spec.BmcFirmware.URL) {
			return fmt.Errorf("invalid BMC firmware URL: %v", spec.BmcFirmware.URL)
		}
	}

	return nil
}

func convertToFirmwareUpdates(spec pluginv1alpha1.HardwareProfileSpec) []metal3v1alpha1.FirmwareUpdate {
	var updates []metal3v1alpha1.FirmwareUpdate

	if spec.BiosFirmware.URL != "" {
		updates = append(updates, metal3v1alpha1.FirmwareUpdate{
			Component: "bios",
			URL:       spec.BiosFirmware.URL,
		})
	}

	if spec.BmcFirmware.URL != "" {
		updates = append(updates, metal3v1alpha1.FirmwareUpdate{
			Component: "bmc",
			URL:       spec.BmcFirmware.URL,
		})
	}

	return updates
}

func isVersionChangeDetected(ctx context.Context, logger *slog.Logger, status *metal3v1alpha1.HostFirmwareComponentsStatus,
	spec pluginv1alpha1.HardwareProfileSpec) ([]metal3v1alpha1.FirmwareUpdate, bool) {

	firmwareMap := map[string]pluginv1alpha1.Firmware{
		"bios": spec.BiosFirmware,
		"bmc":  spec.BmcFirmware,
	}

	var updates []metal3v1alpha1.FirmwareUpdate
	updateRequired := false

	for _, component := range status.Components {
		if fw, exists := firmwareMap[component.Component]; exists {
			// Skip if firmware spec is empty
			if fw.IsEmpty() {
				logger.DebugContext(ctx, "Skipping firmware update due to empty firmware spec",
					slog.String("component", component.Component))
				continue
			}

			// If version differs, append update
			if component.CurrentVersion != fw.Version {
				updates = append(updates, metal3v1alpha1.FirmwareUpdate{
					Component: component.Component,
					URL:       fw.URL,
				})
				logger.InfoContext(ctx, "Add firmware update",
					slog.String("component", component.Component),
					slog.String("url", fw.URL))
				updateRequired = true
			}
		}
	}

	return updates, updateRequired
}

func (a *Adaptor) createHostFirmwareComponents(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost,
	spec pluginv1alpha1.HardwareProfileSpec) (*metal3v1alpha1.HostFirmwareComponents, error) {

	updates := convertToFirmwareUpdates(spec)

	hfc := metal3v1alpha1.HostFirmwareComponents{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		},
		Spec: metal3v1alpha1.HostFirmwareComponentsSpec{
			Updates: updates,
		},
	}

	if err := a.Client.Create(ctx, &hfc); err != nil {
		return nil, fmt.Errorf("failed to create HostFirmwareComponents: %w", err)
	}

	return hfc.DeepCopy(), nil
}

func (a *Adaptor) updateHostFirmwareComponents(ctx context.Context, name types.NamespacedName, updates []metal3v1alpha1.FirmwareUpdate) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		hfc := &metal3v1alpha1.HostFirmwareComponents{}
		if err := a.Get(ctx, name, hfc); err != nil {
			return fmt.Errorf("failed to fetch HostFirmwareComponents %s/%s: %w", name.Namespace, name.Name, err)
		}
		hfc.Spec.Updates = updates
		return a.Client.Update(ctx, hfc)
	})
}

func (a *Adaptor) IsFirmwareUpdateRequired(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, spec pluginv1alpha1.HardwareProfileSpec) (bool, error) {
	if err := validateFirmwareUpdateSpec(spec); err != nil {
		return false, fmt.Errorf("firmware spec is invalid (%v): %w", spec, err)
	}

	existingHFC, created, err := a.getOrCreateHostFirmwareComponents(ctx, bmh, spec)
	if err != nil {
		return false, err
	}
	// If the resource was just created, we assume an update is needed
	if created {
		return true, nil
	}

	updates, updateRequired := isVersionChangeDetected(ctx, a.Logger, &existingHFC.Status, spec)

	// No update needed if already up-to-date
	if !updateRequired {
		return false, nil
	}

	if err := a.updateHostFirmwareComponents(ctx, types.NamespacedName{
		Name:      existingHFC.Name,
		Namespace: existingHFC.Namespace,
	}, updates); err != nil {
		return false, fmt.Errorf("failed to update HostFirmwareComponents: %w", err)
	}

	return true, nil
}

// Retrieves existing HostFirmwareComponents or creates a new one if not found.
func (a *Adaptor) getOrCreateHostFirmwareComponents(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost,
	spec pluginv1alpha1.HardwareProfileSpec) (*metal3v1alpha1.HostFirmwareComponents, bool, error) {

	hfc := &metal3v1alpha1.HostFirmwareComponents{}
	err := a.Client.Get(ctx, types.NamespacedName{
		Name:      bmh.Name,
		Namespace: bmh.Namespace,
	}, hfc)

	if err != nil {
		if errors.IsNotFound(err) {
			newHFC, err := a.createHostFirmwareComponents(ctx, bmh, spec)
			if err != nil {
				return nil, false, fmt.Errorf("failed to create HostFirmwareComponents: %w", err)
			}
			a.Logger.InfoContext(ctx, "Successfully created HostFirmwareComponents", slog.String("HFC", bmh.Name))
			return newHFC, true, nil
		}
		return nil, false, fmt.Errorf("failed to get HostFirmwareComponents %s/%s: %w", bmh.Namespace, bmh.Name, err)
	}

	return hfc, false, nil
}
