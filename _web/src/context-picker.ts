import { listDatabases, listSchemas } from "./api";
import type { ExecutionContext } from "./workspace";

export interface ContextPickerOptions {
  parent: HTMLElement;
  initial: ExecutionContext;
  onChange: (context: ExecutionContext) => void;
}

export interface ContextPicker {
  set(context: ExecutionContext): void;
}

/** Snowflake-style two-column database/schema picker. */
export function createContextPicker(options: ContextPickerOptions): ContextPicker {
  let context = { ...options.initial };
  let databases: string[] = [];
  let schemas: string[] = [];

  const root = document.createElement("div");
  root.className = "selector-shell namespace-selector";
  const trigger = document.createElement("button");
  trigger.className = "context-trigger";
  trigger.type = "button";
  trigger.setAttribute("aria-label", "Choose database and schema");
  trigger.setAttribute("aria-expanded", "false");
  const popover = document.createElement("div");
  popover.className = "selector-popover namespace-popover";
  popover.hidden = true;
  root.append(trigger, popover);
  options.parent.append(root);

  function renderTrigger(): void {
    trigger.replaceChildren(icon("database"), text(context.database || "Database"), separator(), icon("schema"), text(context.schema || "Schema"), chevron());
  }

  function renderPopover(): void {
    popover.replaceChildren(
      column("Databases", databases, context.database, async (database) => {
        context = { database, schema: "" };
        renderTrigger();
        await loadSchemas(database);
        renderPopover();
      }),
      column("Schemas", schemas, context.schema, (schema) => {
        context = { ...context, schema };
        renderTrigger();
        close();
        options.onChange({ ...context });
      }),
    );
  }

  async function loadDatabases(): Promise<void> {
    try {
      databases = (await listDatabases()).map((entry) => entry.name);
      const chosen = databases.includes(context.database) ? context.database : (databases[0] ?? context.database);
      context = { ...context, database: chosen };
      await loadSchemas(chosen);
      renderTrigger();
      if (!popover.hidden) renderPopover();
    } catch {
      databases = context.database ? [context.database] : [];
      schemas = context.schema ? [context.schema] : [];
    }
  }

  async function loadSchemas(database: string): Promise<void> {
    if (!database) {
      schemas = [];
      return;
    }
    try {
      schemas = (await listSchemas(database)).map((entry) => entry.name);
      const chosen = schemas.includes(context.schema) ? context.schema : (schemas[0] ?? "");
      context = { database, schema: chosen };
      options.onChange({ ...context });
    } catch {
      schemas = context.schema ? [context.schema] : [];
    }
  }

  function close(): void {
    popover.hidden = true;
    trigger.setAttribute("aria-expanded", "false");
  }

  trigger.addEventListener("click", () => {
    const opening = popover.hidden;
    if (opening) closeOtherSelectors(popover);
    popover.hidden = !opening;
    trigger.setAttribute("aria-expanded", String(!popover.hidden));
    if (!popover.hidden) renderPopover();
  });
  root.addEventListener("keydown", (event) => {
    if (event.key === "Escape") close();
  });

  renderTrigger();
  void loadDatabases();

  return {
    set(next: ExecutionContext) {
      context = { ...next };
      renderTrigger();
      void loadSchemas(next.database);
    },
  };
}

function column(title: string, names: string[], selected: string, choose: (name: string) => void | Promise<void>): HTMLElement {
  const section = document.createElement("section");
  section.className = "selector-column";
  const search = document.createElement("input");
  search.type = "search";
  search.placeholder = title;
  search.setAttribute("aria-label", `Search ${title.toLowerCase()}`);
  const list = document.createElement("div");
  list.className = "selector-list";

  const render = (): void => {
    const query = search.value.trim().toLowerCase();
    const filtered = names.filter((name) => name.toLowerCase().includes(query));
    list.replaceChildren(...filtered.map((name) => option(name, name === selected, () => void choose(name))));
    if (filtered.length === 0) list.append(empty(`No ${title.toLowerCase()} found`));
  };
  search.addEventListener("input", render);
  render();
  section.append(search, list);
  return section;
}

function option(name: string, selected: boolean, choose: () => void): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "selector-option";
  button.setAttribute("aria-current", selected ? "true" : "false");
  button.append(icon("item"), text(name));
  if (selected) button.append(check());
  button.addEventListener("click", choose);
  return button;
}

function icon(kind: string): HTMLElement {
  const element = document.createElement("span");
  element.className = `selector-icon ${kind}`;
  element.setAttribute("aria-hidden", "true");
  return element;
}

function text(value: string): Text {
  return document.createTextNode(value);
}

function separator(): HTMLElement {
  const element = document.createElement("span");
  element.className = "context-separator";
  element.textContent = "·";
  return element;
}

function chevron(): HTMLElement {
  const element = document.createElement("span");
  element.className = "selector-chevron";
  element.textContent = "⌄";
  return element;
}

function check(): HTMLElement {
  const element = document.createElement("span");
  element.className = "selector-check";
  element.textContent = "✓";
  return element;
}

function empty(message: string): HTMLElement {
  const element = document.createElement("p");
  element.className = "selector-empty";
  element.textContent = message;
  return element;
}

function closeOtherSelectors(current: HTMLElement): void {
  document.querySelectorAll<HTMLElement>(".selector-popover").forEach((popover) => {
    if (popover !== current) popover.hidden = true;
  });
  document.querySelectorAll<HTMLElement>('.context-trigger[aria-expanded="true"]').forEach((button) => {
    if (button.nextElementSibling !== current) button.setAttribute("aria-expanded", "false");
  });
}
