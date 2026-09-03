import { describe, expect, it, vi } from "vitest";
import { exportFilename, jsonKeys, toCSV, toJSON } from "./export";
import type { Statement } from "./api";

function result(columns: { name: string; type: string }[], rows: unknown[][]): Statement {
  return {
    columns: columns.map((column) => ({ ...column, nullable: true })),
    rows: rows as Statement["rows"],
    rowsAffected: null,
    totalRows: rows.length,
    elapsedMs: 1,
  } as Statement;
}

const lines = (csv: string) => csv.split("\r\n");

describe("toCSV", () => {
  it("writes a header row of column names", () => {
    const csv = toCSV(result([{ name: "ID", type: "FIXED" }], [[1]]));
    expect(lines(csv)).toEqual(["ID", "1"]);
  });

  it("separates rows with CRLF, as RFC 4180 and Excel expect", () => {
    const csv = toCSV(result([{ name: "ID", type: "FIXED" }], [[1], [2]]));
    expect(csv).toBe("ID\r\n1\r\n2");
  });

  it("leaves NULL as an empty field rather than the word", () => {
    const csv = toCSV(result([{ name: "NAME", type: "TEXT" }], [[null]]));
    expect(lines(csv)[1]).toBe("");
  });

  it("quotes a value containing the delimiter", () => {
    const csv = toCSV(result([{ name: "NAME", type: "TEXT" }], [["Smith, John"]]));
    expect(lines(csv)[1]).toBe('"Smith, John"');
  });

  it("doubles an embedded quote", () => {
    const csv = toCSV(result([{ name: "NAME", type: "TEXT" }], [['say "hi"']]));
    expect(lines(csv)[1]).toBe('"say ""hi"""');
  });

  it("quotes a value containing a newline", () => {
    const csv = toCSV(result([{ name: "NOTE", type: "TEXT" }], [["one\ntwo"]]));
    expect(csv).toBe('NOTE\r\n"one\ntwo"');
  });

  it("quotes a value a spreadsheet would read as a formula", () => {
    const csv = toCSV(result([{ name: "NAME", type: "TEXT" }], [["=1+1"]]));
    expect(lines(csv)[1]).toBe('"=1+1"');
  });

  it("unpacks a DECIMAL rather than writing the struct", () => {
    const csv = toCSV(
      result([{ name: "AMOUNT", type: "TEXT" }], [[{ Width: 10, Scale: 2, Value: 1234 }]]),
    );
    expect(lines(csv)[1]).toBe("12.34");
  });

  it("trims a DATE to the day, as the grid shows it", () => {
    const csv = toCSV(result([{ name: "DAY", type: "DATE" }], [["2026-09-02T00:00:00Z"]]));
    expect(lines(csv)[1]).toBe("2026-09-02");
  });

  it("writes only a header for an empty result", () => {
    expect(toCSV(result([{ name: "ID", type: "FIXED" }], []))).toBe("ID");
  });
});

describe("toJSON", () => {
  it("writes one object per row, keyed by column name", () => {
    const json = toJSON(
      result([{ name: "ID", type: "FIXED" }, { name: "NAME", type: "TEXT" }], [[1, "Ada"]]),
    );
    expect(JSON.parse(json)).toEqual([{ ID: 1, NAME: "Ada" }]);
  });

  it("keeps NULL as null", () => {
    expect(JSON.parse(toJSON(result([{ name: "NAME", type: "TEXT" }], [[null]])))).toEqual([
      { NAME: null },
    ]);
  });

  it("keeps booleans as booleans", () => {
    expect(JSON.parse(toJSON(result([{ name: "OK", type: "BOOLEAN" }], [[true]])))).toEqual([
      { OK: true },
    ]);
  });

  it("keeps a DECIMAL as a string, since it can outrun a double", () => {
    const json = toJSON(
      result(
        [{ name: "AMOUNT", type: "TEXT" }],
        [[{ Width: 38, Scale: 2, Value: 1234 }]],
      ),
    );
    expect(JSON.parse(json)).toEqual([{ AMOUNT: "12.34" }]);
  });

  it("writes an empty array for an empty result", () => {
    expect(JSON.parse(toJSON(result([{ name: "ID", type: "FIXED" }], [])))).toEqual([]);
  });
});

describe("jsonKeys", () => {
  it("keeps distinct names as they are", () => {
    expect(jsonKeys([{ name: "A" }, { name: "B" }] as never)).toEqual(["A", "B"]);
  });

  it("disambiguates a repeated name so no column is lost", () => {
    expect(jsonKeys([{ name: "X" }, { name: "X" }, { name: "X" }] as never)).toEqual([
      "X",
      "X_2",
      "X_3",
    ]);
  });

  it("keeps every column of a result with duplicate names", () => {
    const json = toJSON(
      result([{ name: "X", type: "FIXED" }, { name: "X", type: "FIXED" }], [[1, 2]]),
    );
    expect(JSON.parse(json)).toEqual([{ X: 1, X_2: 2 }]);
  });
});

describe("exportFilename", () => {
  it("stamps the name to the second so repeated exports do not collide", () => {
    const at = new Date(2026, 8, 2, 7, 5, 9);
    expect(exportFilename("csv", at)).toBe("results-20260902-070509.csv");
  });

  it("uses the extension it is given", () => {
    expect(exportFilename("json", new Date(2026, 0, 1, 0, 0, 0))).toBe(
      "results-20260101-000000.json",
    );
  });

  it("stamps from the clock when no time is given", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 11, 25, 13, 30, 0));
    expect(exportFilename("csv")).toBe("results-20261225-133000.csv");
    vi.useRealTimers();
  });
});
