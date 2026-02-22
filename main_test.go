package main

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTags(t *testing.T) {
	tests := []struct {
		name        string
		schema      openapi3.Schema
		expected    []string
		expectError bool
	}{
		{
			name: "String Min Max",
			schema: openapi3.Schema{
				MinLength: 5,
				MaxLength: ptr(uint64(10)),
			},
			expected: []string{"min=5", "max=10"},
		},
		{
			name: "Number Min Max (Inclusive)",
			schema: openapi3.Schema{
				Min: ptr(float64(1)),
				Max: ptr(float64(100)),
			},
			expected: []string{"min=1", "max=100"},
		},
		{
			name: "Number Min Max (Exclusive)",
			schema: openapi3.Schema{
				Min:          ptr(float64(1)),
				ExclusiveMin: true,
				Max:          ptr(float64(100)),
				ExclusiveMax: true,
			},
			expected: []string{"gt=1", "lt=100"},
		},
		{
			name: "Array Validation",
			schema: openapi3.Schema{
				MinItems:    2,
				MaxItems:    ptr(uint64(5)),
				UniqueItems: true,
			},
			expected: []string{"min=2", "max=5", "unique"},
		},
		{
			name: "Format Email",
			schema: openapi3.Schema{
				Format: "email",
			},
			expected: []string{"email"},
		},
		{
			name: "Format UUID",
			schema: openapi3.Schema{
				Format: "uuid",
			},
			expected: []string{"uuid"},
		},
		{
			name: "Format URL",
			schema: openapi3.Schema{
				Format: "uri",
			},
			expected: []string{"url"},
		},
		{
			name: "Unsupported MultipleOf",
			schema: openapi3.Schema{
				MultipleOf: ptr(float64(5)),
			},
			expectError: true,
		},
		{
			name: "Unsupported Pattern",
			schema: openapi3.Schema{
				Pattern: "^[a-z]+$",
			},
			expectError: true,
		},
		{
			name:     "No Validation",
			schema:   openapi3.Schema{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags, err := generateTags(&tt.schema)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expected, tags)
			}
		})
	}
}

func TestEnrichSchema_Required(t *testing.T) {
	// Setup a schema with a required field
	doc := &openapi3.T{
		Components: &openapi3.Components{
			Schemas: make(openapi3.Schemas),
		},
	}

	parentSchema := &openapi3.Schema{
		Type:     &openapi3.Types{"object"},
		Required: []string{"username"},
		Properties: openapi3.Schemas{
			"username": &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"string"},
				},
			},
			"bio": &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"string"},
				},
			},
		},
	}

	doc.Components.Schemas["User"] = &openapi3.SchemaRef{Value: parentSchema}

	// Run Enricher
	err := enrichSpec(doc)
	require.NoError(t, err)

	// Verify 'username' got 'required' tag
	usernameExt := parentSchema.Properties["username"].Value.Extensions
	require.NotNil(t, usernameExt)

	tagsMap, ok := usernameExt[tagKey].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, tagsMap[validate], "required")

	// Verify 'bio' did NOT get 'required' tag
	bioExt := parentSchema.Properties["bio"].Value.Extensions
	if bioExt != nil {
		tagsMap, ok = bioExt[tagKey].(map[string]interface{})
		if ok {
			assert.NotContains(t, tagsMap[validate], "required")
		}
	}
}

func TestEnrichSchema_Conflict(t *testing.T) {
	// Setup schema with MANUAL tags
	parentSchema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"code": &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type:      &openapi3.Types{"string"},
					MinLength: 5, // Should generate min=5
					Extensions: map[string]interface{}{
						tagKey: map[string]interface{}{
							validate: "manual_tag",
						},
					},
				},
			},
		},
	}

	doc := &openapi3.T{
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				"Code": &openapi3.SchemaRef{Value: parentSchema},
			},
		},
	}

	err := enrichSpec(doc)
	require.NoError(t, err)

	// Verify manual tag was PRESERVED and NOT overwritten
	codeExt := parentSchema.Properties["code"].Value.Extensions
	tagsMap := codeExt[tagKey].(map[string]interface{})
	assert.Equal(t, "manual_tag", tagsMap[validate])
	assert.NotContains(t, tagsMap[validate], "min=5")
}

func ptr[T any](v T) *T {
	return &v
}
