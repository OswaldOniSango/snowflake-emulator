import { createContextPicker } from "./context-picker";
import { createWarehousePicker } from "./warehouse-picker";
import { listSchemaObjects, uploadStageFile } from "./api";
import type { ExecutionContext, Workspace } from "./workspace";

/** Browser-local projects; never imply server persistence or invented timestamps. */
export function renderProjects(parent: HTMLElement, workspace: Workspace, onOpen: (id: string) => void, onNew: () => void): void {
  parent.innerHTML = `<section class="view projects-view"><div class="view-head"><div><h1>Recent projects</h1><p>SQL files saved in this browser</p></div><button class="run">+ SQL worksheet</button></div><div class="project-tabs">All projects <span>SQL files</span></div><div class="tablewrap"><table class="grid"><thead><tr><th>Title</th><th>Type</th><th>Database</th><th>Schema</th></tr></thead><tbody></tbody></table></div></section>`;
  parent.querySelector("button")!.addEventListener("click", onNew);
  const body = parent.querySelector("tbody")!;
  for (const worksheet of workspace.worksheets) {
    const row = document.createElement("tr");
    const title = document.createElement("td");
    const open = document.createElement("button");
    open.className = "project-link";
    open.textContent = `▤  ${worksheet.name}`;
    open.addEventListener("click", () => onOpen(worksheet.id));
    title.append(open);
    row.append(title);
    for (const value of ["SQL File", worksheet.context.database, worksheet.context.schema]) {
      const cell = document.createElement("td");
      cell.textContent = value;
      row.append(cell);
    }
    body.append(row);
  }
}

export function createIngestionView(parent: HTMLElement, context: ExecutionContext, onOpen: (sql: string, context: ExecutionContext) => void): void {
  parent.innerHTML = `<section class="view ingestion-view"><div class="view-head"><div><h1>Add data</h1><p>Bring your data into Mallard</p></div></div><div class="ingestion-cards"><button class="ingestion-card" data-load="table"><span aria-hidden="true">↥</span><b>Load data into a Table</b><small>Upload CSV, then review COPY INTO in a worksheet</small></button><button class="ingestion-card" data-load="stage"><span aria-hidden="true">▱</span><b>Load files into a Stage</b><small>Upload to an existing internal stage</small></button></div><p class="ingestion-note">External cloud storage connectors are not available in this emulator.</p></section>`;
  parent.querySelectorAll<HTMLButtonElement>("[data-load]").forEach((button) => {
    button.addEventListener("click", () => openUploadDialog(context, button.dataset["load"] === "table", onOpen));
  });
}

