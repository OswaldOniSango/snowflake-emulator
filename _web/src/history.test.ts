import { describe, expect, it } from "vitest";
import { humanise } from "./history";

describe("humanise", () => {
  it.each([
    ["1h0m0s", "1 hour"],
    ["2h0m0s", "2 hours"],
    ["1h30m0s", "1 hour 30 minutes"],
    ["30m0s", "30 minutes"],
    ["45s", "45 seconds"],
    ["1.5s", "1.5 seconds"],
  ])("reads %s as %s", (duration, want) => {
    expect(humanise(duration)).toBe(want);
  });

  it("passes through anything it does not recognise", () => {
    // Better to show the raw duration than to invent a wrong one.
    expect(humanise("forever")).toBe("forever");
    expect(humanise("")).toBe("");
  });

  it("keeps a zero duration rather than saying nothing", () => {
    expect(humanise("0s")).toBe("0s");
  });
});
