import { afterEach, describe, expect, it, vi } from "vitest";
import { createExplorer } from "./explorer";
import { createHistoryView } from "./history";
import { createWarehousesView } from "./warehouses";

/**
 * These views fetch on creation and render whatever comes back — object names,
 * SQL text, warehouse names. All of it is server data, and none of it may be
 * parsed as markup.
 */

/** Answers each request from a map of path fragment to body. */
function stubFetch(routes: Record<string, unknown>): void {
  vi.stubGlobal("fetch", async (input: string) => {
    const match = Object.keys(routes).find((path) => input.includes(path));
    if (match === undefined) {
      return { ok: false, status: 404, json: async () => ({ message: `no stub for ${input}` }) };
    }
    return { ok: true, status: 200, json: async () => routes[match] };
  });
}

/** Lets the view's pending fetches resolve and render. */
const settle = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));

function host(): HTMLElement {
  const element = document.createElement("div");
  document.body.append(element);
  return element;
}

/**
 * The upload button opens a detached file input and reacts to its "change"
 * event, so a test drives that same flow: capture the input `chooseStageFile`
 * creates and feed it a file, as if the user had picked one.
 */
function stubChosenFile(file: File): void {
  const create = document.createElement.bind(document);
  vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
    const element = create(tag);
    if (tag === "input") {
      Object.defineProperty(element, "files", { value: [file], configurable: true });
      element.click = () => element.dispatchEvent(new Event("change"));
    }
    return element;
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.body.replaceChildren();
});

