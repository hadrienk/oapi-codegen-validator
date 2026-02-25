package main

import (
	"errors"
	"flag"
	"fmt"
	"iter"
	"log"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/hadrienk/oapi-codegen-validator/internal/tree"
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

	data, err := yaml.Marshal(doc)
	if err != nil {
		log.Fatalf("Failed to marshal enriched spec: %v", err)
	}

	if err := os.WriteFile(*output, data, 0644); err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}
}

type SchemaContext struct {
	Schema *openapi3.Schema
	Name   string
}

func toSchemaContext(schemas openapi3.Schemas) iter.Seq[SchemaContext] {
	return func(yield func(SchemaContext) bool) {
		for name, ref := range schemas {
			if ref.Value != nil {
				ref.Ref = "" // Force inline so modifications persist
				if !yield(SchemaContext{Schema: ref.Value, Name: name}) {
					return
				}
			}
		}
	}
}

func getChildren(ctx SchemaContext) iter.Seq[SchemaContext] {
	return func(yield func(SchemaContext) bool) {
		for propName, propRef := range ctx.Schema.Properties {
			if propRef.Value != nil {
				childCtx := SchemaContext{
					Schema: propRef.Value,
					Name:   ctx.Name + "." + propName,
				}
				if !yield(childCtx) {
					return
				}
			}
		}
	}
}

func enrichSpec(doc *openapi3.T) (errs error) {
	for ctx := range tree.PreOrder(toSchemaContext(doc.Components.Schemas), getChildren) {
		if err := enrichNode(ctx); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

func enrichNode(ctx SchemaContext) error {
	// We iterate the properties of the current schema to calculate and inject tags.
	for propName, propRef := range ctx.Schema.Properties {
		if propRef.Value == nil {
			continue
		}

		oapiRules, err := generateRules(propRef.Value)
		if err != nil {
			return fmt.Errorf("property %s.%s: %w", ctx.Name, propName, err)
		}

		validatorRules, extMap := extractAndResetValidateRules(propRef.Value)

		rules, err := mergeRules(validatorRules, oapiRules)
		if err != nil {
			return fmt.Errorf("property %s.%s: %w", ctx.Name, propName, err)
		}

		// NB. Order is important for these tags, so we prepend them after generateTags & injectTags.
		if slices.Contains(ctx.Schema.Required, propName) {
			oapiRules = slices.Insert(rules, 0, "required")
		} else if len(rules) > 0 {
			oapiRules = slices.Insert(rules, 0, "omitempty")
		} else {
			// No rules and not required: nothing useful to emit.
			delete(propRef.Value.Extensions, tagKey)
			continue
		}

		extMap[validate] = strings.Join(oapiRules, ",")
		propRef.Value.Extensions[tagKey] = extMap
	}

	return nil
}

func extractAndResetValidateRules(s *openapi3.Schema) (rules []string, extMap map[string]any) {

	if s.Extensions == nil {
		s.Extensions = make(map[string]any)
	}

	extMap, ok := s.Extensions[tagKey].(map[string]any)
	if !ok {
		extMap = make(map[string]any)
	}
	existingVal, _ := extMap[validate].(string)

	// Reset the validate.
	delete(extMap, validate)

	// Parse the existing rules.
	if existingVal != "" {
		for part := range strings.SplitSeq(existingVal, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			rules = append(rules, part)
		}
	}

	return rules, extMap
}

func generateRules(s *openapi3.Schema) ([]string, error) {
	var tags []string

	if s.MultipleOf != nil {
		return nil, fmt.Errorf("validation keyword 'multipleOf' is not supported by auto-enricher")
	}

	if s.Pattern != "" {
		if _, err := regexp.Compile(s.Pattern); err != nil {
			return nil, fmt.Errorf("validation keyword 'pattern' '%s' is not a valid Go RE2 regex: %w", s.Pattern, err)
		}
		tags = append(tags, fmt.Sprintf("regex=%s", s.Pattern))
	}

	if s.MinLength > 0 {
		tags = append(tags, fmt.Sprintf("min=%d", s.MinLength))
	}

	if s.MaxLength != nil {
		tags = append(tags, fmt.Sprintf("max=%d", *s.MaxLength))
	}

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

	if s.MinItems > 0 {
		tags = append(tags, fmt.Sprintf("min=%d", s.MinItems))
	}
	if s.MaxItems != nil {
		tags = append(tags, fmt.Sprintf("max=%d", *s.MaxItems))
	}
	if s.UniqueItems {
		tags = append(tags, "unique")
	}

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

func mergeRules(existingRules, newRules []string) (rules []string, err error) {
	existingKeys := make(map[string]string)

	for _, part := range existingRules {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		rules = append(rules, part)
		key := getTagKey(part)
		existingKeys[key] = part
	}

	for _, tag := range newRules {
		key := getTagKey(tag)
		if existingTag, exists := existingKeys[key]; exists {
			// Conflict check
			if existingTag != tag {
				return nil, fmt.Errorf("conflict: manual tag '%s' differs from generated tag '%s'", existingTag, tag)
			}
		} else {
			rules = append(rules, tag)
			existingKeys[key] = tag
		}
	}
	return rules, nil
}

func getTagKey(tag string) string {
	if idx := strings.Index(tag, "="); idx != -1 {
		return tag[:idx]
	}
	return tag
}
