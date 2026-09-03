import { listDatabases, listSchemas } from "./api";
import type { ExecutionContext } from "./workspace";

/**
 * Chooses the database and schema statements run in.
 *
 * The namespace is per worksheet rather than global: two worksheets often look
 * at different schemas, and carrying one context across all of them makes the
 * second one silently wrong.
 */

export interface ContextPickerOptions {
  parent: HTMLElement;
  initial: ExecutionContext;
  onChange: (context: ExecutionContext) => void;
}

export interface ContextPicker {
  /** Shows a context chosen elsewhere, as when switching worksheets. */
  set(context: ExecutionContext): void;
}

export function createContextPicker(options: ContextPickerOptions): ContextPicker {
  let context = { ...options.initial };

  const root = document.createElement("div");
  root.className = "context-picker";
  options.parent.append(root);

  const database = select("Database", () => {
    context = { database: database.value, schema: "" };
    void loadSchemas();
  });
  const schema = select("Schema", () => {
    context = { ...context, schema: schema.value };
    options.onChange({ ...context });
  });

  root.append(database.field, schema.field);

  async function loadDatabases(): Promise<void> {
    try {
      const found = await listDatabases();
      const names = found.map((entry) => entry.name);

      // A stored context can outlive what it names: the emulator runs in
      // memory by default, so a restart leaves the worksheet pointing at a
      // database that no longer exists. Falling back to a real one keeps the
      // console usable instead of leaving the picker blank.
      const chosen = names.includes(context.database)
        ? context.database
        : (names[0] ?? context.database);

      fill(database, names.length > 0 ? names : [context.database], chosen);
      context = { ...context, database: chosen };
      await loadSchemas();
    } catch {
      // The catalog is unreachable; keep the stored context selectable so the
      // worksheet still runs against what it was written for.
      fill(database, [context.database], context.database);
      fill(schema, [context.schema], context.schema);
    }
  }

  async function loadSchemas(): Promise<void> {
    if (!database.value) {
      fill(schema, [], "");
      return;
    }
    try {
      const found = await listSchemas(database.value);
      const names = found.map((entry) => entry.name);
      const chosen = names.includes(context.schema) ? context.schema : (names[0] ?? "");
      fill(schema, names, chosen);
      context = { database: database.value, schema: chosen };
      options.onChange({ ...context });
    } catch {
      fill(schema, [context.schema], context.schema);
    }
  }

  void loadDatabases();

  return {
    set(next: ExecutionContext) {
      context = { ...next };
      if (!hasOption(database, next.database)) {
        fill(database, [next.database], next.database);
      }
      database.value = next.database;
      if (!hasOption(schema, next.schema)) {
        fill(schema, [next.schema], next.schema);
      }
      schema.value = next.schema;
      void loadSchemas();
    },
  };
}

interface Field {
  field: HTMLElement;
  element: HTMLSelectElement;
  value: string;
}

function select(label: string, onChange: () => void): Field {
  const wrapper = document.createElement("label");
  wrapper.className = "ctx";

  const caption = document.createElement("span");
  caption.className = "lab";
  caption.textContent = label;

  const element = document.createElement("select");
  element.setAttribute("aria-label", label);
  element.addEventListener("change", onChange);

  wrapper.append(caption, element);

  return {
    field: wrapper,
    element,
    get value() {
      return element.value;
    },
    set value(next: string) {
      element.value = next;
    },
  };
}

function fill(field: Field, names: string[], selected: string): void {
  field.element.replaceChildren(
    ...names.filter(Boolean).map((name) => {
      const option = document.createElement("option");
      option.value = name;
      option.textContent = name;
      return option;
    }),
  );
  if (selected) {
    field.element.value = selected;
  }
}

function hasOption(field: Field, name: string): boolean {
  return [...field.element.options].some((option) => option.value === name);
}
