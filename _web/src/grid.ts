import { compareValues, sortableHeaders } from "./table-sort";
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
  const gutter = cell("th", "", "rn");
  gutter.append(dataTypeIcon("gutter"));
  headRow.append(gutter);

  statement.columns.forEach((column, index) => {
    const th = cell("th", "");
    th.title = `${column.name} · ${column.type}`;
    const heading = cell("span", "", "column-heading");
    heading.append(dataTypeIcon(numericColumns[index] ? "NUMBER" : column.type), cell("span", column.name));
    th.append(heading);
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
  const renderedRows: HTMLTableRowElement[] = [];
  statement.rows.forEach((row, rowIndex) => {
    const tr = document.createElement("tr");
    tr.append(cell("td", String(rowIndex + 1), "rn"));

    statement.columns.forEach((column, index) => {
      const formatted = formatCell(row[index] ?? null, column);
      const td = cell("td", formatted.isNull ? "null" : formatted.text);
      if (formatted.isNull) {
        td.classList.add("nul");
      } else if (numericColumns[index] || formatted.isNumeric) {
        td.classList.add("num");
      }
      tr.append(td);
    });

    renderedRows.push(tr);
    body.append(tr);
  });

  if (statement.rows.length === 0) {
    body.append(emptyRow(statement.columns.length + 1));
  }

  sortableHeaders([...headRow.children].slice(1) as HTMLElement[], (index, direction) => {
    const column = statement.columns[index]!;
    const kind = dataTypeKind(column.type);
    const numeric = numericColumns[index] || kind === "number";
    const value = (rowIndex: number): string | number | boolean | null => {
      const raw = statement.rows[rowIndex]![index] ?? null;
      if (raw === null || typeof raw === "boolean") return raw;
      const formatted = formatCell(raw, column).text;
      if (kind === "date" || kind === "timestamp") {
        const original = typeof raw === "string" ? raw : formatted;
        const epoch = Date.parse(original);
        if (Number.isFinite(epoch)) {
          // Keep timestamp fractions beyond Date's millisecond precision.
          const fraction = /[T ]\d{2}:\d{2}:\d{2}\.(\d+)/.exec(original)?.[1]?.slice(0, 9) || "";
          const precision = Math.max(3, fraction.length);
          const scale = 10n ** BigInt(precision);
          const ticks = BigInt(Math.floor(epoch / 1000)) * scale + BigInt(fraction.padEnd(precision, "0"));
          return `${ticks}e-${precision}`;
        }
      }
      return formatted;
    };
    const order = renderedRows.map((row, rowIndex) => ({ row, rowIndex }));
    order.sort((a, b) => compareValues(value(a.rowIndex), value(b.rowIndex), numeric || kind === "date" || kind === "timestamp") * (direction === "ascending" ? 1 : -1));
    order.forEach(({ row }, rowIndex) => { row.cells[0]!.textContent = String(rowIndex + 1); body.append(row); });
  });

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
  if (detail && detail !== title) {
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


export function dataTypeKind(type: string): string {
  const name = type.toUpperCase();
  if (/^(NUMBER|NUMERIC|DECIMAL|FIXED|REAL|FLOAT|DOUBLE|BIGINT|SMALLINT|TINYINT|INTEGER|INT|UBIGINT)/.test(name)) return "number";
  if (name.startsWith("TIMESTAMP")) return "timestamp";
  if (name === "DATE") return "date";
  if (name.startsWith("TIME")) return "time";
  if (/^(BOOLEAN|BOOL)$/.test(name)) return "boolean";
  if (/^(VARIANT|OBJECT|ARRAY|JSON)/.test(name)) return "variant";
  if (/^(BINARY|BLOB|VARBINARY)/.test(name)) return "binary";
  if (name === "GUTTER") return "gutter";
  return "text";
}
function dataTypeIcon(type: string): SVGSVGElement {
  const kind = dataTypeKind(type);
  const paths: Record<string, string> = {
    number: "M9 3 7 21M17 3l-2 18M3 9h18M2 15h18",
    text: "M6 17 12 3l6 14M8 12h8M5 21h14",
    timestamp: "M20 8a8 8 0 1 0-14 12M12 5v7l5 2M13 17v5M17 17v5M13 17l4 5M19 17h5M21.5 17v5",
    date: "M3 6h18v15H3zM7 3v6M17 3v6M3 11h18",
    time: "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M12 6v6l4 2",
    boolean: "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M7 12l3 3 7-7",
    variant: "M8 3H5v6l-3 3 3 3v6h3M16 3h3v6l3 3-3 3v6h-3",
    binary: "M4 4h5v16H4zM15 5l3-1v16M15 20h6",
    gutter: "M3 11h4v10H3zM10 4h4v17h-4zM17 8h4v13h-4z",
  };
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "1.5");
  svg.setAttribute("stroke-linejoin", "round");
  svg.classList.add("data-type-icon", `type-${kind}`);
  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  path.setAttribute("d", paths[kind]!);
  svg.append(path);
  return svg;
}
