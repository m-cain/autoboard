package mcpapi

import (
	"strings"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestRepairHintsCoverEveryDomainFailure(t *testing.T) {
	for _, test := range []struct {
		kind app.ErrorKind
		want string
	}{
		{kind: app.ErrorRevisionConflict, want: "latest entity"},
		{kind: app.ErrorValidationFailed, want: "listed fields"},
		{kind: app.ErrorNotFound, want: "current ID"},
		{kind: app.ErrorInvalidTransition, want: "valid next status"},
		{kind: app.ErrorBlockedByDependency, want: "blocking tickets"},
		{kind: app.ErrorDependencyCycle, want: "dependency direction"},
		{kind: app.ErrorAttachmentFailed, want: "source path"},
		{kind: app.ErrorUnauthorized, want: "authorized"},
		{kind: app.ErrorKind("unexpected"), want: "Read the current state"},
	} {
		if hint := repairHint(test.kind); !strings.Contains(hint, test.want) {
			t.Errorf("repairHint(%q) = %q, want %q", test.kind, hint, test.want)
		}
	}
}
