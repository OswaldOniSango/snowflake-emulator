import { runStatement } from "./api";
import { session } from "./auth";
import type { ExecutionContext } from "./workspace";

/** Small SQL-driven identity console until dedicated identity REST endpoints exist. */
export function createIdentityAdminView(parent: HTMLElement, context: () => ExecutionContext): { refresh: () => Promise<void> } {
  const root = document.createElement("div");
  root.className = "view identity-view";
  parent.append(root);

  async function refresh(): Promise<void> {
    root.replaceChildren();
    const current = session();
    if (!current) {
      root.append(notice("Sign in to manage users, roles and grants."));
      return;
    }
    if (!["ACCOUNTADMIN", "USERADMIN", "SECURITYADMIN"].includes(current.role)) {
      root.append(notice(`Role ${current.role} cannot administer identities.`));
      return;
    }
    const heading = document.createElement("div");
    heading.className = "view-head";
    const title = document.createElement("div");
    const h1 = document.createElement("h1");
    h1.textContent = "Identity administration";
    const p = document.createElement("p");
    p.textContent = "These controls submit Snowflake-style SQL through the authenticated worksheet session.";
    title.append(h1, p);
    heading.append(title);
    root.append(heading, forms());
  }

  function forms(): HTMLElement {
    const grid = document.createElement("div");
    grid.className = "identity-admin-grid";
    grid.append(
      form(context, "Create user", [field("Username"), field("Password", "password"), field("Default role", "text", "PUBLIC")], (values) =>
        `CREATE USER ${identifier(values[0] ?? "")} PASSWORD = '${literal(values[1] ?? "")}' DEFAULT_ROLE = ${identifier(values[2] || "PUBLIC")}`),
      form(context, "Create role", [field("Role name")], (values) => `CREATE ROLE ${identifier(values[0] ?? "")}`),
      form(context, "Grant role", [field("Child role"), field("Grantee role/user")], (values) =>
        `GRANT ROLE ${identifier(values[0] ?? "")} TO ROLE ${identifier(values[1] ?? "")}`),
      form(context, "Grant privilege", [field("Privilege"), field("Object", "text", "TABLE my_table"), field("Role")], (values) =>
        `GRANT ${(values[0] ?? "").toUpperCase()} ON ${values[1] ?? ""} TO ROLE ${identifier(values[2] ?? "")}`),
      form(context, "Revoke privilege", [field("Privilege"), field("Object", "text", "TABLE my_table"), field("Role")], (values) =>
        `REVOKE ${(values[0] ?? "").toUpperCase()} ON ${values[1] ?? ""} FROM ROLE ${identifier(values[2] ?? "")}`),
    );
    return grid;
  }

  void refresh();
  return { refresh };
}

/** SHOW ROLES returns its role name in the second column. */
export function roleNamesFromRows(rows: unknown[][], currentRole: string): string[] {
  const roles = rows.map((row) => String(row[1] ?? row[0] ?? "")).filter(Boolean);
  return [...new Set([currentRole, ...roles])];
}

function form(context: () => ExecutionContext, title: string, fields: HTMLInputElement[], sql: (values: string[]) => string): HTMLElement {
  const article = document.createElement("form");
  article.className = "identity-card";
  const heading = document.createElement("h2");
  heading.textContent = title;
  const output = document.createElement("small");
  output.className = "identity-output";
  const submit = document.createElement("button");
  submit.className = "ghost";
  submit.type = "submit";
  submit.textContent = "Execute";
  article.append(heading, ...fields, submit, output);
  article.addEventListener("submit", (event) => {
    event.preventDefault();
    if (fields.some((item) => !item.value.trim())) {
      output.textContent = "Complete all fields.";
      return;
    }
    const values = fields.map((item) => item.value.trim());
    if (title.includes("privilege") && (!privilegeNames.has((values[0] ?? "").toUpperCase()) || !objectName(values[1] ?? "") || !safeName(values[2] ?? ""))) {
      output.textContent = "Use a supported privilege and object name.";
      return;
    }
    if (title === "Grant role" && (!safeName(values[0] ?? "") || !safeName(values[1] ?? ""))) {
      output.textContent = "Role names must be simple identifiers.";
      return;
    }
    submit.disabled = true;
    output.textContent = "Running…";
    void runStatement(sql(values), context())
      .then(() => { output.textContent = "Succeeded"; })
      .catch((cause: unknown) => { output.textContent = cause instanceof Error ? cause.message : "Statement failed"; })
      .finally(() => { submit.disabled = false; });
  });
  return article;
}

function field(label: string, type = "text", placeholder = label): HTMLInputElement {
  const input = document.createElement("input");
  input.type = type;
  input.placeholder = placeholder;
  input.setAttribute("aria-label", label);
  return input;
}

function identifier(value: string): string {
  return `"${value.replaceAll('"', '""')}"`;
}

function literal(value: string): string {
  return value.replaceAll("'", "''");
}

const privilegeNames = new Set(["USAGE", "OPERATE", "SELECT", "INSERT", "UPDATE", "DELETE", "CREATE TABLE"]);

export function isSupportedPrivilege(value: string): boolean {
  return privilegeNames.has(value.toUpperCase());
}

export function safeName(value: string): boolean {
  return /^[A-Za-z_][A-Za-z0-9_$]*$/.test(value);
}

export function objectName(value: string): boolean {
  const match = /^(DATABASE|SCHEMA|TABLE|WAREHOUSE)\s+([A-Za-z_][A-Za-z0-9_$]*(?:\.[A-Za-z_][A-Za-z0-9_$]*)*)$/i.exec(value);
  return Boolean(match && match[2]?.split(".").every(safeName));
}

function notice(message: string): HTMLElement {
  const element = document.createElement("p");
  element.className = "notice info";
  element.textContent = message;
  return element;
}
