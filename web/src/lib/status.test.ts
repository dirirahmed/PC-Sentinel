import { describe, expect, it } from "vitest";
import { parseRoute } from "../hooks/useHashRoute";
import { gradeTone, severityTone, spaceTone } from "./status";

describe("status tones", () => {
  it("maps health grades onto the status palette", () => {
    expect(gradeTone("good")).toBe("good");
    expect(gradeTone("fair")).toBe("warning");
    expect(gradeTone("poor")).toBe("serious");
    expect(gradeTone("critical")).toBe("critical");
    expect(gradeTone("unavailable")).toBe("muted");
  });
  it("maps alert severity and disk space", () => {
    expect(severityTone("info")).toBe("info");
    expect(severityTone("critical")).toBe("critical");
    expect(spaceTone("warning")).toBe("warning");
    expect(spaceTone("ok")).toBe("good");
  });
});

describe("parseRoute", () => {
  it("parses hash routes and falls back to the dashboard", () => {
    expect(parseRoute("#/processes")).toBe("processes");
    expect(parseRoute("#alerts")).toBe("alerts");
    expect(parseRoute("")).toBe("dashboard");
    expect(parseRoute("#/nope")).toBe("dashboard");
  });
});
