package task

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestErrorsKeepTheirMessage: ErrInvalidInput built by invalidInputf carries
// exactly the text (the 400 body), and the recurring refusal is the spec's
// sentence (story 10's 409).
func TestErrorsKeepTheirMessage(t *testing.T) {
	err := invalidInputf("summary must be at most %d characters", 500)
	assert.Equal(t, "summary must be at most 500 characters", err.Error())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Equal(t, "Repeating tasks cannot be completed here yet. Complete it in your task app.", ErrRecurringNotSupported.Error())
}
