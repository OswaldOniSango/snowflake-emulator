import { describe, expect, it } from "vitest";
import { humanise, emptyDetail } from "./history";

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

describe("emptyDetail", () => {
  it("says only how long statements are kept when they survive a restart", () => {
    const detail = emptyDetail("168h0m0s", true);
    expect(detail).toContain("keeps statements for");
    expect(detail).not.toContain("restart");
  });

  it("explains the loss, and the fix, when they do not", () => {
    const detail = emptyDetail("168h0m0s", false);
    expect(detail).toContain("forgets them on restart");
    expect(detail).toContain("DB_PATH");
  });
});

describe("humanise over longer retentions", () => {
  it("says a week in days, not 168 hours", () => {
    expect(humanise("168h0m0s")).toBe("7 days");
  });

  it("keeps the remaining hours", () => {
    expect(humanise("50h0m0s")).toBe("2 days 2 hours");
  });

  it("singularises one trailing hour", () => {
    expect(humanise("49h0m0s")).toBe("2 days 1 hour");
  });

  it("still counts a day and a half in hours", () => {
    expect(humanise("36h0m0s")).toBe("36 hours");
  });

  it("leaves an hour alone", () => {
    expect(humanise("1h0m0s")).toBe("1 hour");
  });
});
