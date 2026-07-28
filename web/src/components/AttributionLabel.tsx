import type { TicketSummary } from "@autoboard/contracts";

type Attribution = TicketSummary["created_attribution"];

const labels = {
  "me:me": "me",
  "codex:me": "me via Codex",
  "codex:codex": "Codex",
  "system:system": "system",
} as const;

export const AttributionLabel = ({
  attribution,
}: {
  readonly attribution: Attribution;
}) => {
  const key =
    `${attribution.performed_by}:${attribution.initiated_by}` as keyof typeof labels;
  const label = labels[key];
  return (
    <span className="attribution-label" aria-label={`By ${label}`}>
      {label}
    </span>
  );
};
