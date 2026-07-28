package app_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestAttributionValidateAcceptsOnlyPermittedPrincipalPairs(t *testing.T) {
	principals := []app.Principal{
		app.PrincipalMe,
		app.PrincipalCodex,
		app.PrincipalSystem,
		"",
		"unknown",
	}
	validPairs := map[string]bool{
		"me/me":         true,
		"codex/me":      true,
		"codex/codex":   true,
		"system/system": true,
	}

	for _, performedBy := range principals {
		for _, initiatedBy := range principals {
			attribution := app.Attribution{
				PerformedBy: performedBy,
				InitiatedBy: initiatedBy,
			}
			name := fmt.Sprintf("%s/%s", performedBy, initiatedBy)

			t.Run(name, func(t *testing.T) {
				err := attribution.Validate()
				if validPairs[name] {
					if err != nil {
						t.Fatalf("Validate() error = %v, want nil", err)
					}
					return
				}

				var domainErr *app.Error
				if !errors.As(err, &domainErr) ||
					domainErr.Kind != app.ErrorValidationFailed {
					t.Fatalf(
						"Validate() error = %v, want validation-domain error",
						err,
					)
				}
			})
		}
	}
}