function openUploadDialog(context: ExecutionContext, tableMode: boolean, onOpen: (sql: string, context: ExecutionContext) => void): void {
  const dialog = document.createElement("dialog");
  dialog.className = "load-dialog";
  dialog.setAttribute("aria-labelledby", "load-title");
  dialog.innerHTML = `<form><div class="load-dialog-heading"><h2 id="load-title">${tableMode ? "Load Data into Table" : "Load files into a Stage"}</h2><div class="load-warehouse"></div></div><p>Choose an existing database, schema and internal stage.</p><label class="upload-zone">↥<strong>Choose a file to upload</strong><input name="file" type="file" required ${tableMode ? 'accept=".csv,text/csv"' : ""}><small>Maximum file size: 63 MB${tableMode ? " · CSV with a header row" : ""}</small></label><div class="load-fields"><div class="load-namespace"><span>Select a database and schema</span><div class="load-namespace-picker"></div><input type="hidden" name="database"><input type="hidden" name="schema"></div><label>Internal stage<input name="stage" required placeholder="MY_STAGE"></label>${tableMode ? '<label class="load-table-choice">Select or create a table<select name="tableChoice" aria-label="Select or create a table"><option value="">+ Create new table</option></select><small class="table-catalog-status"></small></label><label class="load-table-name">Name<input name="table" required placeholder="MY_TABLE"></label>' : ""}</div><p class="load-status" role="status"></p><div class="load-actions"><button type="button" class="ghost">Cancel</button><button type="submit" class="run">${tableMode ? "Upload & prepare SQL" : "Upload"}</button></div></form>`;
  const form = dialog.querySelector("form")!;
  const field = (name: string): HTMLInputElement => form.elements.namedItem(name) as HTMLInputElement;
  field("database").value = context.database;
  field("schema").value = context.schema;
  createWarehousePicker(dialog.querySelector<HTMLElement>(".load-warehouse")!);
  let catalogVersion = 0;
  const choice = form.elements.namedItem("tableChoice") as HTMLSelectElement | null;
  const nameLabel = form.querySelector<HTMLElement>(".load-table-name");
  choice?.addEventListener("change", () => {
    nameLabel!.hidden = choice.value !== "";
    field("table").required = choice.value === "";
  });
  async function refreshTables(next: ExecutionContext): Promise<void> {
    if (!choice) return;
    const version = ++catalogVersion;
    choice.replaceChildren(tableOption("+ Create new table", ""));
    nameLabel!.hidden = false;
    field("table").required = true;
    const note = form.querySelector<HTMLElement>(".table-catalog-status")!;
    note.textContent = "Loading tables…";
    try {
      const objects = await listSchemaObjects(next.database, next.schema);
      if (version !== catalogVersion) return;
      for (const object of objects.filter((object) => object.kind.toLowerCase() === "table")) {
        choice.append(tableOption(object.name, object.name));
      }
      note.textContent = "";
    } catch {
      if (version === catalogVersion) note.textContent = "Could not load existing tables. You can still prepare a new table.";
    }
  }
  void refreshTables(context);
  createContextPicker({
    parent: dialog.querySelector<HTMLElement>(".load-namespace-picker")!, initial: context, qualifiedLabel: true,
    onChange: (next) => { field("database").value = next.database; field("schema").value = next.schema; void refreshTables(next); },
  });
  const cancel = dialog.querySelector<HTMLButtonElement>('.load-actions button[type="button"]')!;
  const submit = dialog.querySelector<HTMLButtonElement>('button[type="submit"]')!;
  const status = dialog.querySelector<HTMLElement>(".load-status")!;
  let uploading = false;
  cancel.addEventListener("click", () => dialog.close());
  dialog.addEventListener("cancel", (event) => { if (uploading) event.preventDefault(); });
  dialog.addEventListener("close", () => dialog.remove());
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    if (uploading) return;
    const file = field("file").files?.[0];
    if (!file) return;
    if (file.size > 63 * 1024 * 1024) { status.textContent = "Choose a file smaller than 63 MB."; return; }
    if (tableMode && !file.name.toLowerCase().endsWith(".csv")) { status.textContent = "Table loading currently supports CSV files only."; return; }
    const next = { database: field("database").value.trim(), schema: field("schema").value.trim() };
    const stage = field("stage").value.trim();
    const table = tableMode ? (choice?.value || field("table").value.trim()) : "";
    const creating = tableMode && !choice?.value;
    if (!next.database || !next.schema || !stage || (tableMode && !table)) { status.textContent = "Complete all destination fields."; return; }
    if (tableMode && (![next.database, next.schema, stage, table].every((name) => /^[A-Za-z_][A-Za-z0-9_$]*$/.test(name)) || !/^[A-Za-z0-9_.-]+$/.test(file.name))) {
      status.textContent = "SQL preparation requires simple identifiers and a filename containing only letters, numbers, dots, hyphens or underscores.";
      return;
    }
    uploading = true;
    submit.disabled = cancel.disabled = true;
    status.textContent = "Uploading…";
    void (async () => {
      const columns = creating ? csvColumns(await file.text()) : [];
      const uploaded = await uploadStageFile(next.database, next.schema, stage, file, file.name);
      return { uploaded, columns };
    })().then(({ uploaded, columns }) => {
      if (tableMode) {
        const namespace = `${next.database.toUpperCase()}.${next.schema.toUpperCase()}.`;
        // Stage filtering currently uses filepath.Match glob semantics, not regex.
        const filename = file.name;
        const create = creating ? `CREATE TABLE ${namespace}${table.toUpperCase()} (\n  ${columns.map((column) => `${column} VARCHAR`).join(",\n  ")}\n);\n\n` : "";
        onOpen(`-- Review before running. ${creating ? "Run all to create the table and load the CSV. New columns use VARCHAR." : "The destination table must already exist."}\n${create}COPY INTO ${namespace}${table.toUpperCase()}\nFROM @${namespace}${stage.toUpperCase()}\nPATTERN = '${filename}'\nFILE_FORMAT = (TYPE = CSV SKIP_HEADER = 1);`, next);
        dialog.close();
      } else {
        status.textContent = `${uploaded.name} uploaded to ${stage}.`;
      }
    }).catch((error: unknown) => {
      status.textContent = error instanceof Error ? error.message : "Upload failed. Please try again.";
    }).finally(() => {
      uploading = false;
      submit.disabled = cancel.disabled = false;
    });
  });
  document.body.append(dialog);
  dialog.showModal();
}

/** Read the first CSV record, including escaped quotes and embedded newlines. */
export function csvColumns(csv: string): string[] {
  const columns: string[] = [];
  let value = "";
  let quoted = false;
  const text = csv.replace(/^\uFEFF/, "");
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch === '"') {
      if (quoted && text[i + 1] === '"') { value += '"'; i++; }
      else if (quoted || value === "") quoted = !quoted;
      else throw new Error("Invalid CSV header.");
    } else if (!quoted && (ch === "," || ch === "\n" || ch === "\r")) {
      columns.push(value.trim()); value = "";
      if (ch !== ",") break;
    } else value += ch;
    if (i === text.length - 1) columns.push(value.trim());
  }
  if (quoted || !columns.length || columns.some((column) => !column || /[\r\n]/.test(column))) throw new Error("CSV needs a non-empty header for every column.");
  const normalized = columns.map((column) => column.toUpperCase());
  if (new Set(normalized).size !== normalized.length) throw new Error("CSV column names must be unique.");
  if (normalized.some((column) => !/^[A-Za-z_][A-Za-z0-9_$]*$/.test(column))) throw new Error("New tables currently require simple CSV column names: letters, numbers, underscores or dollar signs, starting with a letter or underscore.");
  return normalized;
}

function tableOption(label: string, value: string): HTMLOptionElement {
  const option = document.createElement("option");
  option.textContent = label; option.value = value;
  return option;
}
