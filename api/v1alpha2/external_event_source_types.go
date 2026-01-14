package v1alpha2

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ExternalEventSourceSpec defines connection details for an external gRPC provider
type ExternalEventSourceSpec struct {
	Endpoint   ExternalEndpoint    `json:"endpoint"`
	Config     *ExternalConfig     `json:"config,omitempty"`
	Connection *ExternalConnPolicy `json:"connection,omitempty"`
}

// ExternalEndpoint contains address, TLS, and auth configuration
type ExternalEndpoint struct {
	Address string        `json:"address"`
	TLS     *ExternalTLS  `json:"tls,omitempty"`
	Auth    *ExternalAuth `json:"auth,omitempty"`
}

// ExternalTLS holds TLS/mTLS settings, referencing a single Secret with CA, cert, and key
type ExternalTLS struct {
	Enabled   bool                  `json:"enabled"`
	SecretRef *ExternalTLSSecretRef `json:"secretRef,omitempty"`
}

// ExternalTLSSecretRef points to a Secret that contains CA, client cert and key
type ExternalTLSSecretRef struct {
	Name    string `json:"name"`
	CAKey   string `json:"caKey"`
	CertKey string `json:"certKey"`
	KeyKey  string `json:"keyKey"`
}

// ExternalAuth defines optional per-connection auth
type ExternalAuth struct {
	Type      string             `json:"type"` // BearerToken|Basic
	SecretRef *SecretKeySelector `json:"secretRef,omitempty"`
}

// SecretKeySelector references a key in a Secret
type SecretKeySelector struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ExternalConfig is an opaque JSON string passed-through to the provider
type ExternalConfig struct {
	JSON string `json:"json"`
}

// ExternalConnPolicy defines connection behavior
type ExternalConnPolicy struct {
	NamespaceScope   bool              `json:"namespaceScope,omitempty"`
	ReconnectBackoff *ReconnectBackoff `json:"reconnectBackoff,omitempty"`
}

type ReconnectBackoff struct {
	InitialSeconds int32 `json:"initialSeconds,omitempty"`
	MaxSeconds     int32 `json:"maxSeconds,omitempty"`
}

// ExternalEventSourceStatus captures readiness and connection info
type ExternalEventSourceStatus struct {
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
	LastConnectedTime *metav1.Time       `json:"lastConnectedTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ExternalEventSource configures an external gRPC-based event source
type ExternalEventSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExternalEventSourceSpec   `json:"spec,omitempty"`
	Status ExternalEventSourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExternalEventSourceList contains a list of ExternalEventSource
type ExternalEventSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalEventSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ExternalEventSource{}, &ExternalEventSourceList{})
}

// ValidateCreate implements webhook.Validator
func (r *ExternalEventSource) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	res, ok := obj.(*ExternalEventSource)
	if !ok {
		return nil, fmt.Errorf("expected ExternalEventSource, got %T", obj)
	}
	return validateExternalEventSource(res)
}

// ValidateUpdate implements webhook.Validator
func (r *ExternalEventSource) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	res, ok := newObj.(*ExternalEventSource)
	if !ok {
		return nil, fmt.Errorf("expected ExternalEventSource, got %T", newObj)
	}
	return validateExternalEventSource(res)
}

// ValidateDelete implements webhook.Validator
func (r *ExternalEventSource) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateExternalEventSource(res *ExternalEventSource) (admission.Warnings, error) {
	var allErrs []string
	var warnings admission.Warnings

	// Address validation
	addr := strings.TrimSpace(res.Spec.Endpoint.Address)
	if addr == "" {
		allErrs = append(allErrs, "spec.endpoint.address cannot be empty")
	}

	// TLS validation
	if res.Spec.Endpoint.TLS != nil {
		if res.Spec.Endpoint.TLS.Enabled {
			if res.Spec.Endpoint.TLS.SecretRef == nil {
				allErrs = append(allErrs, "spec.endpoint.tls.secretRef must be set when tls.enabled is true")
			} else {
				s := res.Spec.Endpoint.TLS.SecretRef
				if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.CAKey) == "" || strings.TrimSpace(s.CertKey) == "" || strings.TrimSpace(s.KeyKey) == "" {
					allErrs = append(allErrs, "spec.endpoint.tls.secretRef.name, caKey, certKey, and keyKey must be set")
				}
			}
		}
	}

	// Auth validation
	if res.Spec.Endpoint.Auth != nil {
		t := strings.TrimSpace(res.Spec.Endpoint.Auth.Type)
		if t != "" && t != "BearerToken" && t != "Basic" {
			allErrs = append(allErrs, "spec.endpoint.auth.type must be BearerToken or Basic if set")
		}
		if res.Spec.Endpoint.Auth.SecretRef != nil {
			if strings.TrimSpace(res.Spec.Endpoint.Auth.SecretRef.Name) == "" || strings.TrimSpace(res.Spec.Endpoint.Auth.SecretRef.Key) == "" {
				allErrs = append(allErrs, "spec.endpoint.auth.secretRef.name and key must be set when auth.secretRef is provided")
			}
		}
	}

	if len(allErrs) > 0 {
		return warnings, fmt.Errorf("validation failed: %s", strings.Join(allErrs, "; "))
	}
	return warnings, nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ExternalEventSource) DeepCopyInto(out *ExternalEventSource) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	// Spec: shallow copy is acceptable for pointer fields here
	out.Spec = in.Spec
	// Status: deep copy slice and time pointer
	if in.Status.Conditions != nil {
		out.Status.Conditions = make([]metav1.Condition, len(in.Status.Conditions))
		copy(out.Status.Conditions, in.Status.Conditions)
	}
	if in.Status.LastConnectedTime != nil {
		t := *in.Status.LastConnectedTime
		out.Status.LastConnectedTime = &t
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ExternalEventSource.
func (in *ExternalEventSource) DeepCopy() *ExternalEventSource {
	if in == nil {
		return nil
	}
	out := new(ExternalEventSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *ExternalEventSource) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ExternalEventSourceList) DeepCopyInto(out *ExternalEventSourceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ExternalEventSource, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ExternalEventSourceList.
func (in *ExternalEventSourceList) DeepCopy() *ExternalEventSourceList {
	if in == nil {
		return nil
	}
	out := new(ExternalEventSourceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *ExternalEventSourceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
