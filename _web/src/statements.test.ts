import { describe, expect, it } from "vitest";
import { splitStatements, statementAt } from "./statements";

const texts = (buffer: string): string[] => splitStatements(buffer).map((s) => s.text);

describe("splitStatements", () => {
  it("splits on semicolons", () => {
    expect(texts("SELECT 1; SELECT 2")).toEqual(["SELECT 1", "SELECT 2"]);
  });

  it("drops a trailing semicolon and empty statements", () => {
    expect(texts("SELECT 1;")).toEqual(["SELECT 1"]);
    expect(texts("SELECT 1;;\n\n;")).toEqual(["SELECT 1"]);
  });

  it("returns a single statement when there is no semicolon", () => {
    expect(texts("SELECT 1")).toEqual(["SELECT 1"]);
  });

  it("returns nothing for an empty or blank buffer", () => {
    expect(texts("")).toEqual([]);
    expect(texts("   \n\t ")).toEqual([]);
  });

  it("ignores a semicolon inside a string literal", () => {
    expect(texts("SELECT 'a; b'; SELECT 2")).toEqual(["SELECT 'a; b'", "SELECT 2"]);
  });

  it("handles a doubled quote inside a literal", () => {
    expect(texts("SELECT 'it''s; fine'; SELECT 2")).toEqual(["SELECT 'it''s; fine'", "SELECT 2"]);
  });

  it("ignores a semicolon inside a quoted identifier", () => {
    expect(texts(`SELECT "a; b"; SELECT 2`)).toEqual([`SELECT "a; b"`, "SELECT 2"]);
  });

  it("ignores a semicolon inside a line comment", () => {
    expect(texts("SELECT 1 -- ; not a split\n; SELECT 2")).toEqual([
      "SELECT 1 -- ; not a split",
      "SELECT 2",
    ]);
  });

  it("ignores a semicolon inside a block comment", () => {
    expect(texts("SELECT /* ; */ 1; SELECT 2")).toEqual(["SELECT /* ; */ 1", "SELECT 2"]);
  });

  it("handles nested block comments", () => {
    expect(texts("SELECT /* a /* ; */ b */ 1; SELECT 2")).toEqual([
      "SELECT /* a /* ; */ b */ 1",
      "SELECT 2",
    ]);
  });

  it("keeps a procedure body whole", () => {
    // The case that makes splitting on ";" wrong: the body is full of them.
    const buffer = `CREATE PROCEDURE p() RETURNS VARCHAR LANGUAGE SQL AS $$
DECLARE
    n INTEGER DEFAULT 0;
BEGIN
    n := 1;
    RETURN n;
END
$$;
SELECT 1`;

    const statements = texts(buffer);
    expect(statements).toHaveLength(2);
    expect(statements[0]).toContain("DECLARE");
    expect(statements[0]).toContain("RETURN n;");
    expect(statements[0]?.endsWith("$$")).toBe(true);
    expect(statements[1]).toBe("SELECT 1");
  });

  it("tolerates an unterminated literal", () => {
    expect(texts("SELECT 'unterminated; SELECT 2")).toEqual(["SELECT 'unterminated; SELECT 2"]);
  });

  it("tolerates an unterminated procedure body", () => {
    expect(texts("CREATE PROCEDURE p() AS $$ BEGIN; SELECT 1")).toEqual([
      "CREATE PROCEDURE p() AS $$ BEGIN; SELECT 1",
    ]);
  });

  it("records offsets that point at the statement text", () => {
    const buffer = "  SELECT 1;\n  SELECT 2  ";
    const statements = splitStatements(buffer);

    for (const statement of statements) {
      expect(buffer.slice(statement.start, statement.end)).toBe(statement.text);
    }
  });
});

describe("statementAt", () => {
  const buffer = "SELECT 1;\nSELECT 2;\nSELECT 3";

  it("finds the statement the cursor is inside", () => {
    expect(statementAt(buffer, 3)?.text).toBe("SELECT 1");
    expect(statementAt(buffer, 13)?.text).toBe("SELECT 2");
    expect(statementAt(buffer, 25)?.text).toBe("SELECT 3");
  });

  it("finds it at either boundary", () => {
    expect(statementAt(buffer, 0)?.text).toBe("SELECT 1");
    expect(statementAt(buffer, 8)?.text).toBe("SELECT 1");
  });

  it("falls back to the statement before, where the cursor lands after a semicolon", () => {
    // Offset 9 is the semicolon's own position, which belongs to no statement.
    expect(statementAt(buffer, 9)?.text).toBe("SELECT 1");
  });

  it("returns the last statement for an offset past the end", () => {
    expect(statementAt(buffer, 999)?.text).toBe("SELECT 3");
  });

  it("returns null for a buffer with nothing to run", () => {
    expect(statementAt("", 0)).toBeNull();
    expect(statementAt("  \n ", 2)).toBeNull();
  });
});
