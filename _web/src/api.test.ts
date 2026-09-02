import { describe, expect, it } from "vitest";
import { isDecimalValue, runStatement, StatementError } from "./api";

const CONTEXT = { database: "TEST_DB", schema: "PUBLIC" };

function respondWith(body: unknown, ok = true, status = 200): typeof fetch {
  return (async () => ({ ok, status, json: async () => body })) as unknown as typeof fetch;
}

describe("runStatement", () => {
  it("returns columns and rows from a successful statement", async () => {
    const result = await runStatement(
      "SELECT 1",
      CONTEXT,
      respondWith({
        resultSetMetaData: {
          numRows: 1,
          format: "jsonv2",
          rowType: [{ name: "n", type: "NUMBER", nullable: true }],
        },
        data: [[1]],
        code: "090001",
        sqlState: "00000",
        statementHandle: "abc",
      }),
    );

    expect(result.columns).toEqual([{ name: "n", type: "NUMBER", nullable: true }]);
    expect(result.rows).toEqual([[1]]);
    expect(result.handle).toBe("abc");
    expect(result.rowsAffected).toBeNull();
  });

  it("lifts the row count out of a DML response", async () => {
    // DDL and DML come back as a one-column result set, not a real grid.
    const result = await runStatement(
      "INSERT INTO t VALUES (1)",
      CONTEXT,
      respondWith({
        resultSetMetaData: {
          numRows: 2,
          format: "jsonv2",
          rowType: [{ name: "number of rows affected", type: "FIXED", nullable: false }],
        },
        data: [[2]],
        sqlState: "00000",
      }),
    );

    expect(result.rowsAffected).toBe(2);
  });

  it("throws on a failed statement even though the response is HTTP 200", async () => {
    // The emulator reports failures in the body, so the status says nothing.
    const failing = respondWith({
      code: "001007",
      sqlState: "42000",
      message: "Catalog Error: Table with name X does not exist!",
      statementHandle: "def",
    });

    await expect(runStatement("SELECT * FROM x", CONTEXT, failing)).rejects.toThrow(StatementError);

    await runStatement("SELECT * FROM x", CONTEXT, failing).catch((error: StatementError) => {
      expect(error.code).toBe("001007");
      expect(error.sqlState).toBe("42000");
      expect(error.handle).toBe("def");
    });
  });

  it("reports an HTTP failure that carries no sqlState", async () => {
    await expect(
      runStatement("SELECT 1", CONTEXT, respondWith({ message: "boom" }, false, 500)),
    ).rejects.toThrow("boom");
  });

  it("tolerates a response with no result set", async () => {
    const result = await runStatement("BEGIN", CONTEXT, respondWith({ sqlState: "00000" }));

    expect(result.columns).toEqual([]);
    expect(result.rows).toEqual([]);
  });
});

describe("isDecimalValue", () => {
  it("recognises the DuckDB DECIMAL struct", () => {
    expect(isDecimalValue({ Width: 10, Scale: 2, Value: 1050 })).toBe(true);
  });

  it.each([null, 5, "text", {}, { Value: 1 }])("rejects %o", (value) => {
    expect(isDecimalValue(value)).toBe(false);
  });
});
