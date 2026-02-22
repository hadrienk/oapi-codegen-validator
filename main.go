package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

var (
	input  = flag.String("input", "", "Input OpenAPI file path")
	output = flag.String("output", "", "Output enriched OpenAPI file path")
)

const (
	tagKey   = "x-oapi-codegen-extra-tags"
	validate = "validate"
)

func main() {
	flag.Parse()
	if *input == "" || *output == "" {
		flag.Usage()
		os.Exit(1)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromFile(*input)
	if err != nil {
		log.Fatalf("Failed to load OpenAPI spec: %v", err)
	}

	if err := enrichSpec(doc); err != nil {
		log.Fatalf("Enrichment failed: %v", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(doc)
	if err != nil {
		log.Fatalf("Failed to marshal enriched spec: %v", err)
	}

	if err := os.WriteFile(*output, data, 0644); err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}
}

func enrichSpec(doc *openapi3.T) error {
	for name, ref := range doc.Components.Schemas {
		if ref.Value == nil {
			continue
		}
		if err := enrichSchema(name, ref.Value); err != nil {
			return fmt.Errorf("schema %s: %w", name, err)
		}
		// Force inline so that our modifications (Extensions) are persisted in the output YAML
		// instead of just printing the $ref path.
		ref.Ref = ""
	}
	return nil
}

func enrichSchema(name string, schema *openapi3.Schema) error {
	// If this schema has properties, we iterate them to:
	// 1. Calculate tags for the property (min, max, etc)
	// 2. Add 'required' tag if it is in THIS schema's Required list
	// 3. Inject into the property's extensions
	// 4. Recurse

	for propName, propRef := range schema.Properties {
		if propRef.Value == nil {
			continue
		}

		propSchema := propRef.Value
		tags, err := generateTags(propSchema)
		if err != nil {
			return fmt.Errorf("property %s: %w", propName, err)
		}

		// Check if required
		isRequired := false
		for _, reqField := range schema.Required {
			if reqField == propName {
				isRequired = true
				break
			}
		}

		if isRequired {
			tags = append(tags, "required")
		} else {
			// If not required, usually we want "omitempty" for pointers,
			// but for validator, we often want validation ONLY IF provided.
			// "omitempty" is a json tag, not a validate tag (though validator supports it).
			// We'll leave it to the user or default logic.
		}

		// Inject tags if any
		if len(tags) > 0 {
			if err := injectTags(propSchema, tags); err != nil {
				return fmt.Errorf("property %s: %w", propName, err)
			}
		}

		// Recurse
		if err := enrichSchema(fmt.Sprintf("%s.%s", name, propName), propSchema); err != nil {
			return err
		}
	}

	return nil
}

func generateTags(s *openapi3.Schema) ([]string, error) {
	var tags []string

	// 1. UNSUPPORTED KEYWORDS (Fail Fast)
	if s.MultipleOf != nil {
		return nil, fmt.Errorf("validation keyword 'multipleOf' is not supported by auto-enricher; please implement custom validation or add manual tags")
	}
	if s.Pattern != "" {
		return nil, fmt.Errorf("validation keyword 'pattern' is not supported by auto-enricher; please implement custom validation or add manual tags")
	}

	// 2. String Validation
	if s.MinLength > 0 {
		tags = append(tags, fmt.Sprintf("min=%d", s.MinLength))
	}
	if s.MaxLength != nil {
		tags = append(tags, fmt.Sprintf("max=%d", *s.MaxLength))
	}

	// 3. Number Validation (Handling Exclusive Ranges)
	if s.Min != nil {
		op := "min"
		if s.ExclusiveMin {
			op = "gt"
		}
		tags = append(tags, fmt.Sprintf("%s=%.0f", op, *s.Min))
	}
	if s.Max != nil {
		op := "max"
		if s.ExclusiveMax {
			op = "lt"
		}
		tags = append(tags, fmt.Sprintf("%s=%.0f", op, *s.Max))
	}

	// 4. Array Validation
	if s.MinItems > 0 {
		tags = append(tags, fmt.Sprintf("min=%d", s.MinItems))
	}
	if s.MaxItems != nil {
		tags = append(tags, fmt.Sprintf("max=%d", *s.MaxItems))
	}
	if s.UniqueItems {
		tags = append(tags, "unique")
	}

	// 5. Formats
	switch s.Format {
	case "email":
		tags = append(tags, "email")
	case "uuid":
		tags = append(tags, "uuid")
	case "ipv4":
		tags = append(tags, "ipv4")
	case "ipv6":
		tags = append(tags, "ipv6")
	case "uri", "url":
		tags = append(tags, "url")
	}

	return tags, nil
}

func injectTags(s *openapi3.Schema, newTags []string) error {
	if s.Extensions == nil {
		s.Extensions = make(map[string]interface{})
	}

	// Prepare new value
	newTagStr := strings.Join(newTags, ",")

	existingMap, ok := s.Extensions[tagKey].(map[string]interface{})
	if !ok {
		// New map
		existingMap = make(map[string]interface{})
		existingMap[validate] = newTagStr
		s.Extensions[tagKey] = existingMap
		return nil
	}

	// Check for conflict
	if existingVal, exists := existingMap[validate]; exists {
		// MVP: We assume manual tags are intentional overrides or additions.
		// A strict check would parse both lists and look for contradictions.
		// For now, if we see manual tags, we SKIP injection to favor manual control.
		_ = existingVal
		return nil
	}

	// No existing validation tags, inject ours
	existingMap[validate] = newTagStr
	s.Extensions[tagKey] = existingMap
	return nil
}
