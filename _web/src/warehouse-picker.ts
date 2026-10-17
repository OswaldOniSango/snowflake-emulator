import { listWarehouses, type Warehouse } from "./api";
import { session, useWarehouse } from "./auth";
export function warehouseSize(size: string): string {
  const value = size.toUpperCase().replaceAll("_", "").replaceAll("-", "");
  if (value === "XSMALL") return "X-Small";
  if (value === "XLARGE") return "X-Large";
  if (/^[2-6]XLARGE$/.test(value)) return `${value[0]}X-Large`;
  return size ? size[0]!.toUpperCase() + size.slice(1).toLowerCase() : "Unknown size";
}
export function warehouseLabel(warehouse: Warehouse | undefined, name = "Choose warehouse"): HTMLElement {
  const root = document.createElement("span"); root.className = "warehouse-label";
  const light = document.createElement("span"); light.className = "warehouse-light";
  light.dataset["active"] = String(warehouse?.state.toUpperCase() === "ACTIVE");
  light.setAttribute("aria-hidden", "true");
  const label = document.createElement("span"); label.className = "warehouse-name"; label.textContent = warehouse?.name ?? name;
  root.title = warehouse ? `${warehouse.name} · ${warehouse.state} · ${warehouseSize(warehouse.size)}` : `${name} · State unavailable`;
  root.append(light, label);
  if (warehouse) { const size = document.createElement("small"); size.className = "warehouse-size"; size.textContent = warehouseSize(warehouse.size); root.append(size); }
  return root;
}
export function createWarehousePicker(parent: HTMLElement): void {
  const details = document.createElement("details"); details.className = "warehouse-picker";
  const trigger = document.createElement("summary"); trigger.setAttribute("aria-label", "Run on warehouse");
  const popover = document.createElement("div"); popover.className = "warehouse-options";
  const search = document.createElement("input"); search.type = "search"; search.placeholder = "Run on warehouse…"; search.setAttribute("aria-label", "Search warehouses");
  const list = document.createElement("div"); list.className = "warehouse-options-list";
  const error = document.createElement("p"); error.className = "warehouse-picker-error"; error.setAttribute("role", "status");
  const searchBox = document.createElement("label"); searchBox.className = "warehouse-search";
  searchBox.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="10" cy="10" r="7"/><path d="m15 15 6 6"/></svg>';
  searchBox.append(search);
  popover.append(searchBox, list, error); details.append(trigger, popover); parent.append(details);
  let warehouses: Warehouse[] = []; let selected = session()?.warehouse ?? "";
  function paint(): void {
    trigger.replaceChildren(warehouseLabel(warehouses.find((warehouse) => warehouse.name === selected), selected || "Choose warehouse"));
    list.replaceChildren();
    for (const warehouse of warehouses.filter((entry) => entry.name.toLowerCase().includes(search.value.toLowerCase()))) {
      const option = document.createElement("button"); option.type = "button"; option.className = "selector-option";
      option.setAttribute("aria-current", String(warehouse.name === selected)); option.append(warehouseLabel(warehouse));
      if (warehouse.name === selected) { const check = document.createElement("span"); check.className = "selector-check"; check.textContent = "✓"; option.append(check); }
      option.addEventListener("click", () => { selected = warehouse.name; useWarehouse(selected); details.open = false; paint(); }); list.append(option);
    }
    if (warehouses.length > 0 && list.childElementCount === 0) list.textContent = "No matching warehouses";
  }
  async function refresh(): Promise<void> {
    error.textContent = "Loading warehouses…";
    try { warehouses = await listWarehouses(); error.textContent = warehouses.length ? "" : "No warehouses available"; paint(); }
    catch (cause) { warehouses = []; paint(); error.textContent = cause instanceof Error ? cause.message : "Could not load warehouses"; }
  }
  search.addEventListener("input", paint);
  details.addEventListener("toggle", () => { if (details.open) void refresh(); });
  details.addEventListener("keydown", (event) => { if (event.key === "Escape" && details.open) { event.preventDefault(); event.stopPropagation(); details.open = false; trigger.focus(); } });
  const dialog = parent.closest("dialog");
  dialog?.addEventListener("click", (event) => { if (event.target instanceof Node && !details.contains(event.target)) details.open = false; });
  paint(); void refresh();
}
