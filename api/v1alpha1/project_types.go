/*
Copyright 2026.

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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProjectSpec defines the desired state of Project.
type ProjectSpec struct {
	// displayName is human-friendly name for this project
	DisplayName string `json:"displayName,omitempty"`

	// projectId uniquely identifies the project in the provider
	ProjectID string `json:"projectID,omitempty"`

	// projectLead is the administrative contact for a project
	ProjectLead string `json:"projectLead,omitempty"`

	// members is a list of users that are members of this project
	Members []string `json:"members,omitempty"`

	// quotas sets default quotas for this project (see ResourceQuota.spec.hard)
	Quotas map[string]resource.Quantity `json:"quotas,omitempty"`

	// limits sets a default limitrange for this project (see LimitRange.spec.limits)
	Limits []corev1.LimitRangeItem `json:"limits,omitempty"`

	// deleteAction determines what happens to project resources when the Project resource is deleted.
	//
	// deleteAction can have one of the following values:
	//
	// - Ignore -- Do not delete any resources when the Project is deleted.
	// - Preserve -- Delete the Group when the Project is deleted, but preserve the project Namespace and all resources.
	// - Delete -- Delete the Group, the Namespace, and all associated resources.
	// +kubebuilder:default=Preserve
	DeleteAction DeleteAction `json:"deleteAction,omitempty"`
}

// +kubebuilder:validation:Enum=Ignore;Preserve;Delete
type DeleteAction string

const (
	DeleteActionIgnore   DeleteAction = "Ignore"
	DeleteActionPreserve DeleteAction = "Preserve"
	DeleteActionDelete   DeleteAction = "Delete"
)

// ProjectStatus defines the observed state of Project.
type ProjectStatus struct {
	// +kubebuilder:default=Unknown
	Phase Phase `json:"phase"`
}

type Phase string

const (
	PhaseUnknown     Phase = "Unknown"
	PhaseReady       Phase = "Ready"
	PhaseProgressing Phase = "Progressing"
	PhaseFailed      Phase = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".spec.projectID",description="Project ID"
// +kubebuilder:printcolumn:name="Lead",type="string",JSONPath=".spec.projectLead",description="Project lead"
// +kubebuilder:printcolumn:name="Display Name",type="string",JSONPath=".spec.displayName",description="Project display name"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Project phase"

// Project is the Schema for the projects API.
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectSpec   `json:"spec,omitempty"`
	Status ProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProjectList contains a list of Project.
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
