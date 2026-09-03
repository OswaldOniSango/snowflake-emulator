import "./style.css";
import { runStatement, StatementError, translateStatement, type Statement } from "./api";
import { createEditor, type Editor } from "./editor";
import { createExplorer } from "./explorer";
import { renderGrid, renderNotice } from "./grid";
import { checkHealth } from "./health";
import { createContextPicker } from "./context-picker";
import { changesCatalog, createCatalog } from "./catalog";
import { createCompletionSource } from "./completion";
import { createHistoryView } from "./history";
import { createWarehousesView } from "./warehouses";
import { createLimitationsButton } from "./limitations";
import { createThemeToggle } from "./theme";
import { splitStatements, statementAt } from "./statements";
import { renderTranslation } from "./translation";
import {
  loadWorkspace,
  nextWorksheetName,
  newWorksheet,
  saveWorkspace,
  type ExecutionContext,
  type Workspace,
  type Worksheet,
} from "./workspace";

// Static, author-controlled markup. Everything derived from data is built with
// createElement and textContent — see grid.ts and explorer.ts.
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
  <nav class="nav" data-role="nav">
    <button data-view="worksheets" aria-current="page">Worksheets</button>
    <button data-view="warehouses">Warehouses</button>
    <button data-view="history">History</button>
  </nav>
  <div class="spacer"></div>
  <div class="conn" data-state="pending" role="status">
    <span class="dot"></span><span data-role="health">Checking emulator…</span>
  </div>
  <div data-role="theme"></div>
</header>

<main class="workspace" data-view-pane="worksheets">
  <aside class="sidebar" data-role="sidebar"></aside>

  <div class="center">
    <div class="tabstrip" data-role="tabs" role="tablist" aria-label="Open worksheets"></div>

    <div class="ctxbar">
      <div data-role="context"></div>
      <button class="run" data-role="run">Run <kbd data-role="shortcut"></kbd></button>
      <button class="ghost" data-role="run-all" hidden></button>
    </div>

    <section class="editor" data-role="editor"></section>

    <div class="toolbar">
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
  </div>
</main>

<div class="view-pane" data-view-pane="warehouses" hidden></div>
<div class="view-pane" data-view-pane="history" hidden></div>

<footer class="footer">
  <span>Not affiliated with or endorsed by Snowflake Inc. Snowflake is a trademark of Snowflake Inc.</span>
  <span data-role="limitations"></span>
