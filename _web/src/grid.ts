import type { Statement } from "./api";
import { formatCell, isNumericColumn } from "./format";

/**
 * Renders a result set. Values are set with textContent, never as markup.
 *
 * A query that matched nothing still renders its header row. The columns are
 * the answer to "did this run against what I meant?", and a bare "no rows"
 * notice throws them away — leaving a reader unable to tell an empty table
 * from a query they got wrong.
 */
export function renderGrid(statement: Statement): HTMLElement {
  const wrapper = document.createElement("div");
  wrapper.className = "grid-wrap";

  const table = document.createElement("table");
  table.className = "grid";

  const numericColumns = statement.columns.map((column, index) =>
    isNumericColumn(statement.rows, index, column),
  );

  const head = document.createElement("thead");
  const headRow = document.createElement("tr");
  headRow.append(cell("th", "", "rn"));

  statement.columns.forEach((column, index) => {
    const th = cell("th", column.name);
    if (numericColumns[index]) {
      th.classList.add("num");
    }
    const type = document.createElement("span");
    type.className = "ty";
    type.textContent = column.type;
    th.append(type);
    headRow.append(th);
  });

  head.append(headRow);
  table.append(head);

  const body = document.createElement("tbody");
  statement.rows.forEach((row, rowIndex) => {
    const tr = document.createElement("tr");
    tr.append(cell("td", String(rowIndex + 1), "rn"));

    statement.columns.forEach((column, index) => {
      const formatted = formatCell(row[index] ?? null, column);
      const td = cell("td", formatted.text);
      if (formatted.isNull) {
        td.classList.add("nul");
      } else if (numericColumns[index] || formatted.isNumeric) {
        td.classList.add("num");
      }
      tr.append(td);
    });

    body.append(tr);
  });

  if (statement.rows.length === 0) {
    body.append(emptyRow(statement.columns.length + 1));
  }

  table.append(body);
  wrapper.append(table);
  return wrapper;
}

/** Says the result is empty, spanning the grid so it reads as part of it. */
function emptyRow(span: number): HTMLElement {
  const row = document.createElement("tr");
  row.className = "empty";

  const cell = document.createElement("td");
  cell.colSpan = span;
  cell.textContent = "No rows";
  row.append(cell);
  return row;
}

/** A message pane: an empty result, a row count, or a failure. */
export function renderNotice(
  kind: "info" | "error",
  title: string,
  detail?: string,
): HTMLElement {
  const notice = document.createElement("div");
  notice.className = `notice ${kind}`;

  const bar = document.createElement("div");
  bar.className = "bar";

  const content = document.createElement("div");
  content.append(cell("h4", title));
  if (detail) {
    content.append(cell("pre", detail));
  }

  notice.append(bar, content);
  return notice;
}

function cell<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  text: string,
  className = "",
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) {
    node.className = className;
  }
  node.textContent = text;
  return node;
}
