import { compareValues, sortableHeaders, type SortDirection } from "./table-sort";
import { listHistory, type HistoryEntry } from "./api";
import { warehouseSize } from "./warehouse-picker";
import { renderNotice } from "./grid";

/**
 * The statement history.
 *
 * The emulator keeps statements for a fixed period, so this is a recent
 * history rather than a complete one, and it survives a restart only when the
 * emulator was given a database file. Both are shown, so an empty table reads
 * as "nothing recent" instead of "something is broken".
 */

export interface HistoryOptions {
  parent: HTMLElement;
  /** Reopens a statement in a new worksheet. */
  onOpen: (entry: HistoryEntry) => void;
}

export function createHistoryView(options: HistoryOptions): { refresh: () => Promise<void> } {
  const root = document.createElement("div");
  root.className = "view query-history";
  options.parent.append(root);

  async function refresh(): Promise<void> {
    root.replaceChildren(header(""), renderNotice("info", "Loading history…"));
    try {
      const history = await listHistory();
      root.replaceChildren(header(history.retainedFor));
      root.append(
        history.statements.length === 0
          ? renderNotice(
              "info",
              "No statements yet",
              emptyDetail(history.retainedFor, history.persistent ?? false),
            )
          : table(history.statements),
      );
    } catch (cause) {
      root.replaceChildren(header(""));
      root.append(renderNotice("error", "Could not load the history", messageOf(cause)));
    }
  }

  function header(retainedFor: string): HTMLElement {
    const head = document.createElement("div");
    head.className = "view-head";

    const heading = document.createElement("h1");
    heading.textContent = "Query History";

    const blurb = document.createElement("p");
    blurb.textContent = retainedFor
      ? `Your statements from the last ${humanise(retainedFor)}. Click a row to reopen it in a new worksheet.`
      : "Your statements. Click a row to reopen it in a new worksheet.";

    const text = document.createElement("div");
    text.append(heading, blurb);

    const reload = document.createElement("button");
    reload.className = "ghost";
    reload.textContent = "Reload";
    reload.addEventListener("click", () => void refresh());

    head.append(text, reload);
    return head;
  }

  function table(entries: HistoryEntry[]): HTMLElement {
    const area = document.createElement("section");
    const controls = document.createElement("div");
    controls.className = "history-controls";
    controls.innerHTML = `<label>Status <select aria-label="History status"><option value="">All</option><option value="success">Success</option><option value="failed">Failed</option><option value="running">Running</option></select></label><label>User <select aria-label="History user"><option value="">All</option></select></label><label>◷ <select aria-label="History period"><option value="0">All retained</option><option value="86400000">Last day</option><option value="604800000">Last 7 days</option></select></label><input type="search" aria-label="Search query history" placeholder="Search SQL or query ID"><span class="history-count"></span><details class="history-columns"><summary>▥ Columns</summary></details>`;
    const wrapper = document.createElement("div");
    wrapper.className = "tablewrap";
    const element = document.createElement("table");
    element.className = "grid";
    const columns = ["SQL TEXT", "QUERY ID", "STATUS", "USER", "WAREHOUSE", "WAREHOUSE SIZE", "DURATION", "STARTED", "ROWS"];
    const visible = columns.map(() => true);
    let sortColumn = -1;
    let sortDirection: SortDirection = "ascending";
    const head = document.createElement("thead");
    const headRow = document.createElement("tr");
    headRow.append(document.createElement("th"));
    columns.forEach((label, index) => {
      const th = document.createElement("th");
      th.textContent = label;
      headRow.append(th);
      const option = document.createElement("label");
      const check = document.createElement("input");
      check.type = "checkbox"; check.checked = true;
      check.addEventListener("change", () => { visible[index] = check.checked; render(); });
      option.append(check, label);
      controls.querySelector("details")!.append(option);
    });
    head.append(headRow);
    const body = document.createElement("tbody");
    element.append(head, body); wrapper.append(element); area.append(controls, wrapper);
    const status = controls.querySelector<HTMLSelectElement>('[aria-label="History status"]')!;
    const period = controls.querySelector<HTMLSelectElement>('[aria-label="History period"]')!;
    const search = controls.querySelector<HTMLInputElement>('input[type="search"]')!;
    const user = controls.querySelector<HTMLSelectElement>('[aria-label="History user"]')!;
    for (const username of [...new Set(entries.map((entry) => entry.username).filter((name): name is string => !!name))].sort()) {
      const option = document.createElement("option"); option.value = `user:${username}`; option.textContent = username; user.append(option);
    }
    if (entries.some((entry) => !entry.username)) {
      const option = document.createElement("option"); option.value = "missing"; option.textContent = "Not recorded"; user.append(option);
    }
    const sizeOrder = ["XSMALL", "SMALL", "MEDIUM", "LARGE", "XLARGE", "2XLARGE", "3XLARGE", "4XLARGE", "5XLARGE", "6XLARGE"];
    function sortValue(entry: HistoryEntry): string | number | null {
      switch (sortColumn) {
        case 0: return entry.statement;
        case 1: return entry.statementHandle;
        case 2: return entry.status;
        case 3: return entry.username || null;
        case 4: return entry.warehouse || null;
        case 5: return entry.warehouseSize ? sizeOrder.indexOf(entry.warehouseSize.toUpperCase().replaceAll(/[-_]/g, "")) : null;
        case 6: return entry.durationMs ?? null;
        case 7: return entry.createdOn;
        case 8: return entry.numRows;
        default: return null;
      }
    }
    sortableHeaders([...headRow.children].slice(1) as HTMLElement[], (index, direction) => { sortColumn = index; sortDirection = direction; render(); });

    function render(): void {
      const query = search.value.toLowerCase();
      const filtered = entries.filter((entry) => (!status.value || entry.status === status.value)
        && (!user.value || (user.value === "missing" ? !entry.username : `user:${entry.username}` === user.value))
        && (!Number(period.value) || entry.createdOn >= Date.now() - Number(period.value))
        && `${entry.statement} ${entry.statementHandle}`.toLowerCase().includes(query));
      if (sortColumn >= 0) filtered.sort((a, b) => compareValues(sortValue(a), sortValue(b), sortColumn >= 5) * (sortDirection === "ascending" ? 1 : -1));
      controls.querySelector(".history-count")!.textContent = `${filtered.length} Queries`;
      body.replaceChildren();
      [...headRow.children].forEach((th, index) => { (th as HTMLElement).hidden = index > 0 && !visible[index - 1]; });
      filtered.forEach((entry, index) => {
        const tr = document.createElement("tr");
        tr.className = "history-row"; tr.tabIndex = 0; tr.title = "Open in a new worksheet";
        tr.addEventListener("click", () => options.onOpen(entry));
        tr.addEventListener("keydown", (event) => {
          if (event.key === "Enter" || event.key === " ") { event.preventDefault(); options.onOpen(entry); }
        });
        tr.append(cell(String(index + 1), "history-index"));
        const sql = cell(entry.statement, "hist-sql"); sql.title = entry.statement;
        const state = cell("");
        const badge = document.createElement("span");
        badge.className = `pill ${statusClass(entry.status)}`;
        badge.textContent = entry.status === "success" ? "Success" : entry.status === "failed" ? "Failed ⓘ" : entry.status;
        badge.title = entry.message || entry.code || entry.status; state.append(badge);
        const duration = entry.durationMs === undefined ? "—" : entry.durationMs < 1000 ? `${entry.durationMs}ms` : `${(entry.durationMs / 1000).toFixed(1)}s`;
        tr.append(sql, cell(entry.statementHandle), state, cell(entry.username || "—"), cell(entry.warehouse || "—"), cell(entry.warehouseSize ? warehouseSize(entry.warehouseSize) : "—"), cell(duration, "num"), cell(new Date(entry.createdOn).toLocaleString()), cell(String(entry.numRows), "num"));
        [...tr.children].forEach((td, i) => { (td as HTMLElement).hidden = i > 0 && !visible[i - 1]; });
        body.append(tr);
      });
    }
    user.addEventListener("change", render);
    status.addEventListener("change", render); period.addEventListener("change", render); search.addEventListener("input", render);
    render();
    return area;
  }

  void refresh();
  return { refresh };
}

