import { beforeEach, describe, expect, it, vi } from "vitest";
import { authHeaders, login, logout, rememberRole, session, subscribe, useRole, useWarehouse } from "./auth";

beforeEach(() => {
  sessionStorage.clear();
});

describe("browser authentication", () => {
  it("logs in and keeps the token in session storage", async () => {
    const fetchFn = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
      success: true,
      data: {
        token: "session-token",
        masterToken: "master-token",
        validityInSeconds: 3600,
        sessionInfo: { databaseName: "TEST_DB", schemaName: "PUBLIC", warehouseName: "COMPUTE_WH", roleName: "SYSADMIN" },
      },
    }), { status: 200 }));

    const result = await login({ username: "ADMIN", password: "admin", warehouse: "COMPUTE_WH" }, fetchFn);
    expect(result.role).toBe("SYSADMIN");
    expect(session()?.token).toBe("session-token");
    expect(fetchFn.mock.calls[0]?.[1]).toMatchObject({ method: "POST" });
    expect(JSON.parse(String(fetchFn.mock.calls[0]?.[1]?.body)).data.warehouseName).toBe("COMPUTE_WH");
  });

  it("uses the authenticated token for role changes", async () => {
    sessionStorage.setItem("mallard.session", JSON.stringify({ token: "token", masterToken: "master", username: "ADMIN", role: "PUBLIC", database: "TEST_DB", schema: "PUBLIC", warehouse: "", expiresAt: Date.now() + 10000 }));
    // Reloading the module is unnecessary for header behavior; login establishes state below.
    const loginFetch = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ success: true, data: { token: "token", masterToken: "master", validityInSeconds: 100, sessionInfo: { databaseName: "TEST_DB", schemaName: "PUBLIC", warehouseName: "", roleName: "PUBLIC" } } })));
    await login({ username: "ADMIN", password: "admin" }, loginFetch);
    const fetchFn = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ success: true })));
    await useRole("SYSADMIN", fetchFn);
    expect(authHeaders()).toEqual({ Authorization: "Bearer token" });
    expect(JSON.parse(String(fetchFn.mock.calls[0]?.[1]?.body))).toEqual({ sqlText: 'USE ROLE "SYSADMIN"' });
    expect(session()?.role).toBe("SYSADMIN");
  });

  it("logs out and removes the session", async () => {
    const fetchFn = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ success: true })));
    await logout(fetchFn);
    expect(session()).toBeNull();
    expect(authHeaders()).toEqual({});
  });

  it("notifies the UI that the authenticated session ended", async () => {
    const loginFetch = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
      success: true,
      data: {
        token: "session-token",
        masterToken: "master-token",
        validityInSeconds: 100,
        sessionInfo: { databaseName: "TEST_DB", schemaName: "PUBLIC", warehouseName: "COMPUTE_WH", roleName: "SYSADMIN" },
      },
    })));
    await login({ username: "ADMIN", password: "admin" }, loginFetch);

    const listener = vi.fn();
    const unsubscribe = subscribe(listener);
    await logout(vi.fn<typeof fetch>().mockRejectedValue(new Error("offline")));
    unsubscribe();

    expect(listener).toHaveBeenLastCalledWith(null);
  });

  it("keeps the selected warehouse in the browser session", async () => {
    const loginFetch = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ success: true, data: { token: "token", masterToken: "master", validityInSeconds: 100, sessionInfo: { databaseName: "TEST_DB", schemaName: "PUBLIC", warehouseName: "", roleName: "PUBLIC" } } })));
    await login({ username: "ADMIN", password: "admin" }, loginFetch);

    useWarehouse("COMPUTE_WH");

    expect(session()?.warehouse).toBe("COMPUTE_WH");
  });

  it("keeps a successful worksheet USE ROLE in the browser session", async () => {
    const loginFetch = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({ success: true, data: { token: "token", masterToken: "master", validityInSeconds: 100, sessionInfo: { databaseName: "TEST_DB", schemaName: "PUBLIC", warehouseName: "", roleName: "ACCOUNTADMIN" } } })));
    await login({ username: "ADMIN", password: "admin" }, loginFetch);
    rememberRole("PHASE7_READER");
    expect(session()?.role).toBe("PHASE7_READER");
  });
});