describe("createExplorer", () => {
  it("lists the databases the catalog reports", async () => {
    stubFetch({ "/databases": [{ name: "TEST_DB" }, { name: "OTHER_DB" }] });
    const parent = host();

    createExplorer({ parent, context: () => ({ database: "TEST_DB", schema: "PUBLIC" }), onInsert: () => {} });
    await settle();

    expect([...parent.querySelectorAll(".node .nm")].map((n) => n.textContent)).toEqual([
      "TEST_DB",
      "OTHER_DB",
    ]);
  });

  it("says so when the catalog is empty", async () => {
    stubFetch({ "/databases": [] });
    const parent = host();

    createExplorer({ parent, context: () => ({ database: "", schema: "" }), onInsert: () => {} });
    await settle();

    expect(parent.querySelector(".tree-note")?.textContent).toBe("No databases");
  });

  it("reports a failure instead of rendering nothing", async () => {
    vi.stubGlobal("fetch", async () => ({
      ok: false,
      status: 500,
      json: async () => ({ message: "catalog is down" }),
    }));
    const parent = host();

    createExplorer({ parent, context: () => ({ database: "", schema: "" }), onInsert: () => {} });
    await settle();

    expect(parent.querySelector(".tree-note")?.textContent).toContain("catalog is down");
  });

  it("renders a database name containing a tag as text", async () => {
    stubFetch({ "/databases": [{ name: "<script>alert(1)</script>" }] });
    const parent = host();

    createExplorer({ parent, context: () => ({ database: "", schema: "" }), onInsert: () => {} });
    await settle();

    expect(parent.querySelector("script")).toBeNull();
    expect(parent.querySelector(".node .nm")?.textContent).toBe("<script>alert(1)</script>");
  });

  it("offers file upload only for stage objects", async () => {
    vi.stubGlobal("fetch", async (input: string) => {
      if (input.endsWith("/objects")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ objects: [{ name: "USERS", kind: "table" }, { name: "LOAD_STAGE", kind: "stage" }] }),
        };
      }
      if (input.endsWith("/schemas")) {
        return { ok: true, status: 200, json: async () => [{ name: "PUBLIC" }] };
      }
      return { ok: true, status: 200, json: async () => [{ name: "TEST_DB" }] };
    });
    const parent = host();

    createExplorer({ parent, context: () => ({ database: "TEST_DB", schema: "PUBLIC" }), onInsert: () => {} });
    await settle();
    parent.querySelector<HTMLButtonElement>('[role="treeitem"]')?.click();
    await settle();
    parent.querySelectorAll<HTMLButtonElement>('[role="treeitem"]')[1]?.click();
    await settle();

    expect(parent.querySelector('[aria-label="Upload file to LOAD_STAGE"]')).not.toBeNull();
    expect(parent.querySelector('[aria-label="Upload file to USERS"]')).toBeNull();
  });

  it("renders views in their own object group", async () => {
    vi.stubGlobal("fetch", async (input: string) => {
      if (input.endsWith("/objects")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ objects: [{ name: "ACTIVE_USERS", kind: "view" }] }),
        };
      }
      if (input.endsWith("/schemas")) {
        return { ok: true, status: 200, json: async () => [{ name: "PUBLIC" }] };
      }
      return { ok: true, status: 200, json: async () => [{ name: "TEST_DB" }] };
    });
    const parent = host();

    createExplorer({ parent, context: () => ({ database: "TEST_DB", schema: "PUBLIC" }), onInsert: () => {} });
    await settle();
    parent.querySelector<HTMLButtonElement>('[role="treeitem"]')?.click();
    await settle();
    parent.querySelectorAll<HTMLButtonElement>('[role="treeitem"]')[1]?.click();
    await settle();

    expect([...parent.querySelectorAll(".group-label")].map((node) => node.textContent)).toContain("Views");
    expect([...parent.querySelectorAll(".node .nm")].map((node) => node.textContent)).toContain("ACTIVE_USERS");
  });

  it("renders dynamic tables in their own object group", async () => {
    vi.stubGlobal("fetch", async (input: string) => {
      if (input.endsWith("/objects")) {
        return { ok: true, status: 200, json: async () => ({ objects: [{ name: "USER_SUMMARY", kind: "dynamic_table", detail: "1 MINUTE" }] }) };
      }
      if (input.endsWith("/schemas")) return { ok: true, status: 200, json: async () => [{ name: "PUBLIC" }] };
      return { ok: true, status: 200, json: async () => [{ name: "TEST_DB" }] };
    });
    const parent = host();
    createExplorer({ parent, context: () => ({ database: "TEST_DB", schema: "PUBLIC" }), onInsert: () => {} });
    await settle();
    parent.querySelector<HTMLButtonElement>('[role="treeitem"]')?.click();
    await settle();
    parent.querySelectorAll<HTMLButtonElement>('[role="treeitem"]')[1]?.click();
    await settle();
    expect([...parent.querySelectorAll(".group-label")].map((node) => node.textContent)).toContain("Dynamic tables");
    expect([...parent.querySelectorAll(".node .nm")].map((node) => node.textContent)).toContain("USER_SUMMARY");
  });

  async function openLoadStage(parent: HTMLElement): Promise<void> {
    vi.stubGlobal("fetch", async (input: string) => {
      if (input.endsWith("/objects")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ objects: [{ name: "LOAD_STAGE", kind: "stage" }] }),
        };
      }
      if (input.endsWith("/schemas")) {
        return { ok: true, status: 200, json: async () => [{ name: "PUBLIC" }] };
      }
      if (input.includes("/files")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ name: "data.csv", size: 3, last_modified: "2026-01-01T00:00:00Z" }),
        };
      }
      return { ok: true, status: 200, json: async () => [{ name: "TEST_DB" }] };
    });

    createExplorer({ parent, context: () => ({ database: "TEST_DB", schema: "PUBLIC" }), onInsert: () => {} });
    await settle();
    parent.querySelector<HTMLButtonElement>('[role="treeitem"]')?.click();
    await settle();
    parent.querySelectorAll<HTMLButtonElement>('[role="treeitem"]')[1]?.click();
    await settle();
  }

  it("shows a dismissable message once the upload finishes, instead of it sticking around forever", async () => {
    const parent = host();
    await openLoadStage(parent);
    stubChosenFile(new File(["a,b"], "data.csv", { type: "text/csv" }));

    parent.querySelector<HTMLButtonElement>('[aria-label="Upload file to LOAD_STAGE"]')?.click();
    await settle();

    const feedback = parent.querySelector(".explorer-feedback");
    expect(feedback?.textContent).toContain("data.csv uploaded to @LOAD_STAGE");

    parent.querySelector<HTMLButtonElement>('[aria-label="Dismiss"]')?.click();

    expect(parent.querySelector(".explorer-feedback")).toBeNull();
  });

  it("clears a lingering upload message when the tree is refreshed", async () => {
    const parent = host();
    await openLoadStage(parent);
    stubChosenFile(new File(["a,b"], "data.csv", { type: "text/csv" }));

    parent.querySelector<HTMLButtonElement>('[aria-label="Upload file to LOAD_STAGE"]')?.click();
    await settle();
    expect(parent.querySelector(".explorer-feedback")).not.toBeNull();

    parent.querySelector<HTMLButtonElement>('[aria-label="Refresh objects"]')?.click();
    await settle();

    expect(parent.querySelector(".explorer-feedback")).toBeNull();
  });

});

