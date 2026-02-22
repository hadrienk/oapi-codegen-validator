package middleware

import (
	"context"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

// StrictHandlerFunc matches the signature of the generated strict handler.
type StrictHandlerFunc func(ctx context.Context, w http.ResponseWriter, r *http.Request, args any) (any, error)

// StrictMiddlewareFunc matches the signature of the generated strict middleware.
type StrictMiddlewareFunc func(f StrictHandlerFunc, operationID string) StrictHandlerFunc

// ErrorHandler handles validation errors.
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

type options struct {
	validator    *validator.Validate
	errorHandler ErrorHandler
}

type Option func(*options)

// WithValidator sets a custom validator instance.
func WithValidator(v *validator.Validate) Option {
	return func(o *options) {
		o.validator = v
	}
}

// WithErrorHandler sets the error handler.
func WithErrorHandler(h ErrorHandler) Option {
	return func(o *options) {
		o.errorHandler = h
	}
}

// New creates a new strict middleware that validates the request body.
func New(opts ...Option) StrictMiddlewareFunc {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	if o.validator == nil {
		o.validator = validator.New()
	}

	// Get the name from the json tag.
	o.validator.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	// Custom validator for regexp
	_ = o.validator.RegisterValidation("regex", func(fl validator.FieldLevel) bool {
		pattern := fl.Param()
		value := fl.Field().String()
		match, err := regexp.MatchString(pattern, value)
		if err != nil {
			return false
		}
		return match
	})

	if o.errorHandler == nil {
		o.errorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "Validation failed", http.StatusBadRequest)
		}
	}

	return func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, args any) (any, error) {
			val := reflect.ValueOf(args)
			if val.Kind() == reflect.Struct {
				bodyField := val.FieldByName("Body")
				if bodyField.IsValid() && !bodyField.IsZero() {
					if err := o.validator.Struct(bodyField.Interface()); err != nil {
						o.errorHandler(w, r, err)
						return nil, nil
					}
				}
			}

			return f(ctx, w, r, args)
		}
	}
}
