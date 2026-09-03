import { describe, expect, it } from "vitest";
import type { Translation } from "./api";
import { renderTranslation } from "./translation";

function translation(overrides: Partial<Translation> = {}): Translation {
  return {
    statement: "SELECT IFF(a, 'y', 'n') FROM users",
    translated: "select IF(a, 'y', 'n') from TEST_DB.PUBLIC_USERS",
    handledBy: "translator",
    complete: true,
    rewrites: [],
    ...overrides,
  };
}

describe("renderTranslation", () => {
  it("shows what was written beside what runs", () => {
    const pane = renderTranslation(translation());
    const columns = [...pane.querySelectorAll(".xcol pre")].map((node) => node.textContent);

    expect(columns[0]).toBe("SELECT IFF(a, 'y', 'n') FROM users");
    expect(columns[1]).toContain("TEST_DB.PUBLIC_USERS");
  });

  it("breaks the generated SQL into lines but leaves the original alone", () => {
    const pane = renderTranslation(
      translation({
        statement: "SELECT a, b FROM t",
        translated: "select a, b from t",
      }),
    );
    const [written, runs] = [...pane.querySelectorAll(".xcol pre")].map((n) => n.textContent ?? "");

    expect(written).toBe("SELECT a, b FROM t");
    expect(runs).toBe("select a,\nb\nfrom t");
  });

  it("warns when a processor builds the real SQL elsewhere", () => {
    const pane = renderTranslation(
      translation({
        handledBy: "copy_processor",
        complete: false,
        note: "The stage reference is resolved to a file path on disk.",
      }),
    );

    const notice = pane.querySelector(".notice.warn");
    expect(notice?.querySelector("h4")?.textContent).toContain("COPY processor");
    expect(notice?.querySelector("p")?.textContent).toContain("file path on disk");
  });

  it("says so when a statement reaches DuckDB unchanged", () => {
    const pane = renderTranslation(
      translation({ statement: "SELECT 1", translated: "SELECT 1" }),
    );

    expect(pane.querySelector(".xnote")?.textContent).toContain("Nothing to translate");
  });

  it("names an unknown handler rather than hiding it", () => {
    const pane = renderTranslation(
      translation({ handledBy: "future_processor", complete: false, note: "…" }),
    );

    expect(pane.querySelector(".notice.warn h4")?.textContent).toContain("future_processor");
  });
});

/**
 * The SQL rendered here is whatever the caller typed and whatever the emulator
 * returned. Neither may be parsed as markup.
 */
describe("renderTranslation does not treat SQL as markup", () => {
  it("renders a statement containing a tag as text", () => {
    const pane = renderTranslation(
      translation({
        statement: "SELECT '<script>alert(1)</script>'",
        translated: "select '<script>alert(1)</script>'",
      }),
    );

    expect(pane.querySelector("script")).toBeNull();
    expect(pane.querySelectorAll(".xcol pre")[0]?.textContent).toContain("<script>");
  });

  it("renders a processor note containing a tag as text", () => {
    const pane = renderTranslation(
      translation({ complete: false, note: "<img src=x onerror=1>" }),
    );

    expect(pane.querySelector("img")).toBeNull();
  });
});

/**
 * The rewrite list is required, so a fixture that omitted it stopped
 * compiling the moment the field landed. This pins the panel's behaviour with
 * and without rewrites so the two stay in step.
 */
describe("renderTranslation with rewrites", () => {
  it("lists each substitution and why it happened", () => {
    const pane = renderTranslation(
      translation({
        rewrites: [
          { from: "IFF", to: "IF", kind: "function" },
          { from: "USERS", to: "TEST_DB.PUBLIC_USERS", kind: "object" },
        ],
      }),
    );

    const rows = [...pane.querySelectorAll(".rewrites tbody tr")].map((row) =>
      [...row.querySelectorAll("td")].map((cell) => cell.textContent),
    );

    expect(rows).toEqual([
      ["IFF", "IF", "DuckDB has no such function"],
      ["USERS", "TEST_DB.PUBLIC_USERS", "Resolved against the worksheet's database and schema"],
    ]);
  });

  it("shows no table when nothing was rewritten", () => {
    expect(renderTranslation(translation()).querySelector(".rewrites")).toBeNull();
  });

  it("renders a rewritten name containing a tag as text", () => {
    const pane = renderTranslation(
      translation({ rewrites: [{ from: "<script>alert(1)</script>", to: "X", kind: "object" }] }),
    );

    expect(pane.querySelector("script")).toBeNull();
    expect(pane.querySelector(".rewrites .from")?.textContent).toBe("<script>alert(1)</script>");
  });
});