</footer>`;

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
  const tabstrip = pick(root, "tabs");
  const runButton = pick<HTMLButtonElement>(root, "run");
  const runAllButton = pick<HTMLButtonElement>(root, "run-all");
  const pill = pick(root, "pill");
  const meta = pick(root, "meta");

  const workspace: Workspace = loadWorkspace();
  let activeTab: "results" | "translation" = "results";
  let resultsPane: HTMLElement = renderNotice("info", "Run a statement to see results here");
  let translationPane: HTMLElement | null = null;
  let translatedStatement = "";
  let running = false;

  pick(root, "shortcut").textContent = isApplePlatform() ? "⌘↵" : "Ctrl+↵";
  pick(root, "theme").append(createThemeToggle());
  pick(root, "limitations").append(createLimitationsButton());

  // Completion reads whatever the cache last loaded, so a namespace that has
  // not arrived yet degrades to keywords rather than blocking a keystroke.
  const catalog = createCatalog();

  const editor: Editor = createEditor({
    parent: pick(root, "editor"),
    initialValue: active().sql,
    completions: createCompletionSource(catalog),
    onRun: () => void run(),
    onChange: (value) => {
      active().sql = value;
      persist();
      updateRunAll();
    },
  });

  const contextPicker = createContextPicker({
    parent: pick(root, "context"),
    initial: active().context,
    onChange: (context) => {
      active().context = context;
      persist();
      syncCatalog();
      // The translation depends on the namespace, so it is no longer current.
      translationPane = null;
      if (activeTab === "translation") {
        showTab("translation");
      }
    },
  });

  createExplorer({
    parent: pick(root, "sidebar"),
    context: () => active().context,
    onInsert: (name) => editor.insert(name),
  });

  /**
   * Points the completion cache at the active worksheet's namespace. Cheap to
   * call from every path that changes which worksheet or context is active:
   * loading a namespace already cached does nothing.
   */
  function syncCatalog(): void {
    const context = active().context;
    void catalog.load(context.database, context.schema);
  }

  function active(): Worksheet {
    const worksheet = workspace.worksheets.find((w) => w.id === workspace.activeId);
    if (!worksheet) {
      throw new Error("the active worksheet is missing from the workspace");
    }
    return worksheet;
  }

  function persist(): void {
    saveWorkspace(workspace);
  }

  /** What Run will submit: the selection, or the statement around the cursor. */
  function statementToRun(): string {
    const selected = editor.selection();
    if (selected) {
      return selected;
    }
    return statementAt(editor.value(), editor.cursorOffset())?.text ?? "";
  }

  function updateRunAll(): void {
    const count = splitStatements(editor.value()).length;
    runAllButton.hidden = count < 2;
    runAllButton.textContent = `Run all ${count}`;
  }

  async function run(): Promise<void> {
    await execute([statementToRun()].filter(Boolean));
  }

  async function runAll(): Promise<void> {
    await execute(splitStatements(editor.value()).map((statement) => statement.text));
  }

  /**
   * Submits statements one at a time, because the API takes one per request.
   * A failure stops the run: the statements after it were written expecting the
   * earlier ones to have happened.
   */
  async function execute(statements: string[]): Promise<void> {
    if (running || statements.length === 0) {
      if (statements.length === 0) {
        setStatus("idle", "Idle", "nothing to run");
      }
      return;
    }

    running = true;
    runButton.disabled = true;
    runAllButton.disabled = true;
    setStatus("run", "Running", statements.length === 1 ? "submitting statement" : `1 of ${statements.length}`);

    const context = active().context;
    let elapsed = 0;

    try {
      for (const [index, statement] of statements.entries()) {
        if (statements.length > 1) {
          setStatus("run", "Running", `${index + 1} of ${statements.length}`);
        }

        const result = await runStatement(statement, context);
        elapsed += result.elapsedMs;

        // A statement that created or dropped an object has just invalidated
        // what completion is offering.
        if (changesCatalog(statement)) {
          void catalog.refresh();
        }

        resultsPane =
          result.rowsAffected !== null
            ? renderNotice(
                "info",
                `${result.rowsAffected} ${result.rowsAffected === 1 ? "row" : "rows"} affected`,
              )
            : result.rows.length === 0
              ? renderNotice("info", "Statement returned no rows")
              : withTruncationNotice(result);

        setStatus(
          "ok",
          "Succeeded",
          summary(statements.length, index + 1, result, elapsed),
        );
      }
    } catch (cause) {
      const error = asStatementError(cause);
      resultsPane = renderNotice("error", firstLine(error.message), error.message);
      setStatus("err", "Failed", `${error.code} · SQLSTATE ${error.sqlState}`);
    } finally {
      running = false;
      runButton.disabled = false;
      runAllButton.disabled = false;
      showTab("results");
    }
  }

  function setStatus(state: string, label: string, detail: string): void {
    pill.className = `pill ${state}`;
    pill.textContent = label;
    meta.textContent = detail;
  }

  function showTab(tab: "results" | "translation"): void {
    activeTab = tab;
    root!.querySelectorAll<HTMLElement>(".dtab").forEach((button) => {
      button.setAttribute("aria-selected", String(button.dataset["tab"] === tab));
    });

    if (tab === "results") {
      dock.replaceChildren(resultsPane);
      return;
    }

    const statement = statementToRun();
    if (!statement) {
      dock.replaceChildren(renderNotice("info", "Write a statement to see how it translates"));
      return;
    }
    if (translationPane && statement === translatedStatement) {
      dock.replaceChildren(translationPane);
      return;
    }

    dock.replaceChildren(renderNotice("info", "Translating…"));
    void translateStatement(statement, active().context)
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
          dock.replaceChildren(renderNotice("error", firstLine(error.message), error.message));
        }
      });
  }

  // --- worksheets ---

  /** Output belongs to a worksheet, so moving to another one clears it. */
  function resetOutput(): void {
    translationPane = null;
    translatedStatement = "";
    resultsPane = renderNotice("info", "Run a statement to see results here");
    setStatus("idle", "Idle", "no statement submitted");
  }

  function switchTo(id: string): void {
    if (id === workspace.activeId) {
      return;
    }
    workspace.activeId = id;
    editor.setValue(active().sql);
    contextPicker.set(active().context);
    syncCatalog();
    resetOutput();
    persist();
    renderTabs();
    updateRunAll();
    showTab("results");
    editor.focus();
  }

  function addWorksheet(): void {
    const worksheet = newWorksheet(nextWorksheetName(workspace.worksheets), active().context);
    workspace.worksheets.push(worksheet);
    workspace.activeId = worksheet.id;
    editor.setValue("");
    contextPicker.set(worksheet.context);
    syncCatalog();
    resetOutput();
    persist();
    renderTabs();
    updateRunAll();
    showTab("results");
    editor.focus();
  }

  function closeWorksheet(id: string): void {
    const index = workspace.worksheets.findIndex((worksheet) => worksheet.id === id);
    if (index < 0) {
      return;
    }

    workspace.worksheets.splice(index, 1);
    if (workspace.worksheets.length === 0) {
      // Closing the last one leaves an empty worksheet rather than no console.
      const replacement = newWorksheet("Worksheet 1");
      workspace.worksheets.push(replacement);
      workspace.activeId = replacement.id;
    } else if (workspace.activeId === id) {
      const neighbour = workspace.worksheets[Math.max(0, index - 1)] as Worksheet;
      workspace.activeId = neighbour.id;
    }

    editor.setValue(active().sql);
    contextPicker.set(active().context);
    syncCatalog();
    resetOutput();
    persist();
    renderTabs();
    updateRunAll();
    showTab("results");
  }

  function renameWorksheet(worksheet: Worksheet, tab: HTMLElement): void {
    const input = document.createElement("input");
    input.className = "tab-rename";
    input.value = worksheet.name;
    input.setAttribute("aria-label", "Worksheet name");

    const commit = (): void => {
      const name = input.value.trim();
      if (name) {
        worksheet.name = name;
        persist();
      }
      renderTabs();
    };

    input.addEventListener("blur", commit);
    input.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        commit();
      } else if (event.key === "Escape") {
        renderTabs();
      }
    });

    tab.replaceChildren(input);
    input.focus();
    input.select();
  }

  function renderTabs(): void {
    tabstrip.replaceChildren();

    for (const worksheet of workspace.worksheets) {
      const tab = document.createElement("div");
      tab.className = "wtab";
      tab.setAttribute("role", "tab");
      tab.setAttribute("aria-selected", String(worksheet.id === workspace.activeId));

      const name = document.createElement("button");
      name.className = "wtab-name";
      name.textContent = worksheet.name;
      name.title = "Double-click to rename";
      name.addEventListener("click", () => switchTo(worksheet.id));
      name.addEventListener("dblclick", () => renameWorksheet(worksheet, tab));

      const close = document.createElement("button");
      close.className = "wtab-close";
      close.textContent = "×";
      close.setAttribute("aria-label", `Close ${worksheet.name}`);
      close.addEventListener("click", (event) => {
        event.stopPropagation();
        closeWorksheet(worksheet.id);
      });

      tab.append(name, close);
      tabstrip.append(tab);
    }

    const add = document.createElement("button");
    add.className = "newtab";
    add.textContent = "+";
    add.setAttribute("aria-label", "New worksheet");
    add.addEventListener("click", addWorksheet);
    tabstrip.append(add);
  }

  // The secondary views cost a request each, so they are built the first time
  // they are opened rather than on load.
  const views = new Map<string, { refresh: () => Promise<void> }>();

  function showView(name: string): void {
    root!.querySelectorAll<HTMLElement>("[data-view-pane]").forEach((pane) => {
      pane.hidden = pane.dataset["viewPane"] !== name;
    });
    root!.querySelectorAll<HTMLElement>("[data-view]").forEach((button) => {
      if (button.dataset["view"] === name) {
        button.setAttribute("aria-current", "page");
      } else {
        button.removeAttribute("aria-current");
      }
    });

    if (name === "worksheets") {
      return;
    }

    const pane = root!.querySelector<HTMLElement>(`[data-view-pane="${name}"]`);
    if (!pane) {
      return;
    }

    const existing = views.get(name);
    if (existing) {
      void existing.refresh();
      return;
    }

    views.set(
      name,
      name === "warehouses"
        ? createWarehousesView(pane)
        : createHistoryView({
            parent: pane,
            onOpen: (entry) => {
              openInWorksheet(entry.statement, {
                database: entry.database || active().context.database,
                schema: entry.schema || active().context.schema,
              });
              showView("worksheets");
            },
          }),
    );
  }

  /** Reopens a statement from the history in a worksheet of its own. */
  function openInWorksheet(sql: string, context: ExecutionContext): void {
    const worksheet = newWorksheet(nextWorksheetName(workspace.worksheets), context, sql);
    workspace.worksheets.push(worksheet);
    workspace.activeId = worksheet.id;
    editor.setValue(sql);
    contextPicker.set(context);
    syncCatalog();
    resetOutput();
    persist();
    renderTabs();
    updateRunAll();
    showTab("results");
    editor.focus();
  }

  pick(root, "nav").addEventListener("click", (event) => {
    const button = (event.target as HTMLElement).closest<HTMLElement>("[data-view]");
    if (button?.dataset["view"]) {
      showView(button.dataset["view"]);
    }
  });

  runButton.addEventListener("click", () => void run());
  runAllButton.addEventListener("click", () => void runAll());
  root.querySelectorAll<HTMLElement>(".dtab").forEach((button) => {
    button.addEventListener("click", () => {
      showTab(button.dataset["tab"] === "translation" ? "translation" : "results");
    });
  });

  renderTabs();
  updateRunAll();
  showTab("results");
  syncCatalog();
  editor.focus();
  void showHealth(root);
}

function summary(total: number, done: number, result: Statement, elapsedMs: number): string {
  const of = total > 1 ? `${done} of ${total} · ` : "";
  if (result.rowsAffected !== null) {
    return `${of}${elapsedMs} ms`;
  }

  // A truncated result says both numbers, so a slice never reads as the whole
  // answer.
  const shown = result.rows.length;
  const rows =
    result.totalRows > shown
      ? `${format(shown)} of ${format(result.totalRows)} rows · `
      : `${format(shown)} ${shown === 1 ? "row" : "rows"} · `;
  return `${of}${rows}${elapsedMs} ms`;
}

/** Pairs a truncated grid with a note saying how to see the rest. */
function withTruncationNotice(result: Statement): HTMLElement {
  const grid = renderGrid(result);
  if (result.totalRows <= result.rows.length) {
    return grid;
  }

  const wrapper = document.createElement("div");
  wrapper.append(
    renderNotice(
      "info",
      `Showing the first ${format(result.rows.length)} of ${format(result.totalRows)} rows`,
      "The emulator caps how many rows one statement returns so a large result cannot outgrow the browser. Add a LIMIT to choose which rows you want.",
    ),
    grid,
  );
  return wrapper;
}

function format(count: number): string {
  return count.toLocaleString();
}

/** The emulator's messages carry the failing SQL; the first line is the gist. */
function firstLine(message: string): string {
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

export type { ExecutionContext };

main();
