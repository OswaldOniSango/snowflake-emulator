import { listHistory, type HistoryEntry } from "./api";
import { renderNotice } from "./grid";

/**
 * The statement history.
 *
 * The emulator keeps statements in memory for a fixed period, so this is a
 * recent history rather than a complete one — and it starts empty after a
 * restart. The retention the API reports is shown, so an empty table reads as
 * "nothing recent" instead of "something is broken".
 */

export interface HistoryOptions {
  parent: HTMLElement;
  /** Reopens a statement in a new worksheet. */
  onOpen: (entry: HistoryEntry) => void;
}

export function createHistoryView(options: HistoryOptions): { refresh: () => Promise<void> } {
  const root = document.createElement("div");
  root.className = "view";
  options.parent.append(root);

  async function refresh(): Promise<void> {
    try {
      const history = await listHistory();
      root.replaceChildren(header(history.retainedFor));
      root.append(
        history.statements.length === 0
          ? renderNotice(
              "info",
              "No statements yet",
              `The emulator keeps statements for ${humanise(history.retainedFor)} and forgets them on restart.`,
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
    heading.textContent = "Statement history";

    const blurb = document.createElement("p");
    blurb.textContent = retainedFor
      ? `Everything submitted through this emulator in the last ${humanise(retainedFor)}. Click a row to reopen it in a new worksheet.`
      : "Everything submitted through this emulator. Click a row to reopen it in a new worksheet.";

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
    const wrapper = document.createElement("div");
    wrapper.className = "tablewrap";

    const element = document.createElement("table");
    element.className = "grid";

    const head = document.createElement("thead");
    const headRow = document.createElement("tr");
    for (const [label, type] of [
      ["Started", "local time"],
      ["Statement", "TEXT"],
      ["Status", "code"],
      ["Duration", "ms"],
      ["Rows", "NUMBER"],
      ["Handle", "statementHandle"],
    ]) {
      const th = document.createElement("th");
      th.textContent = label ?? "";
      const caption = document.createElement("span");
      caption.className = "ty";
      caption.textContent = type ?? "";
      th.append(caption);
      headRow.append(th);
    }
    head.append(headRow);

    const body = document.createElement("tbody");
    for (const entry of entries) {
      body.append(row(entry));
    }

    element.append(head, body);
    wrapper.append(element);
    return wrapper;
  }

  function row(entry: HistoryEntry): HTMLElement {
    const tr = document.createElement("tr");
    tr.className = "history-row";
    tr.tabIndex = 0;
    tr.title = "Open in a new worksheet";

    const open = (): void => options.onOpen(entry);
    tr.addEventListener("click", open);
    tr.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        open();
      }
    });

    tr.append(cell(time(entry.createdOn)));

    const statement = cell("");
    const text = document.createElement("div");
    text.className = "hist-sql";
    text.textContent = entry.statement;
    statement.append(text);
    tr.append(statement);

    const status = cell("");
    const pill = document.createElement("span");
    pill.className = `pill ${statusClass(entry.status)}`;
    pill.textContent = entry.status;
    status.append(pill);
    if (entry.code) {
      const code = document.createElement("span");
      code.className = "hist-code";
      code.textContent = entry.code;
      status.append(code);
    }
    tr.append(status);

    tr.append(cell(entry.durationMs === undefined ? "—" : String(entry.durationMs), "num"));
    tr.append(cell(String(entry.numRows), "num"));
    tr.append(cell(entry.statementHandle));

    return tr;
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

function time(epochMs: number): string {
  return new Date(epochMs).toLocaleTimeString();
}

/** Turns a Go duration such as "1h0m0s" into something readable. */
export function humanise(duration: string): string {
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?$/.exec(duration.trim());
  if (!match) {
    return duration;
  }

  const [, hours, minutes, seconds] = match;
  const parts: string[] = [];
  if (hours && hours !== "0") {
    parts.push(`${hours} ${hours === "1" ? "hour" : "hours"}`);
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
