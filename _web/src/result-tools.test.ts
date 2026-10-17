import { afterEach, expect, it } from "vitest";
import { createResultTools } from "./result-tools";
import { renderGrid } from "./grid";
import type { Statement } from "./api";
afterEach(() => document.body.replaceChildren());
const result: Statement = { columns: [{ name: "ID", type: "NUMBER", nullable: false }, { name: "NAME", type: "TEXT", nullable: false }], rows: [[1, "Ana"], [2, "Bob"]], totalRows: 2, elapsedMs: 804, handle: "h", rowsAffected: null };
it("filters loaded values and renders the real count and measured duration", () => {
  const parent = document.createElement("div"); const grid = renderGrid(result); document.body.append(parent, grid);
  const tools = createResultTools(parent, () => result, () => grid); tools.refresh(true);
  expect(parent.querySelector(".result-row-count")!.textContent).toBe("2 rows");
  expect(parent.querySelector(".result-duration")!.textContent).toBe("804ms");
  const search = parent.querySelector<HTMLInputElement>(".result-search")!; search.value = "ana"; search.dispatchEvent(new Event("input"));
  expect(parent.querySelector(".result-row-count")!.textContent).toBe("1 row");
  expect(grid.querySelectorAll("tbody tr:not([hidden])")).toHaveLength(1);
  expect(parent.querySelector(".duration-options")!.textContent).toContain("Not measured");
});
it("applies selected columns and supports select all without mutating the result", () => {
  const parent = document.createElement("div"); const grid = renderGrid(result); document.body.append(parent, grid);
  const tools = createResultTools(parent, () => result, () => grid); tools.refresh(true);
  const boxes = parent.querySelectorAll<HTMLInputElement>(".column-checklist input"); boxes[1]!.checked = false;
  parent.querySelector<HTMLButtonElement>(".apply-columns")!.click();
  expect(grid.querySelector<HTMLTableCellElement>("th:nth-child(3)")!.hidden).toBe(true);
  expect(result.columns).toHaveLength(2);
  const all = parent.querySelector<HTMLInputElement>(".select-all-columns input")!; all.checked = true; all.dispatchEvent(new Event("change"));
  parent.querySelector<HTMLButtonElement>(".apply-columns")!.click();
  expect(grid.querySelector<HTMLTableCellElement>("th:nth-child(3)")!.hidden).toBe(false);
});
it("escapes column names and resets controls when the result changes", () => {
  let value: Statement | null = { ...result, columns: [{ name: "<img src=x>", type: "TEXT", nullable: true }] };
  const parent = document.createElement("div"); const grid = renderGrid(value); document.body.append(parent, grid);
  const tools = createResultTools(parent, () => value, () => grid); tools.refresh(true);
  expect(parent.querySelector("img")).toBeNull();
  value = null; tools.refresh(true); expect(parent.querySelector<HTMLElement>(".result-tools")!.hidden).toBe(true);
});
it("retains search and column visibility when sorting existing rows", () => {
  const source = { ...result, rows: [[10, "Ana"], [2, "Ana"], [3, "Bob"]] };
  const parent = document.createElement("div"); const grid = renderGrid(source); document.body.append(parent, grid);
  const tools = createResultTools(parent, () => source, () => grid); tools.refresh(true);
  const search = parent.querySelector<HTMLInputElement>(".result-search")!;
  search.value = "Ana"; search.dispatchEvent(new Event("input"));
  parent.querySelector<HTMLInputElement>('.column-checklist input[value="0"]')!.checked = false;
  parent.querySelector<HTMLButtonElement>(".apply-columns")!.click();
  grid.querySelector<HTMLElement>("th:nth-child(3)")!.click();
  expect(grid.querySelectorAll("tbody tr:not([hidden])")).toHaveLength(2);
  expect(grid.querySelector<HTMLTableCellElement>("tbody td:nth-child(2)")!.hidden).toBe(true);
  expect(parent.querySelector(".result-row-count")!.textContent).toBe("2 rows");
});