describe("createWarehousesView", () => {
  it("renders a card per warehouse with its state", async () => {
    stubFetch({
      "/warehouses": [
        { name: "COMPUTE_WH", state: "ACTIVE", size: "X-SMALL", auto_suspend: 600, auto_resume: true, running: 2, queued: 1, last_activity_on: "2026-09-10T10:00:00Z" },
        { name: "LOAD_WH", state: "SUSPENDED", size: "X-SMALL" },
      ],
    });
    const parent = host();

    createWarehousesView(parent);
    await settle();

    expect([...parent.querySelectorAll(".wh-top b")].map((n) => n.textContent)).toEqual([
      "COMPUTE_WH",
      "LOAD_WH",
    ]);
    expect([...parent.querySelectorAll(".wh-top .pill")].map((n) => n.textContent)).toEqual([
      "Active",
      "Suspended",
    ]);
    expect(parent.textContent).toContain("Running2");
    expect(parent.textContent).toContain("Queued1");
  });

  it("offers Suspend for a running warehouse and Resume for a stopped one", async () => {
    stubFetch({
      "/warehouses": [
        { name: "A", state: "ACTIVE", size: "X-SMALL" },
        { name: "B", state: "SUSPENDED", size: "X-SMALL" },
      ],
    });
    const parent = host();

    createWarehousesView(parent);
    await settle();

    const actions = [...parent.querySelectorAll(".wh-act button")].map((n) => n.textContent);
    expect(actions).toEqual(["Suspend", "Drop", "Resume", "Drop"]);
  });

  it("says so when there are none", async () => {
    stubFetch({ "/warehouses": [] });
    const parent = host();

    createWarehousesView(parent);
    await settle();

    expect(parent.querySelector(".notice h4")?.textContent).toBe("No warehouses");
  });

  it("renders a warehouse name containing a tag as text", async () => {
    stubFetch({ "/warehouses": [{ name: "<img src=x onerror=1>", state: "ACTIVE", size: "X-SMALL" }] });
    const parent = host();

    createWarehousesView(parent);
    await settle();

    expect(parent.querySelector("img")).toBeNull();
    expect(parent.querySelector(".wh-top b")?.textContent).toBe("<img src=x onerror=1>");
  });
});

describe("createHistoryView", () => {
  const entry = {
    statementHandle: "01abc",
    status: "success",
    statement: "SELECT * FROM users",
    createdOn: 1_700_000_000_000,
    durationMs: 12,
    numRows: 2,
  };

  it("renders a row per statement", async () => {
    stubFetch({ "/statements": { statements: [entry], retainedFor: "1h0m0s" } });
    const parent = host();

    createHistoryView({ parent, onOpen: () => {} });
    await settle();

    const rows = [...parent.querySelectorAll(".history-row")];
    expect(rows).toHaveLength(1);
    expect(rows[0]?.textContent).toContain("SELECT * FROM users");
    expect(rows[0]?.textContent).toContain("01abc");
  });

  it("filters history and toggles columns without losing row actions", async () => {
    stubFetch({ "/statements": { statements: [entry, { ...entry, statementHandle: "failed-query", status: "failed", warehouse: "DEV_WH", username: "ALICE", warehouseSize: "X-SMALL" }], retainedFor: "1h0m0s" } });
    const parent = host();
    createHistoryView({ parent, onOpen: () => {} }); await settle();
    const status = parent.querySelector<HTMLSelectElement>('[aria-label="History status"]')!;
    status.value = "failed"; status.dispatchEvent(new Event("change"));
    expect(parent.querySelectorAll(".history-row")).toHaveLength(1);
    expect(parent.querySelector(".history-row")!.textContent).toContain("DEV_WH");
    expect(parent.querySelector(".history-row")!.textContent).toContain("ALICE");
    expect(parent.querySelector(".history-row")!.textContent).toContain("X-Small");
    const check = parent.querySelector<HTMLInputElement>('.history-columns input')!;
    check.checked = false; check.dispatchEvent(new Event("change"));
    expect((parent.querySelector("tbody tr")!.children[1] as HTMLElement).hidden).toBe(true);
    const search = parent.querySelector<HTMLInputElement>('[aria-label="Search query history"]')!;
    search.value = "missing"; search.dispatchEvent(new Event("input"));
    expect(parent.querySelector(".history-count")!.textContent).toBe("0 Queries");
  });

  it("combines user filtering with numeric history sorting", async () => {
    stubFetch({ "/statements": { statements: [
      { ...entry, username: "ALICE", durationMs: 10, statementHandle: "a" },
      { ...entry, username: "BOB", durationMs: 1, statementHandle: "b" },
      { ...entry, username: "ALICE", durationMs: 2, statementHandle: "c" },
      { ...entry, statementHandle: "legacy" },
    ], retainedFor: "1h0m0s" } });
    const parent = host(); const opened: string[] = [];
    createHistoryView({ parent, onOpen: (row) => opened.push(row.statementHandle) }); await settle();
    const user = parent.querySelector<HTMLSelectElement>('[aria-label="History user"]')!;
    user.value = "user:ALICE"; user.dispatchEvent(new Event("change"));
    const duration = [...parent.querySelectorAll<HTMLElement>("thead th")].find((th) => th.textContent === "DURATION")!;
    duration.click();
    expect(parent.querySelector(".history-count")!.textContent).toBe("2 Queries");
    parent.querySelector<HTMLElement>(".history-row")!.click();
    expect(opened).toEqual(["c"]);
    duration.click(); parent.querySelector<HTMLElement>(".history-row")!.click();
    expect(opened).toEqual(["c", "a"]);
    user.value = "missing"; user.dispatchEvent(new Event("change"));
    expect(parent.querySelector(".history-count")!.textContent).toBe("1 Queries");
    expect(parent.querySelector(".history-row")!.textContent).toContain("legacy");
  });

  it("reopens a statement when its row is clicked", async () => {
    stubFetch({ "/statements": { statements: [entry], retainedFor: "1h0m0s" } });
    const parent = host();
    const opened: string[] = [];

    createHistoryView({ parent, onOpen: (row) => opened.push(row.statement) });
    await settle();
    parent.querySelector<HTMLElement>(".history-row")?.click();

    expect(opened).toEqual(["SELECT * FROM users"]);
  });

  it("explains an empty history in terms of how long statements are kept", async () => {
    stubFetch({ "/statements": { statements: [], retainedFor: "1h0m0s" } });
    const parent = host();

    createHistoryView({ parent, onOpen: () => {} });
    await settle();

    expect(parent.querySelector(".notice pre")?.textContent).toContain("1 hour");
  });

  it("renders a statement containing a tag as text", async () => {
    stubFetch({
      "/statements": {
        statements: [{ ...entry, statement: "SELECT '<script>alert(1)</script>'" }],
        retainedFor: "1h0m0s",
      },
    });
    const parent = host();

    createHistoryView({ parent, onOpen: () => {} });
    await settle();

    expect(parent.querySelector("script")).toBeNull();
    expect(parent.querySelector(".hist-sql")?.textContent).toContain("<script>");
  });
});

