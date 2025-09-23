package errors

import (
	"fmt"
	"strings"
)

// ProcessingErrors collects multiple errors during processing
type ProcessingErrors struct {
	errors  []error
	context string
}

// NewProcessingErrors creates a new error collection with context
func NewProcessingErrors(context string) *ProcessingErrors {
	return &ProcessingErrors{
		errors:  make([]error, 0),
		context: context,
	}
}

// Add adds an error to the collection
func (pe *ProcessingErrors) Add(err error) {
	if err != nil {
		pe.errors = append(pe.errors, err)
	}
}

// AddWithContext adds an error with additional context
func (pe *ProcessingErrors) AddWithContext(err error, context string) {
	if err != nil {
		pe.errors = append(pe.errors, fmt.Errorf("%s: %w", context, err))
	}
}

// HasErrors returns true if there are any errors
func (pe *ProcessingErrors) HasErrors() bool {
	return len(pe.errors) > 0
}

// Count returns the number of errors
func (pe *ProcessingErrors) Count() int {
	return len(pe.errors)
}

// First returns the first error, or nil if no errors
func (pe *ProcessingErrors) First() error {
	if len(pe.errors) == 0 {
		return nil
	}
	return pe.errors[0]
}

// All returns all errors
func (pe *ProcessingErrors) All() []error {
	return pe.errors
}

// Error implements the error interface
func (pe *ProcessingErrors) Error() string {
	if len(pe.errors) == 0 {
		return ""
	}

	if len(pe.errors) == 1 {
		return fmt.Sprintf("%s: %s", pe.context, pe.errors[0].Error())
	}

	var errorStrings []string
	for i, err := range pe.errors {
		errorStrings = append(errorStrings, fmt.Sprintf("  %d. %s", i+1, err.Error()))
	}

	return fmt.Sprintf("%s (%d errors):\n%s",
		pe.context,
		len(pe.errors),
		strings.Join(errorStrings, "\n"))
}

// ToError returns the error collection as a single error, or nil if no errors
func (pe *ProcessingErrors) ToError() error {
	if !pe.HasErrors() {
		return nil
	}
	return pe
}
