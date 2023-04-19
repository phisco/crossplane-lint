package schema

import (
	"github.com/crossplane-contrib/crossplane-lint/internal/xpkg"
	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sync"
)

const (
	errBuildCompositeCRD = "failed to build composite CRD"
	errBuildClaimCRD     = "failed to build claim CRD"
)

type SchemaStore struct {
	crds map[schema.GroupKind]apiextensions.CustomResourceDefinition
	mu   *sync.RWMutex
}

func NewSchemaStore() *SchemaStore {
	return &SchemaStore{
		crds: map[schema.GroupKind]apiextensions.CustomResourceDefinition{},
		mu:   &sync.RWMutex{},
	}
}

func (s *SchemaStore) RegisterPackage(pkg *xpkg.Package) error {
	for _, e := range pkg.Entries {
		switch {
		case e.IsCRD():
			crd, err := e.AsCRD()
			if err != nil {
				return err
			}
			if err := s.registerCRD(crd); err != nil {
				return err
			}
		case e.IsXRD():
			xrd, err := e.AsXRD()
			if err != nil {
				return err
			}
			comp, err := ForCompositeResource(xrd)
			if err != nil {
				return errors.Wrap(err, errBuildCompositeCRD)
			}
			if err := s.registerCRD(comp); err != nil {
				return err
			}
			if xrd.Spec.ClaimNames != nil {
				claim, err := ForCompositeResourceClaim(xrd)
				if err != nil {
					return errors.Wrap(err, errBuildClaimCRD)
				}
				if err := s.registerCRD(claim); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *SchemaStore) registerCRD(crd *extv1.CustomResourceDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	gk := schema.GroupKind{
		Group: crd.Spec.Group,
		Kind:  crd.Spec.Names.Kind,
	}

	extCRD := apiextensions.CustomResourceDefinition{}
	if err := extv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(crd, &extCRD, nil); err != nil {
		return err
	}

	s.crds[gk] = extCRD
	return nil
}

func addMetaDataToSchema(crdv *apiextensions.CustomResourceValidation) {
	additionalMetaProps := map[string]apiextensions.JSONSchemaProps{
		"name": {
			Type: "string",
		},
		"namespace": {
			Type: "string",
		},
		"labels": {
			Type: "object",
			AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
				Allows: true,
				Schema: &apiextensions.JSONSchemaProps{
					Type: "string",
				},
			},
		},
		"annotations": {
			Type: "object",
			AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
				Allows: true,
				Schema: &apiextensions.JSONSchemaProps{
					Type: "string",
				},
			},
		},
		"uid": {
			Type: "string",
		},
	}
	if _, exists := crdv.OpenAPIV3Schema.Properties["metadata"]; !exists {
		crdv.OpenAPIV3Schema.Properties["metadata"] = apiextensions.JSONSchemaProps{
			Type:       "object",
			Properties: map[string]apiextensions.JSONSchemaProps{},
		}
	}
	if crdv.OpenAPIV3Schema.Properties["metadata"].Properties == nil {
		prop := crdv.OpenAPIV3Schema.Properties["metadata"]
		prop.Properties = map[string]apiextensions.JSONSchemaProps{}
		crdv.OpenAPIV3Schema.Properties["metadata"] = prop
	}
	for name, prop := range additionalMetaProps {
		if _, exists := crdv.OpenAPIV3Schema.Properties["metadata"].Properties[name]; !exists {
			props := crdv.OpenAPIV3Schema.Properties["metadata"]
			props.Properties[name] = prop
			crdv.OpenAPIV3Schema.Properties["metadata"] = props
		}
	}
}

func (s *SchemaStore) GetCRDSchemaValidation(gvk schema.GroupVersionKind) *apiextensions.CustomResourceValidation {
	crd := s.GetCRDSchema(gvk.GroupKind())
	if crd == nil {
		return nil
	}
	if crd.Spec.Validation != nil {
		addMetaDataToSchema(crd.Spec.Validation)
		return crd.Spec.Validation
	}
	for _, v := range crd.Spec.Versions {
		if v.Name == gvk.Version {
			addMetaDataToSchema(v.Schema)
			return v.Schema
		}
	}

	return nil
}

func (s *SchemaStore) GetCRDSchema(gk schema.GroupKind) *apiextensions.CustomResourceDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	crd, exists := s.crds[gk]
	if !exists {
		return nil
	}
	return crd.DeepCopy()
}

func (s *SchemaStore) GetAll() map[schema.GroupKind]apiextensions.CustomResourceDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[schema.GroupKind]apiextensions.CustomResourceDefinition, len(s.crds))
	for k, v := range s.crds {
		out[k] = *v.DeepCopy()
	}
	return out
}
