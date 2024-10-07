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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HardwareManagerAdaptorID defines the type for the Hardware Manager Adaptor
type HardwareManagerAdaptorID string

// SupportedAdaptors defines the string values for valid stages
var SupportedAdaptors = struct {
	Loopback HardwareManagerAdaptorID
	Dell     HardwareManagerAdaptorID
}{
	Loopback: "loopback",
	Dell:     "dell-hwmgr",
}

// LoopbackData defines configuration data for loopback adaptor instance
type LoopbackData struct {
	// A test string
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	AddtionalInfo string `json:"additional-info,omitempty"`
}

// DellData defines configuration data for dell-hwmgr adaptor instance
type DellData struct {
	// The username for connection to the Dell Hardware Manager
	// +kubebuilder:validation:Required
	// +required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	User string `json:"user"`
}

// HardwareManagerSpec defines the desired state of HardwareManager
type HardwareManagerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// The adaptor ID
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=loopback;dell-hwmgr
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	AdaptorID HardwareManagerAdaptorID `json:"adaptor-id"`

	// Config data for an instance of the loopback adaptor
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	LoopbackData *LoopbackData `json:"loopback-data,omitempty"`

	// Config data for an instance of the dell-hwmgr adaptor
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	DellData *DellData `json:"dell-data,omitempty"`
}

// HardwareManagerStatus defines the observed state of HardwareManager
type HardwareManagerStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions describe the state of the UpdateService resource.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +operator-sdk:csv:customresourcedefinitions:resources={{Service,v1,policy-engine-service}}
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the HardwareManager resource."
// +kubebuilder:printcolumn:name="Adaptor ID",type="string",JSONPath=".status.adaptor-id",description="The adaptor ID.",priority=1
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=hardwaremanagers,scope=Namespaced

// HardwareManager is the Schema for the hardwaremanagers API
type HardwareManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HardwareManagerSpec   `json:"spec,omitempty"`
	Status HardwareManagerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HardwareManagerList contains a list of HardwareManager
type HardwareManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HardwareManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HardwareManager{}, &HardwareManagerList{})
}
