import { describe, expect, it } from "vitest";
import { formatSQL } from "./sql-format";

describe("formatSQL", () => {
  it("breaks the select list at its top-level commas", () => {
    const input =
      "select IF(1 > 0, 'yes', 'no') as iff_translates, COALESCE(null, 'fallback') as nvl_translates, (CAST(CURRENT_DATE AS DATE) + interval 30 day) as dateadd_translates";

    expect(formatSQL(input)).toBe(
      [
        "select IF(1 > 0, 'yes', 'no') as iff_translates,",
        "COALESCE(null, 'fallback') as nvl_translates,",
        "(CAST(CURRENT_DATE AS DATE) + interval 30 day) as dateadd_translates",
      ].join("\n"),
    );
  });

  it("leaves the commas inside a function call alone", () => {
    // Breaking there would split one expression across lines for no reason.
    expect(formatSQL("select IF(a, b, c) from t")).toBe("select IF(a, b, c)\nfrom t");
  });

  it("starts a new line at each clause", () => {
    expect(formatSQL("select a from t where b = 1 group by a order by a limit 10")).toBe(
      ["select a", "from t", "where b = 1", "group by a", "order by a", "limit 10"].join("\n"),
    );
  });

  it("breaks a qualified join once, before its modifier", () => {
    expect(formatSQL("select a from t left join u on t.id = u.id")).toBe(
      ["select a", "from t", "left join u on t.id = u.id"].join("\n"),
    );
  });

  it("never breaks inside a string literal", () => {
    expect(formatSQL("select 'a, b from c' as s")).toBe("select 'a, b from c' as s");
  });

  it("handles a doubled quote inside a literal", () => {
    expect(formatSQL("select 'it''s, fine' as s")).toBe("select 'it''s, fine' as s");
  });

  it("leaves a comment's contents alone", () => {
    expect(formatSQL("select a /* x, from y */ from t")).toBe("select a /* x, from y */\nfrom t");
  });

  it("does not break a subquery's internals at the outer level", () => {
    expect(formatSQL("select * from (select a, b from t) x")).toBe(
      "select *\nfrom (select a, b from t) x",
    );
  });

  it("returns a statement with nothing to break unchanged", () => {
    expect(formatSQL("select 1")).toBe("select 1");
  });

  it("tolerates an empty statement", () => {
    expect(formatSQL("")).toBe("");
  });

  it("tolerates unbalanced parentheses", () => {
    expect(formatSQL("select IF(a, b")).toBe("select IF(a, b");
  });
});
