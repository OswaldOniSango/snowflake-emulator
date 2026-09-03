import type { Statement } from "./api";
import { formatCell, isNumericColumn } from "./format";

/** Renders a result set. Values are set with textContent, never as markup. */
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

  table.append(body);
  wrapper.append(table);
  return wrapper;
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