describe("reference database explorer", () => {
  async function expanded(onInsert = vi.fn()): Promise<HTMLElement> {
    stubFetch({
      "/objects": { objects: [
        { name: "USERS", kind: "table" }, { name: "ORDERS", kind: "table" },
        { name: "ACTIVE_USERS", kind: "view" }, { name: "SUMMARY", kind: "dynamic_table" },
        { name: "LOAD", kind: "procedure" }, { name: "FILES", kind: "stage" },
        { name: "CHANGES", kind: "stream" }, { name: "DAILY", kind: "task" },
      ] },
      "/schemas": [{ name: "PUBLIC" }],
      "/databases": [{ name: "TEST_DB" }],
    });
    const parent = host();
    createExplorer({ parent, context: () => ({ database: "TEST_DB", schema: "PUBLIC" }), onInsert });
    await settle();
    parent.querySelector<HTMLButtonElement>(".node")!.click(); await settle();
    parent.querySelector<HTMLButtonElement>(".node.lvl1")!.click(); await settle();
    return parent;
  }
  it.each(["database", "schema", "table", "view", "dynamic_table", "procedure", "stage", "stream", "task"])("uses an outline icon for %s", async (kind) => {
    const parent = await expanded();
    const icon = parent.querySelector(`svg.kind-${kind}`);
    expect(icon?.getAttribute("aria-hidden")).toBe("true");
    expect(icon?.querySelector("path")?.getAttribute("d")).toBeTruthy();
    expect(parent.querySelector(".node .dot")).toBeNull();
  });
  it("counts real objects, collapses and reopens a group", async () => {
    const parent = await expanded();
    const group = parent.querySelector<HTMLButtonElement>(".object-group-heading")!;
    expect(group.querySelector(".object-count")!.textContent).toBe("2");
    group.click();
    expect(group.getAttribute("aria-expanded")).toBe("false");
    const children = parent.querySelector<HTMLElement>(`[id="${group.getAttribute("aria-controls")}"]`)!;
    expect(children.hidden).toBe(true);
    group.click();
    expect(children.hidden).toBe(false);
  });
  it("selects an object and preserves insertion behavior", async () => {
    const onInsert = vi.fn(); const parent = await expanded(onInsert);
    const leaf = [...parent.querySelectorAll<HTMLButtonElement>(".node.lvl3")].find((node) => node.textContent === "USERS")!;
    leaf.click();
    expect(onInsert).toHaveBeenCalledWith("USERS");
    expect(parent.querySelector('[aria-selected="true"] .nm')!.textContent).toBe("USERS");
  });
  it("searches loaded objects and updates group counts", async () => {
    const parent = await expanded();
    const search = parent.querySelector<HTMLInputElement>('input[type="search"]')!;
    search.value = "ORDERS"; search.dispatchEvent(new Event("input"));
    expect(parent.querySelectorAll(".object-group-heading")).toHaveLength(1);
    expect(parent.querySelector(".object-count")!.textContent).toBe("1");
    expect(parent.querySelector(".node.lvl3 .nm")!.textContent).toBe("ORDERS");
  });
});
