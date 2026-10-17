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
  onProjects?: () => void;
}

export interface Explorer {
  /** Reloads from the catalog, discarding what is open. */
  refresh(): Promise<void>;
}

let groupId = 0;

const KIND_ORDER = ["table", "view", "dynamic_table", "procedure", "stage", "stream", "task"];

export function createExplorer(options: ExplorerOptions): Explorer {
  let databases: DatabaseNode[] = [];
  const collapsedGroups = new Set<string>();
  let selected = "";
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
    label.textContent = "Database Explorer";

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
      collapsedGroups.clear();
      // Re-rendering would steal focus mid-typing, so only the list is redrawn.
      const list = root.querySelector(".tree");
      list?.replaceWith(tree());
    });

    const box = document.createElement("label");
    box.className = "filter";
    box.append(objectIcon("search"), search);

    if (options.onProjects) {
      const tabs = document.createElement("div");
      tabs.className = "explorer-tabs";
      const projects = document.createElement("button");
      projects.type = "button";
      projects.textContent = "Workspaces";
      projects.addEventListener("click", options.onProjects);
      const databases = document.createElement("span");
      databases.className = "explorer-active-tab";
      databases.textContent = "Databases";
      tabs.append(projects, databases);
      head.append(tabs);
    }
    const objects = document.createElement("span");
    objects.className = "explorer-objects";
    objects.textContent = "Objects";
    head.append(title, objects, box);
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

      const key = JSON.stringify([database, schema.name, kind]);
      const group = document.createElement("div");
      group.className = "object-group";
      group.setAttribute("role", "group");
      group.id = `explorer-group-${groupId++}`;
      group.hidden = collapsedGroups.has(key);
      const heading = row({
        label: plural(kind), kind, depth: 2,
        expanded: !group.hidden,
        onClick: () => {
          group.hidden = !group.hidden;
          if (group.hidden) collapsedGroups.add(key); else collapsedGroups.delete(key);
          heading.setAttribute("aria-expanded", String(!group.hidden));
        },
      });
      heading.classList.add("object-group-heading");
      heading.setAttribute("aria-controls", group.id);
      heading.querySelector(".nm")!.classList.add("group-label");
      const count = document.createElement("span");
      count.className = "object-count";
      count.textContent = String(ofKind.length);
      heading.append(count);
      rows.push(heading, group);
      for (const object of ofKind) {
        const objectRow = row({
            label: object.name,
            kind: object.kind,
            depth: 3,
            selected: selected === JSON.stringify([database, schema.name, object.kind, object.name]),
            ...(object.detail ? { detail: object.detail } : {}),
            onClick: () => {
              selected = JSON.stringify([database, schema.name, object.kind, object.name]);
              render();
              options.onInsert(
                nameToInsert(database, schema.name, object.name, options.context()),
              );
            },
          });
        if (object.kind === "stage") {
          group.append(stageRow(objectRow, database, schema.name, object.name));
        } else {
          group.append(objectRow);
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
  selected?: boolean;
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
  twisty.textContent = options.expanded === undefined ? "" : "›";
  twisty.setAttribute("aria-hidden", "true");

  const dot = objectIcon(options.kind);
  button.setAttribute("aria-level", String(options.depth + 1));
  if (options.selected !== undefined) button.setAttribute("aria-selected", String(options.selected));

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

function note(text: string, depth = 0): HTMLElement {
  const element = document.createElement("div");
  element.className = `tree-note lvl${depth}`;
  element.textContent = text;
  return element;
}

function plural(kind: string): string {
	if (kind === "dynamic_table") return "Dynamic tables";
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

// Original outline icons; no external product assets are used.
const ICON_PATHS: Record<string, string> = {
  database: "M4 6c0-4 16-4 16 0s-16 4-16 0v12c0 4 16 4 16 0V6M4 12c0 4 16 4 16 0",
  schema: "M8 4h13v12H8zM3 9h13v12H3z",
  table: "M3 4h18v16H3zM3 9h18M9 4v16",
  dynamic_table: "M3 4h18v16H3zM3 9h18M9 4v16M16 12l-2 3h4l-2 3",
  view: "M3 4h18v16H3zM7 9l3 3-3 3M13 16h4",
  procedure: "M5 5h5l4 7h5M5 19h5l4-7M3 3h4v4H3zM3 17h4v4H3zM18 10h4v4h-4z",
  stage: "M3 7h18v13H3zM8 7V4h8v3M3 12h7v3h4v-3h7",
  stream: "M3 8l4-3 4 3 4-3 6 3v4M3 12v2M3 17v2M8 19h2M13 19h2M19 19h2v-2",
  task: "M5 17l5-5 4 2 5-9M3 15h4v4H3zM8 10h4v4H8zM17 3h4v4h-4z",
  search: "M16 16l5 5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
};
function objectIcon(kind: string): SVGSVGElement {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "1.5");
  svg.setAttribute("stroke-linejoin", "round");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("aria-hidden", "true");
  svg.classList.add("object-icon", `kind-${kind}`);
  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  path.setAttribute("d", ICON_PATHS[kind] ?? ICON_PATHS["table"]!);
  svg.append(path);
  return svg;
}
