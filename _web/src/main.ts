import "./style.css";
import { runStatement, StatementError, translateStatement } from "./api";
import { createEditor } from "./editor";
import { renderGrid, renderNotice } from "./grid";
import { renderTranslation } from "./translation";
import { checkHealth } from "./health";

// The execution context is fixed until the context selectors land. It matches
// the namespace the emulator provisions at startup.
const CONTEXT = { database: "TEST_DB", schema: "PUBLIC" };

// No leading comment: the emulator classifies statements with HasPrefix after a
// plain TrimSpace, so a comment before SELECT routes it down the DML path and
// the result set is silently dropped.
const INITIAL_SQL = `SELECT
    IFF(1 > 0, 'yes', 'no')      AS iff_translates,
    NVL(NULL, 'fallback')        AS nvl_translates,
    DATEADD(day, 30, CURRENT_DATE) AS dateadd_translates;`;

// Static, author-controlled markup. Everything derived from data is built with
// createElement and textContent — see grid.ts.
const MARK = `
<svg viewBox="0 0 24 24" aria-hidden="true">
  <defs>
    <linearGradient id="mallard-mark" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#12866F" />
      <stop offset="1" stop-color="#0B4E52" />
    </linearGradient>
  </defs>
  <rect width="24" height="24" rx="7" fill="url(#mallard-mark)" />
  <circle cx="10.2" cy="9.9" r="4.3" fill="#FCFEFD" />
  <path d="M13.3 10.9 L20.2 12.1 L13.3 14.2 Z" fill="#E0A03E" />
  <circle cx="11.7" cy="9" r=".95" fill="#0B4E52" />
</svg>`;

const SHELL = `
<header class="topbar">
  <div class="brand">${MARK}<b>Mallard</b><span>local</span></div>
  <div class="context" title="Fixed until the context selectors land">
    <span class="lab">Context</span><b data-role="context"></b>
  </div>
  <div class="spacer"></div>
  <div class="conn" data-state="pending" role="status">
    <span class="dot"></span><span data-role="health">Checking emulator…</span>
  </div>
</header>

<main class="workspace">
  <section class="editor" data-role="editor"></section>

  <div class="toolbar">
    <button class="run" data-role="run">Run <kbd data-role="shortcut"></kbd></button>
    <div class="status">
      <span class="pill idle" data-role="pill">Idle</span>
      <span data-role="meta">no statement submitted</span>
    </div>
  </div>

  <div class="dock-head" role="tablist" aria-label="Statement output">
    <button class="dtab" data-tab="results" aria-selected="true" role="tab">Results</button>
    <button class="dtab" data-tab="translation" aria-selected="false" role="tab">Translated SQL</button>
  </div>

  <section class="dock" data-role="dock" aria-live="polite"></section>
</main>`;

function pick<T extends HTMLElement>(root: ParentNode, role: string): T {
  const node = root.querySelector<T>(`[data-role="${role}"]`);
  if (!node) {
    throw new Error(`missing element for role "${role}"`);
  }
  return node;
}

