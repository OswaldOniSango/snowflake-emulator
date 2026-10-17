import { afterEach, describe, expect, it, vi } from "vitest";
import { createIngestionView, csvColumns, renderProjects } from "./console-views";
import { defaultWorkspace } from "./workspace";

const context = { database: "TEST_DB", schema: "PUBLIC" };
const settle = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));
afterEach(() => { document.body.replaceChildren(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
function host(): HTMLElement { const parent = document.createElement("div"); document.body.append(parent); return parent; }
function upload(table = false): HTMLFormElement {
  vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(function (this: HTMLDialogElement) { this.open = true; });
  vi.spyOn(HTMLDialogElement.prototype, "close").mockImplementation(function (this: HTMLDialogElement) { this.dispatchEvent(new Event("close")); });
  const parent = host();
  createIngestionView(parent, context, opened);
  parent.querySelector<HTMLButtonElement>(`[data-load="${table ? "table" : "stage"}"]`)!.click();
  const form = document.querySelector("dialog form") as HTMLFormElement;
  (form.elements.namedItem("stage") as HTMLInputElement).value = "MY_STAGE";
  if (table) (form.elements.namedItem("table") as HTMLInputElement).value = "MY_TABLE";
  return form;
}
const opened = vi.fn();
function file(form: HTMLFormElement, name = "data.csv", size = 10): void {
  const chosen = new File(["id\n1\n"], name);
  Object.defineProperty(chosen, "size", { value: size });
  Object.defineProperty(form.elements.namedItem("file"), "files", { value: [chosen] });
}
function submit(form: HTMLFormElement): void { form.dispatchEvent(new Event("submit", { cancelable: true })); }

describe("projects", () => {
  it("opens saved worksheets, creates new ones and treats names as text", () => {
    const workspace = defaultWorkspace(); workspace.worksheets[0]!.name = "<img src=x>";
    const parent = host(); const onOpen = vi.fn(); const onNew = vi.fn();
    renderProjects(parent, workspace, onOpen, onNew);
    expect(parent.querySelector("img")).toBeNull();
    parent.querySelector<HTMLButtonElement>(".project-link")!.click();
    expect(onOpen).toHaveBeenCalledWith(workspace.activeId);
    parent.querySelector<HTMLButtonElement>(".run")!.click();
    expect(onNew).toHaveBeenCalledOnce();
  });
});
describe("ingestion", () => {
  it("uploads a stage file and reports success", async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ name: "data.csv", size: 10, last_modified: "now" }) });
    vi.stubGlobal("fetch", fetch);
    const form = upload(); file(form); submit(form); await settle();
    expect(fetch.mock.calls.some(([path]) => String(path).includes("/stages/MY_STAGE/files"))).toBe(true);
    expect(form.textContent).toContain("data.csv uploaded");
  });
  it("prepares a COPY for precisely the uploaded CSV and waits for explicit execution", async () => {
    opened.mockClear();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => ({ name: "data.csv", size: 10, last_modified: "now" }) }));
    const form = upload(true); file(form); submit(form); await settle();
    expect(opened).toHaveBeenCalledWith(expect.stringContaining("PATTERN = 'data.csv'"), context);
    expect(opened.mock.calls[0]![0]).toContain('CREATE TABLE TEST_DB.PUBLIC.MY_TABLE (\n  ID VARCHAR');
    expect(opened.mock.calls[0]![0]).toContain("COPY INTO TEST_DB.PUBLIC.MY_TABLE");
    expect(document.querySelector("dialog")).toBeNull();
  });
  it.each([["large.csv", 64 * 1024 * 1024, "smaller"], ["data.json", 10, "CSV"], ["bad'name.csv", 10, "simple identifiers"]])("rejects invalid table input %s", (name, size, message) => {
    const fetch = vi.fn(); vi.stubGlobal("fetch", fetch);
    const form = upload(true); file(form, name, size); submit(form);
    expect(form.querySelector(".load-status")!.textContent).toContain(message);
    expect(fetch.mock.calls.some((call) => call[1]?.method === "POST")).toBe(false);
  });
  it("renders upload errors and allows retry", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("Stage unavailable")));
    const form = upload(); file(form); submit(form); await settle();
    expect(form.textContent).toContain("Stage unavailable");
    expect(form.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(false);
  });
});

