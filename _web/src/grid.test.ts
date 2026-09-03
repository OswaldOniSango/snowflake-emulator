import { describe, expect, it } from "vitest";
import type { Statement } from "./api";
import { renderGrid, renderNotice } from "./grid";

function statement(overrides: Partial<Statement> = {}): Statement {
  return {
    columns: [
      { name: "id", type: "NUMBER", nullable: true },
      { name: "email", type: "TEXT", nullable: true },
    ],
    rows: [
      [1, "ana@example.dev"],
      [2, null],
    ],
    totalRows: 2,
    handle: "h",
    elapsedMs: 1,
    rowsAffected: null,
    ...overrides,
  };
}

const text = (element: HTMLElement, selector: string): string[] =>
  [...element.querySelectorAll(selector)].map((node) => node.textContent ?? "");

describe("renderGrid", () => {
  it("puts every column in the header with its type", () => {
    const grid = renderGrid(statement());

    expect(text(grid, "thead th")).toEqual(["", "idNUMBER", "emailTEXT"]);
  });

  it("numbers the rows and renders every cell", () => {
    const grid = renderGrid(statement());
    const rows = [...grid.querySelectorAll("tbody tr")];

    expect(rows).toHaveLength(2);
    expect(text(rows[0] as HTMLElement, "td")).toEqual(["1", "1", "ana@example.dev"]);
  });

  it("marks a NULL apart from the text 'NULL'", () => {
    const grid = renderGrid(
      statement({ rows: [[1, null], [2, "NULL"]] }),
    );
    const cells = [...grid.querySelectorAll("tbody td")];

    const nulls = cells.filter((cell) => cell.classList.contains("nul"));
    expect(nulls).toHaveLength(1);
    expect(nulls[0]?.textContent).toBe("NULL");
  });

  it("right-aligns a decimal column the type does not declare numeric", () => {
    const grid = renderGrid(
      statement({
        columns: [{ name: "precio", type: "TEXT", nullable: true }],
        rows: [[{ Width: 10, Scale: 2, Value: 1050 }], [{ Width: 10, Scale: 2, Value: 5 }]],
      }),
    );

    const cells = [...grid.querySelectorAll("tbody td:not(.rn)")];
    expect(cells.map((cell) => cell.textContent)).toEqual(["10.50", "0.05"]);
    expect(cells.every((cell) => cell.classList.contains("num"))).toBe(true);
  });

  it("renders an empty result without a body row", () => {
    const grid = renderGrid(statement({ rows: [], totalRows: 0 }));

    expect(grid.querySelectorAll("tbody tr")).toHaveLength(0);
    expect(grid.querySelectorAll("thead th")).toHaveLength(3);
  });
});

/**
 * Column names and cell values come from the server, so they are written with
 * textContent and never parsed as markup. Nothing pinned that until now.
 */
describe("renderGrid does not treat server data as markup", () => {
  it("renders a cell value containing a tag as text", () => {
    const grid = renderGrid(
      statement({
        columns: [{ name: "note", type: "TEXT", nullable: true }],
        rows: [["<script>alert(1)</script>"]],
      }),
    );

    expect(grid.querySelector("script")).toBeNull();
    expect(grid.querySelector("tbody td:not(.rn)")?.textContent).toBe("<script>alert(1)</script>");
  });

  it("renders a column name containing a tag as text", () => {
    const grid = renderGrid(
      statement({
        columns: [{ name: "<img src=x onerror=1>", type: "TEXT", nullable: true }],
        rows: [["a"]],
      }),
    );

    expect(grid.querySelector("img")).toBeNull();
    expect(grid.querySelector("thead th:not(.rn)")?.textContent).toContain("<img src=x onerror=1>");
  });

  it("renders a column type containing a tag as text", () => {
    const grid = renderGrid(
      statement({
        columns: [{ name: "a", type: "<b>TEXT</b>", nullable: true }],
        rows: [["x"]],
      }),
    );

    expect(grid.querySelector("thead b")).toBeNull();
  });
});

describe("renderNotice", () => {
  it("shows a title and a detail", () => {
    const notice = renderNotice("info", "A title", "Some detail");

    expect(notice.querySelector("h4")?.textContent).toBe("A title");
    expect(notice.querySelector("pre")?.textContent).toBe("Some detail");
  });

  it("omits the detail when there is none", () => {
    expect(renderNotice("info", "Only a title").querySelector("pre")).toBeNull();
  });

  it("marks an error apart from a note", () => {
    expect(renderNotice("error", "Failed").classList.contains("error")).toBe(true);
    expect(renderNotice("info", "Fine").classList.contains("error")).toBe(false);
  });

  it("renders an error message containing markup as text", () => {
    // Emulator errors quote the failing SQL back, which can contain anything.
    const notice = renderNotice("error", "boom", "<script>alert(1)</script>");

    expect(notice.querySelector("script")).toBeNull();
    expect(notice.querySelector("pre")?.textContent).toBe("<script>alert(1)</script>");
  });
});
