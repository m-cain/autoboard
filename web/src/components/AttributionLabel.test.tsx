// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AttributionLabel } from "./AttributionLabel.js";

afterEach(cleanup);

describe("AttributionLabel", () => {
  it.each([
    [{ performed_by: "me", initiated_by: "me" }, "me"],
    [{ performed_by: "codex", initiated_by: "me" }, "me via Codex"],
    [{ performed_by: "codex", initiated_by: "codex" }, "Codex"],
    [{ performed_by: "system", initiated_by: "system" }, "system"],
  ] as const)("identifies %o as %s", (attribution, label) => {
    render(<AttributionLabel attribution={attribution} />);

    expect(screen.getByText(label)).toHaveAccessibleName(`By ${label}`);
  });
});
