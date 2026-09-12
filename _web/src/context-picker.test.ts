import { afterEach, describe, expect, it, vi } from "vitest";
import { createContextPicker } from "./context-picker";

const settle = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

describe("database and schema picker", () => {
  it("opens a searchable two-column picker and changes the namespace", async () => {
    vi.stubGlobal("fetch", async (input: string) => {
      if (input.includes("/OTHER_DB/schemas")) {
        return response([{ name: "ANALYTICS" }, { name: "STAGING" }]);
      }
      if (input.endsWith("/schemas")) return response([{ name: "PUBLIC" }]);
      return response([{ name: "TEST_DB" }, { name: "OTHER_DB" }]);
    });
    const parent = document.createElement("div");
    document.body.append(parent);
    const onChange = vi.fn();

    createContextPicker({ parent, initial: { database: "TEST_DB", schema: "PUBLIC" }, onChange });
    await settle();
    await settle();
    onChange.mockClear();

    parent.querySelector<HTMLButtonElement>('[aria-label="Choose database and schema"]')?.click();
    expect(parent.querySelectorAll(".selector-column")).toHaveLength(2);
    expect(parent.querySelector<HTMLInputElement>('[aria-label="Search databases"]')).not.toBeNull();

    [...parent.querySelectorAll<HTMLButtonElement>(".selector-option")]
      .find((button) => button.textContent?.includes("OTHER_DB"))?.click();
    await settle();
    await settle();

    expect([...parent.querySelectorAll(".selector-option")].some((button) => button.textContent?.includes("ANALYTICS"))).toBe(true);
    [...parent.querySelectorAll<HTMLButtonElement>(".selector-option")]
      .find((button) => button.textContent?.includes("ANALYTICS"))?.click();

    expect(onChange).toHaveBeenLastCalledWith({ database: "OTHER_DB", schema: "ANALYTICS" });
  });
});

function response(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}
