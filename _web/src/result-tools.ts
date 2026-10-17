import type { Statement } from "./api";
const icons = {
  search: "M16 16l5 5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
  columns: "M3 4h18l-7 8v7l-4 2v-9zM3 4v16h4",
};
/** Client-side view controls; the stored result and exported data are unchanged. */
export function createResultTools(parent: HTMLElement, current: () => Statement | null, grid: () => HTMLElement) {
  const root = document.createElement("div"); root.className = "result-tools";
  root.innerHTML = `<button class="icon-btn result-search-toggle" aria-label="Search results"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="${icons.search}"/></svg></button><input class="result-search" type="search" aria-label="Search result values" placeholder="Search results" hidden><details class="column-picker result-popover"><summary aria-label="Select columns" title="Select columns"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="${icons.columns}"/></svg></summary><div class="column-options"><input type="search" aria-label="Search columns" placeholder="Search"><label class="select-all-columns"><input type="checkbox" checked> Select all</label><div class="column-checklist"></div><button class="run apply-columns">Apply</button></div></details><span class="result-row-count"></span><span class="row-info" title="Rows currently loaded in this browser">ⓘ</span><details class="duration-info result-popover"><summary aria-label="Query duration"><span class="result-duration"></span><span class="duration-bar"></span></summary><div class="duration-options"><strong>Query duration</strong><p>Compilation <span>Not measured</span></p><p>Queued provisioning <span>Not measured</span></p><p>Execution <span>Not measured</span></p><p class="duration-total">Total <span></span></p><small>Total is measured by the browser, including request and polling time.</small></div></details>`;
  parent.append(root);
  const picker = root.querySelector<HTMLDetailsElement>(".column-picker")!;
  const checklist = root.querySelector<HTMLElement>(".column-checklist")!;
  const columnSearch = root.querySelector<HTMLInputElement>('[aria-label="Search columns"]')!;
  const selectAll = root.querySelector<HTMLInputElement>('.select-all-columns input')!;
  const search = root.querySelector<HTMLInputElement>(".result-search")!;
  let result: Statement | null = null;
  let visible = new Set<number>();
  function apply(): void {
    const table = grid().querySelector("table.grid"); if (!table) return;
    table.querySelectorAll<HTMLTableRowElement>("tr").forEach((row) => {
      [...row.cells].forEach((cell, index) => { if (index > 0 && !row.classList.contains("empty")) cell.hidden = !visible.has(index - 1); });
      if (row.parentElement?.tagName === "TBODY" && !row.classList.contains("empty")) {
        row.hidden = search.value !== "" && ![...row.cells].some((cell, index) => index > 0 && !cell.hidden && (cell.textContent ?? "").toLocaleLowerCase().includes(search.value.toLocaleLowerCase()));
      }
    });
    const loaded = table.querySelectorAll("tbody tr:not(.empty):not([hidden])").length;
    root.querySelector(".result-row-count")!.textContent = `${loaded} ${loaded === 1 ? "row" : "rows"}`;
  }
  function syncAll(): void {
    const boxes = [...checklist.querySelectorAll<HTMLInputElement>("input")];
    selectAll.checked = boxes.length > 0 && boxes.every((box) => box.checked);
    selectAll.indeterminate = boxes.some((box) => box.checked) && !selectAll.checked;
  }
  function populate(): void {
    columnSearch.value = ""; checklist.replaceChildren();
    result?.columns.forEach((column, index) => {
      const label = document.createElement("label"); const box = document.createElement("input"); box.type = "checkbox"; box.checked = visible.has(index); box.value = String(index);
      box.addEventListener("change", syncAll); label.append(box, document.createTextNode(column.name)); checklist.append(label);
    }); syncAll();
  }
  function position(details: HTMLDetailsElement, selector: string): void {
    if (!details.open) return;
    const panel = details.querySelector<HTMLElement>(selector)!;
    const anchor = details.querySelector("summary")!.getBoundingClientRect();
    panel.style.position = "fixed";
    panel.style.maxHeight = `${Math.max(100, window.innerHeight - 24)}px`;
    panel.style.overflowY = "auto";
    const size = panel.getBoundingClientRect();
    panel.style.top = `${Math.max(12, anchor.top - size.height - 14)}px`;
    panel.style.left = `${Math.max(12, Math.min(anchor.right - size.width, window.innerWidth - size.width - 12))}px`;
    panel.style.bottom = "auto"; panel.style.right = "auto";
  }
  picker.addEventListener("toggle", () => { if (picker.open) { populate(); position(picker, ".column-options"); } });
  const duration = root.querySelector<HTMLDetailsElement>(".duration-info")!;
  duration.addEventListener("toggle", () => position(duration, ".duration-options"));
  selectAll.addEventListener("change", () => { checklist.querySelectorAll<HTMLInputElement>("input").forEach((box) => { box.checked = selectAll.checked; }); selectAll.indeterminate = false; });
  columnSearch.addEventListener("input", () => { checklist.querySelectorAll("label").forEach((label) => { label.hidden = !(label.textContent ?? "").toLocaleLowerCase().includes(columnSearch.value.toLocaleLowerCase()); }); });
  root.querySelector(".apply-columns")!.addEventListener("click", () => {
    visible = new Set([...checklist.querySelectorAll<HTMLInputElement>("input:checked")].map((box) => Number(box.value))); picker.open = false; apply();
  });
  root.querySelector(".result-search-toggle")!.addEventListener("click", () => { search.hidden = !search.hidden; if (!search.hidden) search.focus(); else { search.value = ""; apply(); } });
  search.addEventListener("input", apply);
  root.addEventListener("keydown", (event) => { if (event.key === "Escape") root.querySelectorAll<HTMLDetailsElement>("details[open]").forEach((details) => { details.open = false; details.querySelector("summary")?.focus(); }); });
  return {
    refresh(enabled: boolean): void {
      const next = current();
      if (next !== result) { result = next; visible = new Set(next?.columns.map((_, index) => index) ?? []); search.value = ""; picker.open = false; populate(); }
      root.hidden = !enabled || !next;
      if (!next) return;
      const duration = `${Math.round(next.elapsedMs)}ms`;
      root.querySelector(".result-duration")!.textContent = duration;
      root.querySelector(".duration-total span")!.textContent = duration;
      root.querySelector<HTMLElement>(".row-info")!.title = `${next.rows.length} loaded of ${next.totalRows} total rows`;
      apply();
    },
  };
}