function main(): void {
  const root = document.querySelector<HTMLElement>("#app");
  if (!root) {
    throw new Error("#app container missing from index.html");
  }
  root.innerHTML = SHELL;

  const dock = pick(root, "dock");
  const runButton = pick<HTMLButtonElement>(root, "run");
  const pill = pick(root, "pill");
  const meta = pick(root, "meta");

  pick(root, "context").textContent = `${CONTEXT.database}.${CONTEXT.schema}`;
  pick(root, "shortcut").textContent = isApplePlatform() ? "⌘↵" : "Ctrl+↵";

  let running = false;
  let activeTab: "results" | "translation" = "results";
  let resultsPane: HTMLElement = renderNotice("info", "Run a statement to see results here");
  // The cached panel is keyed by the statement it describes: editing the
  // buffer must not leave a translation of something else on screen.
  let translationPane: HTMLElement | null = null;
  let translatedStatement = "";

  const editor = createEditor({
    parent: pick(root, "editor"),
    initialValue: INITIAL_SQL,
    onRun: () => void run(),
  });

  async function run(): Promise<void> {
    if (running) {
      return;
    }
    const statement = editor.statementToRun();
    if (!statement) {
      setStatus("idle", "Idle", "nothing to run");
      return;
    }

    running = true;
    runButton.disabled = true;
    setStatus("run", "Running", "submitting statement");

    try {
      const result = await runStatement(statement, CONTEXT);
      resultsPane =
        result.rowsAffected !== null
          ? renderNotice(
              "info",
              `${result.rowsAffected} ${result.rowsAffected === 1 ? "row" : "rows"} affected`,
            )
          : result.rows.length === 0
            ? renderNotice("info", "Statement returned no rows")
            : renderGrid(result);
      setStatus("ok", "Succeeded", describe(result.rows.length, result.rowsAffected, result.elapsedMs));
    } catch (cause) {
      const error = asStatementError(cause);
      resultsPane = renderNotice("error", summarise(error.message), error.message);
      setStatus("err", "Failed", `${error.code} · SQLSTATE ${error.sqlState}`);
    } finally {
      running = false;
      runButton.disabled = false;
      showTab("results");
    }
  }

  /**
   * Renders the active tab. The translation is fetched the first time it is
   * asked for rather than on every run, so previewing costs nothing until
   * somebody looks.
   */
  function showTab(tab: "results" | "translation"): void {
    activeTab = tab;
    root!.querySelectorAll<HTMLElement>(".dtab").forEach((button) => {
      button.setAttribute("aria-selected", String(button.dataset["tab"] === tab));
    });

    if (tab === "results") {
      dock.replaceChildren(resultsPane);
      return;
    }

    const statement = editor.statementToRun();
    if (!statement) {
      dock.replaceChildren(renderNotice("info", "Write a statement to see how it translates"));
      return;
    }

    if (translationPane && statement === translatedStatement) {
      dock.replaceChildren(translationPane);
      return;
    }

    dock.replaceChildren(renderNotice("info", "Translating…"));
    void translateStatement(statement, CONTEXT)
      .then((translation) => {
        translationPane = renderTranslation(translation);
        translatedStatement = statement;
        if (activeTab === "translation") {
          dock.replaceChildren(translationPane);
        }
      })
      .catch((cause: unknown) => {
        const error = asStatementError(cause);
        if (activeTab === "translation") {
          dock.replaceChildren(renderNotice("error", summarise(error.message), error.message));
        }
      });
  }

  function setStatus(state: string, label: string, detail: string): void {
    pill.className = `pill ${state}`;
    pill.textContent = label;
    meta.textContent = detail;
  }

  runButton.addEventListener("click", () => void run());
  root.querySelectorAll<HTMLElement>(".dtab").forEach((button) => {
    button.addEventListener("click", () => {
      const tab = button.dataset["tab"];
      showTab(tab === "translation" ? "translation" : "results");
    });
  });

  showTab("results");
  editor.focus();

  void showHealth(root);
}

function describe(rowCount: number, rowsAffected: number | null, elapsedMs: number): string {
  if (rowsAffected !== null) {
    return `${elapsedMs} ms`;
  }
  return `${rowCount} ${rowCount === 1 ? "row" : "rows"} · ${elapsedMs} ms`;
}

/** The emulator's messages carry the failing SQL; the first line is the gist. */
function summarise(message: string): string {
  return message.split("\n")[0] ?? "Statement failed";
}

function asStatementError(cause: unknown): StatementError {
  if (cause instanceof StatementError) {
    return cause;
  }
  const message = cause instanceof Error ? cause.message : "Request failed";
  return new StatementError("network", "", message, "");
}

async function showHealth(root: ParentNode): Promise<void> {
  const banner = root.querySelector<HTMLElement>(".conn");
  const label = pick(root, "health");
  const health = await checkHealth();

  if (banner) {
    banner.dataset["state"] = health.status;
  }
  label.textContent =
    health.status === "ok" ? "Emulator healthy" : `Emulator unreachable — ${health.detail}`;
}

function isApplePlatform(): boolean {
  return /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
}

main();