function cell(text: string, className = ""): HTMLElement {
  const td = document.createElement("td");
  if (className) {
    td.className = className;
  }
  td.textContent = text;
  return td;
}

function statusClass(status: string): string {
  if (status === "success") {
    return "ok";
  }
  if (status === "failed") {
    return "err";
  }
  return "idle";
}


/** Turns a Go duration such as "1h0m0s" into something readable. */
/**
 * Why the history might be empty. A reader who has just restarted the emulator
 * needs to know whether their statements were kept or thrown away, and that
 * depends on whether it was started against a database file.
 */
export function emptyDetail(retainedFor: string, persistent: boolean): string {
  const kept = `The emulator keeps statements for ${humanise(retainedFor)}`;
  return persistent
    ? `${kept}.`
    : `${kept}, and forgets them on restart because it is running with an in-memory database. Start it with DB_PATH set to a file to keep them.`;
}

export function humanise(duration: string): string {
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?$/.exec(duration.trim());
  if (!match) {
    return duration;
  }

  const [, hours, minutes, seconds] = match;
  const parts: string[] = [];
  if (hours && hours !== "0") {
    // Go writes a week as 168h. Past a couple of days nobody counts in hours,
    // so whole days are said as days and the remainder keeps its hours.
    const total = Number(hours);
    const days = Math.floor(total / 24);
    const rest = total % 24;
    if (days >= 2) {
      parts.push(`${days} days`);
      if (rest > 0) {
        parts.push(`${rest} ${rest === 1 ? "hour" : "hours"}`);
      }
    } else {
      parts.push(`${hours} ${hours === "1" ? "hour" : "hours"}`);
    }
  }
  if (minutes && minutes !== "0") {
    parts.push(`${minutes} minutes`);
  }
  if (parts.length === 0 && seconds && seconds !== "0") {
    parts.push(`${seconds} seconds`);
  }
  return parts.length > 0 ? parts.join(" ") : duration;
}

function messageOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : "Request failed";
}
