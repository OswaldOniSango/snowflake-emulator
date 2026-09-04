import {
  listDatabases,
  listSchemaObjects,
  listSchemas,
  uploadStageFile,
  type SchemaObject,
} from "./api";

/**
 * The object explorer: databases, their schemas, and what each schema holds.
 *
 * Levels load when they are opened rather than up front, because a schema's
 * contents cost a round trip and most of them are never looked at.
 */

interface SchemaNode {
  name: string;
  open: boolean;
  objects: SchemaObject[] | null;
  error: string | null;
  loading: boolean;
}

interface DatabaseNode {
  name: string;
  open: boolean;
  schemas: SchemaNode[] | null;
  error: string | null;
  loading: boolean;
}

export interface ExplorerOptions {
  parent: HTMLElement;
  /**
   * The namespace statements currently run in, which decides how names are
   * inserted. Read through a function rather than captured, because it changes
   * with the worksheet and with the context picker.
   */
  context: () => { database: string; schema: string };
  /** Invoked with the name to write into the editor. */
  onInsert: (name: string) => void;
}

export interface Explorer {
  /** Reloads from the catalog, discarding what is open. */
  refresh(): Promise<void>;
}

const KIND_ORDER = ["table", "stream", "procedure", "task", "stage"];

export function createExplorer(options: ExplorerOptions): Explorer {
  let databases: DatabaseNode[] = [];
  let error: string | null = null;
  let filter = "";
  let refreshing = false;
  let uploading = "";
  let feedback: { kind: "ok" | "error"; text: string } | null = null;
  let feedbackTimer: ReturnType<typeof setTimeout> | null = null;

  function setFeedback(next: typeof feedback): void {
    if (feedbackTimer !== null) {
      clearTimeout(feedbackTimer);
      feedbackTimer = null;
    }
    feedback = next;
    if (next?.kind === "ok") {
      feedbackTimer = setTimeout(() => {
        feedbackTimer = null;
        feedback = null;
        render();
      }, 5000);
    }
  }

  const root = document.createElement("div");
  root.className = "explorer";
  options.parent.append(root);

  async function refresh(): Promise<void> {
    const previous = new Map(databases.map((database) => [database.name, database]));
    refreshing = true;
    setFeedback(null);
    render();
    try {
      const found = await listDatabases();
      databases = found.map((database) => ({
        name: database.name,
        open: previous.get(database.name)?.open ?? false,
        schemas: null,
        error: null,
        loading: false,
      }));
      error = null;

      await Promise.all(
        databases
          .filter((database) => database.open)
          .map((database) => loadSchemas(database, previous.get(database.name))),
      );
    } catch (cause) {
      error = messageOf(cause);
    } finally {
      refreshing = false;
      render();
    }
  }

  async function loadSchemas(node: DatabaseNode, previous?: DatabaseNode): Promise<void> {
    node.loading = true;
    try {
      const previousSchemas = new Map(
        (previous?.schemas ?? []).map((schema) => [schema.name, schema]),
      );
      const found = await listSchemas(node.name);
      node.schemas = found.map((schema) => ({
        name: schema.name,
        open: previousSchemas.get(schema.name)?.open ?? false,
        objects: null,
        error: null,
        loading: false,
      }));
      node.error = null;

      await Promise.all(
        node.schemas
          .filter((schema) => schema.open)
          .map((schema) => loadObjects(node.name, schema)),
      );
    } catch (cause) {
      node.error = messageOf(cause);
    } finally {
      node.loading = false;
    }
  }

  async function loadObjects(database: string, node: SchemaNode): Promise<void> {
    node.loading = true;
    try {
      node.objects = await listSchemaObjects(database, node.name);
      node.error = null;
    } catch (cause) {
      node.error = messageOf(cause);
    } finally {
      node.loading = false;
    }
  }

  async function toggleDatabase(node: DatabaseNode): Promise<void> {
    node.open = !node.open;
    if (!node.open || node.schemas || node.loading) {
      render();
      return;
    }

    node.loading = true;
    render();
    await loadSchemas(node);
    render();
  }

  async function toggleSchema(database: DatabaseNode, node: SchemaNode): Promise<void> {
    node.open = !node.open;
    if (!node.open || node.objects || node.loading) {
      render();
      return;
    }

    node.loading = true;
    render();
    await loadObjects(database.name, node);
    render();
  }

  function render(): void {
    root.replaceChildren(
      header(),
      ...(feedback ? [uploadFeedback(feedback, () => { setFeedback(null); render(); })] : []),
      tree(),
    );
  }

  function header(): HTMLElement {
    const head = document.createElement("div");
    head.className = "explorer-head";

    const label = document.createElement("div");
    label.className = "eyebrow";
    label.textContent = "Objects";

    const reload = document.createElement("button");
    reload.type = "button";
    reload.className = "catalog-refresh";
    reload.textContent = "↻";
    reload.title = "Refresh objects";
    reload.setAttribute("aria-label", "Refresh objects");
    reload.disabled = refreshing;
    reload.addEventListener("click", () => void refresh());

    const title = document.createElement("div");
    title.className = "explorer-title";
    title.append(label, reload);

    const search = document.createElement("input");
    search.type = "search";
    search.placeholder = "Filter loaded objects";
    search.setAttribute("aria-label", "Filter loaded objects");
    search.value = filter;
    search.addEventListener("input", () => {
      filter = search.value;
      // Re-rendering would steal focus mid-typing, so only the list is redrawn.
      const list = root.querySelector(".tree");
      list?.replaceWith(tree());
    });

    const box = document.createElement("label");
    box.className = "filter";
    box.append(search);

    head.append(title, box);
    return head;
  }

  function tree(): HTMLElement {
    const list = document.createElement("div");
    list.className = "tree";
    list.setAttribute("role", "tree");

    if (error) {
      list.append(note(error));
      return list;
    }
    if (databases.length === 0) {
      list.append(note("No databases"));
      return list;
    }

    for (const database of databases) {
      list.append(
        row({
          label: database.name,
          kind: "database",
          depth: 0,
          expanded: database.open,
          onClick: () => void toggleDatabase(database),
        }),
      );

      if (!database.open) {
        continue;
      }
      if (database.loading) {
        list.append(note("Loading schemas…", 1));
        continue;
      }
      if (database.error) {
        list.append(note(database.error, 1));
        continue;
      }

      for (const schema of database.schemas ?? []) {
        list.append(
          row({
            label: schema.name,
            kind: "schema",
            depth: 1,
            expanded: schema.open,
            onClick: () => void toggleSchema(database, schema),
          }),
        );

        if (!schema.open) {
          continue;
        }
        if (schema.loading) {
          list.append(note("Loading objects…", 2));
          continue;
        }
        if (schema.error) {
          list.append(note(schema.error, 2));
          continue;
        }

        list.append(...objectRows(database.name, schema));
      }
    }

    return list;
  }

  function objectRows(database: string, schema: SchemaNode): HTMLElement[] {
    const matching = (schema.objects ?? []).filter((object) => matches(object.name));
    if (matching.length === 0) {
      return [note(filter ? "Nothing matches the filter" : "Empty schema", 2)];
    }

    const rows: HTMLElement[] = [];
    for (const kind of KIND_ORDER) {
      const ofKind = matching.filter((object) => object.kind === kind);
      if (ofKind.length === 0) {
        continue;
      }

      rows.push(groupLabel(plural(kind)));
      for (const object of ofKind) {
        const objectRow = row({
            label: object.name,
            kind: object.kind,
            depth: 2,
            ...(object.detail ? { detail: object.detail } : {}),
            onClick: () =>
              options.onInsert(
                nameToInsert(database, schema.name, object.name, options.context()),
              ),
          });
        if (object.kind === "stage") {
          rows.push(stageRow(objectRow, database, schema.name, object.name));
        } else {
          rows.push(objectRow);
        }
      }
    }
    return rows;
  }

  function stageRow(objectRow: HTMLElement, database: string, schema: string, stage: string): HTMLElement {
    const wrapper = document.createElement("div");
    wrapper.className = "node-line";
    wrapper.setAttribute("role", "presentation");

    const upload = document.createElement("button");
    upload.type = "button";
    upload.className = "node-action";
    upload.textContent = uploading === `${database}.${schema}.${stage}` ? "…" : "↑";
    upload.title = `Upload file to ${stage}`;
    upload.setAttribute("aria-label", `Upload file to ${stage}`);
    upload.disabled = uploading !== "";
    upload.addEventListener("click", () => chooseStageFile(database, schema, stage));

    wrapper.append(objectRow, upload);
    return wrapper;
  }

  function chooseStageFile(database: string, schema: string, stage: string): void {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".csv,.json,text/csv,application/json";
    input.addEventListener("change", () => {
      const file = input.files?.[0];
      if (file) {
        void upload(database, schema, stage, file);
      }
    });
    input.click();
  }

  async function upload(database: string, schema: string, stage: string, file: File): Promise<void> {
    uploading = `${database}.${schema}.${stage}`;
    setFeedback(null);
    render();
    try {
      const uploaded = await uploadStageFile(database, schema, stage, file, file.name);
      setFeedback({ kind: "ok", text: `${uploaded.name} uploaded to @${stage}` });
    } catch (cause) {
      setFeedback({ kind: "error", text: messageOf(cause) });
    } finally {
      uploading = "";
      render();
    }
  }

  function matches(name: string): boolean {
    return filter === "" || name.toUpperCase().includes(filter.toUpperCase());
  }

  void refresh();
  return { refresh };
}

