import { describe, expect, it } from "vitest";
import type { Cell, Column } from "./api";
import { formatCell, formatDecimal, formatTimestamp, isNumericColumn } from "./format";

const column = (name: string, type: string): Column => ({ name, type, nullable: true });

describe("formatDecimal", () => {
  it.each([
    [1050, 2, "10.50"],
    [9999, 2, "99.99"],
    [15, 1, "1.5"],
    [5, 2, "0.05"],
    [-1050, 2, "-10.50"],
    [42, 0, "42"],
  ])("scales %i by 10^%i to %s", (value, scale, want) => {
    expect(formatDecimal(value, scale)).toBe(want);
  });
});

describe("formatCell", () => {
  it("renders a DuckDB DECIMAL struct as a number", () => {
    // The column reports TEXT, so the struct is the only signal it is numeric.
    expect(formatCell({ Width: 10, Scale: 2, Value: 1050 }, column("precio", "TEXT"))).toEqual({
      text: "10.50",
      isNull: false,
      isNumeric: true,
    });
  });

  it("marks NULL apart from the literal string", () => {
    expect(formatCell(null, column("x", "NUMBER")).isNull).toBe(true);
    expect(formatCell("NULL", column("x", "TEXT")).isNull).toBe(false);
  });

  it("renders booleans without quoting", () => {
    expect(formatCell(true, column("b", "BOOLEAN")).text).toBe("true");
  });

  it("trims a DATE to its date part", () => {
    expect(formatCell("2024-01-15T00:00:00Z", column("d", "DATE")).text).toBe("2024-01-15");
  });

  it("keeps time and milliseconds on a timestamp", () => {
    expect(formatCell("2026-09-02T20:59:29.616019Z", column("ts", "TIMESTAMP_TZ")).text).toBe(
      "2026-09-02 20:59:29.616",
    );
  });

  it("right-aligns declared numeric types", () => {
    expect(formatCell(7, column("n", "NUMBER")).isNumeric).toBe(true);
    expect(formatCell("Ana", column("s", "TEXT")).isNumeric).toBe(false);
  });
});

describe("formatTimestamp", () => {
  it("passes through anything that is not ISO 8601", () => {
    expect(formatTimestamp("not a date", "DATE")).toBe("not a date");
  });
});

describe("isNumericColumn", () => {
  const rows: Cell[][] = [
    [1, { Width: 10, Scale: 2, Value: 1050 }, "Ana"],
    [2, { Width: 10, Scale: 2, Value: 9999 }, "Luis"],
  ];

  it("trusts a declared numeric type", () => {
    expect(isNumericColumn(rows, 0, column("id", "NUMBER"))).toBe(true);
  });

  it("detects a decimal column the type does not declare", () => {
    expect(isNumericColumn(rows, 1, column("precio", "TEXT"))).toBe(true);
  });

  it("leaves text columns alone", () => {
    expect(isNumericColumn(rows, 2, column("nombre", "TEXT"))).toBe(false);
  });

  it("does not call an all-NULL column numeric", () => {
    expect(isNumericColumn([[null], [null]], 0, column("x", "TEXT"))).toBe(false);
  });
});
