import { describe, expect, it } from "vitest";
import { LIMITATIONS } from "./limitations.generated";

/**
 * The list is generated from the README, so these guard the generator rather
 * than the content: if the README's Limitations section moves or empties, the
 * console would quietly claim the emulator has no gaps.
 */
describe("LIMITATIONS", () => {
  it("is not empty", () => {
    expect(LIMITATIONS.length).toBeGreaterThan(0);
  });

  it("carries no markdown", () => {
    for (const limitation of LIMITATIONS) {
      expect(limitation).not.toContain("`");
      expect(limitation).not.toMatch(/^[-*]\s/);
    }
  });

  it("has no blank entries", () => {
    for (const limitation of LIMITATIONS) {
      expect(limitation.trim()).not.toBe("");
    }
  });

  it("includes the gaps the README documents", () => {
    const all = LIMITATIONS.join(" ");
    expect(all).toContain("External stages");
    expect(all).toContain("Time Travel");
  });
});
