package app

type Principal string

const (
	PrincipalMe     Principal = "me"
	PrincipalCodex  Principal = "codex"
	PrincipalSystem Principal = "system"
)

type Attribution struct {
	PerformedBy Principal `json:"performed_by" jsonschema_extras:"enum=me,enum=codex,enum=system"`
	InitiatedBy Principal `json:"initiated_by" jsonschema_extras:"enum=me,enum=codex,enum=system"`
}

func (attribution Attribution) Validate() error {
	switch attribution {
	case Attribution{
		PerformedBy: PrincipalMe,
		InitiatedBy: PrincipalMe,
	}, Attribution{
		PerformedBy: PrincipalCodex,
		InitiatedBy: PrincipalMe,
	}, Attribution{
		PerformedBy: PrincipalCodex,
		InitiatedBy: PrincipalCodex,
	}, Attribution{
		PerformedBy: PrincipalSystem,
		InitiatedBy: PrincipalSystem,
	}:
		return nil
	default:
		return domainError(ErrorValidationFailed, "attribution validation failed")
	}
}
