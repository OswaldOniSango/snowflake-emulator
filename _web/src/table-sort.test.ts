import { describe, expect, it } from "vitest";
import { compareValues } from "./table-sort";
describe("numeric ordering without loss of precision", () => {
  it.each([
    ["2", "10", -1], ["9007199254740992", "9007199254740993", -1],
    ["-10", "-2", -1], ["1.20", "1.2", 0], ["0", "-0.00", 0],
    ["1e3", "999", 1], ["0.0002", "0.001", -1], ["-1e2", "-99", -1],
  ])("compares %s and %s", (a, b, sign) => { expect(Math.sign(compareValues(a, b, true))).toBe(sign); });
  it("keeps null distinct from a literal null string", () => {
    expect(compareValues(null, "null")).toBeGreaterThan(0);
    expect(compareValues(null, null)).toBe(0);
  });
});
