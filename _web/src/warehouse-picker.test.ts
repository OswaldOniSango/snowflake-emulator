import { afterEach, expect, it, vi } from "vitest";
import { createWarehousePicker, warehouseLabel, warehouseSize } from "./warehouse-picker";
vi.mock("./auth", () => ({ session: () => ({ warehouse: "ACTIVE_WH" }), useWarehouse: vi.fn(), authHeaders: () => ({}) }));
afterEach(() => { vi.unstubAllGlobals(); document.body.replaceChildren(); });
it.each([["ACTIVE", "true"], ["SUSPENDED", "false"], ["RESUMING", "false"]])("renders %s with the actual status and size", (state, active) => {
  const label = warehouseLabel({ name: "<img src=x>", state, size: "XSMALL" });
  expect(label.querySelector<HTMLElement>(".warehouse-light")!.dataset["active"]).toBe(active);
  expect(label.textContent).toContain("X-Small"); expect(label.querySelector("img")).toBeNull();
});
it.each([["MEDIUM", "Medium"], ["X-Small", "X-Small"], ["2XLARGE", "2X-Large"]])("formats %s", (value, expected) => expect(warehouseSize(value)).toBe(expected));
it("lists real warehouses, filters by name and selects one", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => [{ name: "ACTIVE_WH", state: "ACTIVE", size: "XSMALL" }, { name: "OFF_WH", state: "SUSPENDED", size: "SMALL" }] }));
  const parent = document.createElement("div"); document.body.append(parent); createWarehousePicker(parent);
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(parent.querySelectorAll(".selector-option")).toHaveLength(2);
  const search = parent.querySelector<HTMLInputElement>("input")!; search.value = "OFF"; search.dispatchEvent(new Event("input"));
  expect(parent.querySelectorAll(".selector-option")).toHaveLength(1);
  parent.querySelector<HTMLButtonElement>(".selector-option")!.click();
  const { useWarehouse } = await import("./auth"); expect(useWarehouse).toHaveBeenCalledWith("OFF_WH");
  expect(parent.querySelector("summary")!.textContent).toContain("Small");
});
it("reports catalog failures and leaves the state unknown", async () => {
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("Warehouse catalog unavailable")));
  const parent = document.createElement("div"); createWarehousePicker(parent); await new Promise((resolve) => setTimeout(resolve, 0));
  expect(parent.textContent).toContain("Warehouse catalog unavailable");
  expect(parent.querySelector<HTMLElement>(".warehouse-light")!.dataset["active"]).toBe("false");
});