it("uses the chosen database/schema in the upload destination", async () => {
  const fetch = vi.fn(async (input: string, options?: RequestInit) => {
    if (options?.method === "POST") return { ok: true, json: async () => ({ name: "data.csv", size: 10, last_modified: "now" }) };
    if (input.endsWith("/warehouses")) return { ok: true, json: async () => [] };
    if (input.endsWith("/databases")) return { ok: true, json: async () => [{ name: "TEST_DB" }, { name: "OTHER_DB" }] };
    return { ok: true, json: async () => [{ name: input.includes("OTHER_DB") ? "RAW" : "PUBLIC" }] };
  });
  vi.stubGlobal("fetch", fetch);
  const form = upload(true); await settle();
  form.querySelector<HTMLButtonElement>('[aria-label="Choose database and schema"]')!.click(); await settle();
  [...form.querySelectorAll<HTMLButtonElement>(".selector-option")].find((button) => button.textContent?.startsWith("OTHER_DB"))!.click(); await settle();
  [...form.querySelectorAll<HTMLButtonElement>(".selector-option")].find((button) => button.textContent?.startsWith("RAW"))!.click();
  expect((form.elements.namedItem("schema") as HTMLInputElement).value).toBe("RAW");
  file(form); submit(form); await settle();
  expect(fetch.mock.calls.some(([path]) => path.includes("/databases/OTHER_DB/schemas/RAW/stages/MY_STAGE/files"))).toBe(true);
});

it("closes only through Cancel, leaving the schema trigger usable", async () => {
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("Catalog offline")));
  const form = upload(); await settle();
  form.querySelector<HTMLButtonElement>('[aria-label="Choose database and schema"]')!.click();
  expect(document.querySelector("dialog")).not.toBeNull();
  form.querySelector<HTMLButtonElement>('.load-actions button[type="button"]')!.click();
  expect(document.querySelector("dialog")).toBeNull();
});

describe("CSV table columns", () => {
  it("reads BOM and quoted simple headers", () => {
    expect(csvColumns('\uFEFFid,"full_name"\r\n1,test')).toEqual(["ID", "FULL_NAME"]);
  });
  it.each(['"full,name"\n', '"a""b"\n', "", "id,id\n", "id,\n", '"unfinished', '"multi\nline"\n']) ("rejects invalid header %j", (header) => {
    expect(() => csvColumns(header)).toThrow();
  });
});
it("lists only existing tables and hides Name when one is chosen", async () => {
  vi.stubGlobal("fetch", vi.fn(async (path: string) => ({ ok: true, json: async () => path.endsWith("/files") ? { name: "data.csv", size: 10, last_modified: "now" } : path.endsWith("/objects") ? { objects: [{ name: "EXISTING", kind: "table" }, { name: "VIEW_ONE", kind: "view" }] } : path.endsWith("/databases") ? [{ name: "TEST_DB" }] : path.endsWith("/schemas") ? [{ name: "PUBLIC" }] : [] })));
  const form = upload(true); await settle();
  const choice = form.elements.namedItem("tableChoice") as HTMLSelectElement;
  expect([...choice.options].map((option) => option.value)).toEqual(["", "EXISTING"]);
  choice.value = "EXISTING"; choice.dispatchEvent(new Event("change"));
  expect(form.querySelector<HTMLElement>(".load-table-name")!.hidden).toBe(true);
  file(form); opened.mockClear(); submit(form); await settle();
  expect(opened.mock.calls[0]![0]).toContain("COPY INTO TEST_DB.PUBLIC.EXISTING");
  expect(opened.mock.calls[0]![0]).not.toContain("CREATE TABLE");
});
