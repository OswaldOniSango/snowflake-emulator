import { describe, expect, it } from "vitest";
import { checkHealth } from "./health";

function respondWith(init: { ok: boolean; status: number }): typeof fetch {
  return (async () => init) as unknown as typeof fetch;
}

describe("checkHealth", () => {
  it("reports ok for a 200 response", async () => {
    await expect(checkHealth(respondWith({ ok: true, status: 200 }))).resolves.toEqual({
      status: "ok",
    });
  });

  it("reports the status code when the emulator answers unhealthy", async () => {
    await expect(checkHealth(respondWith({ ok: false, status: 503 }))).resolves.toEqual({
      status: "down",
      detail: "HTTP 503",
    });
  });

  it("reports the cause when the request never lands", async () => {
    const failing = (async () => {
      throw new Error("connection refused");
    }) as unknown as typeof fetch;

    await expect(checkHealth(failing)).resolves.toEqual({
      status: "down",
      detail: "connection refused",
    });
  });
});
