import { afterEach, expect, it, vi } from "vitest";

vi.mock("./editor", () => ({
  createEditor: () => ({
    value: () => "SELECT 'private data';",
    selection: () => "",
    selectionRange: () => null,
    cursorOffset: () => 0,
    highlightRunning: vi.fn(),
    focus: vi.fn(),
  }),
}));
vi.mock("./explorer", () => ({ createExplorer: vi.fn() }));
vi.mock("./context-picker", () => ({ createContextPicker: () => ({ set: vi.fn() }) }));
vi.mock("./catalog", () => ({ createCatalog: () => ({ load: vi.fn(), refresh: vi.fn() }), changesCatalog: () => false }));
vi.mock("./identity-admin", () => ({ discoverAvailableRoles: async () => ["PUBLIC"] }));
vi.mock("./health", () => ({ checkHealth: async () => ({ status: "ok" }) }));
vi.mock("./api", async (original) => ({
  ...await original<typeof import("./api")>(),
  listWarehouses: async () => [],
  runStatement: vi.fn(),
  translateStatement: vi.fn(),
  cancelStatement: vi.fn(),
}));

afterEach(() => {
  document.body.replaceChildren();
  sessionStorage.clear();
  localStorage.clear();
});

it("clears results and ignores old execution and translation responses after logout/login", async () => {
  const { login, logout } = await import("./auth");
  const { runStatement, translateStatement } = await import("./api");
  const signIn = async (token: string): Promise<void> => {
    await login({ username: token, password: "secret" }, vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
      success: true,
      data: { token, masterToken: "master", validityInSeconds: 3600,
        sessionInfo: { databaseName: "TEST_DB", schemaName: "PUBLIC", warehouseName: "WH", roleName: "PUBLIC" } },
    }))));
  };
  const signOut = async (): Promise<void> => { await logout(vi.fn<typeof fetch>().mockResolvedValue(new Response("{}"))); };
  await signIn("alice");
  document.body.innerHTML = '<div id="app"></div>';
  await import("./main");
  const result = { columns: [{ name: "secret", type: "TEXT", nullable: true }], rows: [["alice-private"]], totalRows: 1, handle: "alice-handle", elapsedMs: 1, rowsAffected: null };
  vi.mocked(runStatement).mockResolvedValue(result);
  const run = (): void => document.querySelector<HTMLButtonElement>('[data-role="run"]')!.click();
  const dock = (): string => document.querySelector('[data-role="dock"]')!.textContent ?? "";
  run();
  await vi.waitFor(() => expect(dock()).toContain("alice-private"));
  await signOut();
  await signIn("bob");
  expect(dock()).not.toContain("alice-private");
  expect(dock()).toContain("Run a statement");
  expect(document.querySelector('[data-role="pill"]')!.textContent).toBe("Idle");

  let finish!: (value: typeof result) => void;
  vi.mocked(runStatement).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  run();
  await signOut();
  await signIn("carol");
  finish(result);
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(dock()).toContain("Run a statement");
  expect(dock()).not.toContain("alice-private");
  expect(document.querySelector<HTMLButtonElement>('[data-role="run"]')!.disabled).toBe(false);

  let finishTranslation!: (value: import("./api").Translation) => void;
  vi.mocked(translateStatement).mockImplementationOnce(() => new Promise((resolve) => { finishTranslation = resolve; }));
  document.querySelector<HTMLButtonElement>('[data-tab="translation"]')!.click();
  await signOut();
  await signIn("dave");
  finishTranslation({ statement: "SELECT 'alice-private'", translated: "SELECT 'alice-private'", handledBy: "translator", complete: true, rewrites: [] });
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(dock()).toContain("Run a statement");
  expect(dock()).not.toContain("alice-private");
  await signOut();
});
