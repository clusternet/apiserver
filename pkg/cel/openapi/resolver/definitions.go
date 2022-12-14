/*
Copyright 2023 The Kubernetes Authors.

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

package resolver

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// DefinitionsSchemaResolver resolves the schema of a built-in type
// by looking up the OpenAPI definitions.
type DefinitionsSchemaResolver struct {
	defs        map[string]common.OpenAPIDefinition
	gvkToSchema map[schema.GroupVersionKind]*spec.Schema
}

// NewDefinitionsSchemaResolver creates a new DefinitionsSchemaResolver.
// An example working setup:
// scheme         = "k8s.io/client-go/kubernetes/scheme".Scheme
// getDefinitions = "k8s.io/kubernetes/pkg/generated/openapi".GetOpenAPIDefinitions
func NewDefinitionsSchemaResolver(scheme *runtime.Scheme, getDefinitions common.GetOpenAPIDefinitions) *DefinitionsSchemaResolver {
	gvkToSchema := make(map[schema.GroupVersionKind]*spec.Schema)
	namer := openapi.NewDefinitionNamer(scheme)
	defs := getDefinitions(func(path string) spec.Ref {
		return spec.MustCreateRef(path)
	})
	for name, def := range defs {
		_, e := namer.GetDefinitionName(name)
		gvks := extensionsToGVKs(e)
		for _, gvk := range gvks {
			gvkToSchema[gvk] = &def.Schema
		}
	}
	return &DefinitionsSchemaResolver{
		gvkToSchema: gvkToSchema,
		defs:        defs,
	}
}

func (d *DefinitionsSchemaResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
	s, ok := d.gvkToSchema[gvk]
	if !ok {
		return nil, fmt.Errorf("cannot resolve %v: %w", gvk, ErrSchemaNotFound)
	}
	result, err := deepCopy(s)
	if err != nil {
		return nil, fmt.Errorf("cannot deep copy schema for %v: %v", gvk, err)
	}
	err = populateRefs(func(ref string) (*spec.Schema, bool) {
		// find the schema by the ref string, and return a deep copy
		def, ok := d.defs[ref]
		if !ok {
			return nil, false
		}
		s, err := deepCopy(&def.Schema)
		if err != nil {
			return nil, false
		}
		return s, true
	}, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// deepCopy generates a deep copy of the given schema with JSON marshalling and
// unmarshalling.
// The schema is expected to be "shallow", with all its field being Refs instead
// of nested schemas.
// If the schema contains cyclic reference, for example, a properties is itself
// it will return an error. This resolver does not support such condition.
func deepCopy(s *spec.Schema) (*spec.Schema, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	result := new(spec.Schema)
	err = json.Unmarshal(b, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func extensionsToGVKs(extensions spec.Extensions) []schema.GroupVersionKind {
	gvksAny, ok := extensions[extGVK]
	if !ok {
		return nil
	}
	gvks, ok := gvksAny.([]any)
	if !ok {
		return nil
	}
	result := make([]schema.GroupVersionKind, 0, len(gvks))
	for _, gvkAny := range gvks {
		// type check the map and all fields
		gvkMap, ok := gvkAny.(map[string]any)
		if !ok {
			return nil
		}
		g, ok := gvkMap["group"].(string)
		if !ok {
			return nil
		}
		v, ok := gvkMap["version"].(string)
		if !ok {
			return nil
		}
		k, ok := gvkMap["kind"].(string)
		if !ok {
			return nil
		}
		result = append(result, schema.GroupVersionKind{
			Group:   g,
			Version: v,
			Kind:    k,
		})
	}
	return result
}
