import { beforeEach, describe, expect, it, vi } from "vitest";
import { authHeaders, login, logout, session, useRole } from "./auth";

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

    const result = await login({ username: "ADMIN", password: "admin" }, fetchFn);
    expect(result.role).toBe("SYSADMIN");
    expect(session()?.token).toBe("session-token");
    expect(fetchFn.mock.calls[0]?.[1]).toMatchObject({ method: "POST" });
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
});