function uploadFeedback(feedback: { kind: "ok" | "error"; text: string }, onDismiss: () => void): HTMLElement {
  const message = document.createElement("div");
  message.className = `explorer-feedback ${feedback.kind}`;
  message.setAttribute("role", feedback.kind === "error" ? "alert" : "status");

  const text = document.createElement("span");
  text.textContent = feedback.text;

  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "explorer-feedback-dismiss";
  dismiss.textContent = "×";
  dismiss.title = "Dismiss";
  dismiss.setAttribute("aria-label", "Dismiss");
  dismiss.addEventListener("click", onDismiss);

  message.append(text, dismiss);
  return message;
}

interface RowOptions {
  label: string;
  kind: string;
  depth: number;
  expanded?: boolean;
  detail?: string;
  onClick: () => void;
}

function row(options: RowOptions): HTMLElement {
  const button = document.createElement("button");
  button.className = `node lvl${options.depth}`;
  button.setAttribute("role", "treeitem");
  if (options.expanded !== undefined) {
    button.setAttribute("aria-expanded", String(options.expanded));
  }

  const twisty = document.createElement("span");
  twisty.className = "tw";
  twisty.textContent = options.expanded === undefined ? "" : "▶";

  const dot = document.createElement("span");
  dot.className = `dot kind-${options.kind}`;

  const name = document.createElement("span");
  name.className = "nm";
  name.textContent = options.label;

  button.append(twisty, dot, name);

  if (options.detail) {
    const detail = document.createElement("span");
    detail.className = "kind";
    detail.textContent = options.detail;
    button.append(detail);
  }

  button.addEventListener("click", options.onClick);
  return button;
}

function groupLabel(text: string): HTMLElement {
  const label = document.createElement("div");
  label.className = "group-label";
  label.textContent = text;
  return label;
}

function note(text: string, depth = 0): HTMLElement {
  const element = document.createElement("div");
  element.className = `tree-note lvl${depth}`;
  element.textContent = text;
  return element;
}

function plural(kind: string): string {
  return kind === "stage" ? "Stages" : `${kind.charAt(0).toUpperCase()}${kind.slice(1)}s`;
}

/**
 * Chooses the name to write into the editor.
 *
 * The emulator only resolves unqualified names: "PUBLIC.USERS" and
 * "TEST_DB.PUBLIC.USERS" both fail, because a name containing a dot is passed
 * through to DuckDB, which reads it as catalog.schema.table. So an object in
 * the current namespace is inserted bare, and one outside it keeps its full
 * name — which says plainly that it is somewhere else.
 */
export function nameToInsert(
  database: string,
  schema: string,
  object: string,
  context?: { database: string; schema: string },
): string {
  if (context && database === context.database && schema === context.schema) {
    return object;
  }
  return `${database}.${schema}.${object}`;
}

function messageOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : "Request failed";
}
