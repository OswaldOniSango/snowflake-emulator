import {
  createWarehouse,
  dropWarehouse,
  listWarehouses,
  resumeWarehouse,
  suspendWarehouse,
  type Warehouse,
} from "./api";
import { renderNotice } from "./grid";

/**
 * The warehouses view.
 *
 * Compute is emulated: every statement runs on the same local DuckDB whatever
 * a warehouse is doing, so suspending one changes what the API reports and
 * nothing else. The view says so rather than implying idle capacity.
 */

const SIZES = ["X-Small", "Small", "Medium", "Large", "X-Large"];

/**
 * The states the emulator reports. It moves through RESUMING and SUSPENDING,
 * so a card cannot decide from ACTIVE alone what its button should offer.
 */
const RUNNING_STATES = new Set(["ACTIVE", "RESUMING"]);

export function createWarehousesView(parent: HTMLElement): { refresh: () => Promise<void> } {
  const root = document.createElement("div");
  root.className = "view";
  parent.append(root);

  async function refresh(): Promise<void> {
    root.replaceChildren(header(), renderNotice("info", "Loading warehouses…"));
    try {
      const warehouses = await listWarehouses();
      // The API lists them in whatever order it holds them; sorting keeps a
      // card from moving under the pointer after an action.
      warehouses.sort((a, b) => a.name.localeCompare(b.name));
      root.replaceChildren(header());
      root.append(
        warehouses.length === 0
          ? renderNotice("info", "No warehouses", "Create one to see how the API reports it.")
          : grid(warehouses),
      );
    } catch (cause) {
      root.replaceChildren(header());
      root.append(renderNotice("error", "Could not load warehouses", messageOf(cause)));
    }
  }

  function header(): HTMLElement {
    const head = document.createElement("div");
    head.className = "view-head";

    const heading = document.createElement("h1");
    heading.textContent = "Warehouses";

    const blurb = document.createElement("p");
    blurb.textContent =
      "Emulated compute. Every statement runs on the same local DuckDB engine, so a suspended warehouse only changes what the API reports.";

    const text = document.createElement("div");
    text.append(heading, blurb);

    head.append(text, createForm());
    return head;
  }

  function createForm(): HTMLElement {
    const form = document.createElement("form");
    form.className = "wh-create";

    const name = document.createElement("input");
    name.placeholder = "New warehouse";
    name.setAttribute("aria-label", "Warehouse name");

    const size = document.createElement("select");
    size.setAttribute("aria-label", "Warehouse size");
    size.append(
      ...SIZES.map((value) => {
        const option = document.createElement("option");
        option.value = value;
        option.textContent = value;
        return option;
      }),
    );

    const submit = document.createElement("button");
    submit.className = "ghost";
    submit.type = "submit";
    submit.textContent = "Create";

    form.addEventListener("submit", (event) => {
      event.preventDefault();
      const chosen = name.value.trim();
      if (!chosen) {
        return;
      }
      void act(() => createWarehouse(chosen, size.value));
      name.value = "";
    });

    form.append(name, size, submit);
    return form;
  }

  function grid(warehouses: Warehouse[]): HTMLElement {
    const list = document.createElement("div");
    list.className = "card-grid";

    for (const warehouse of warehouses) {
      list.append(card(warehouse));
    }
    return list;
  }

  function card(warehouse: Warehouse): HTMLElement {
    const state = warehouse.state.toUpperCase();
    const started = RUNNING_STATES.has(state);

    const article = document.createElement("article");
    article.className = "wh";

    const top = document.createElement("div");
    top.className = "wh-top";
    const name = document.createElement("b");
    name.textContent = warehouse.name;
    const badge = document.createElement("span");
    badge.className = `pill ${started ? "ok" : "idle"}`;
    badge.textContent = label(state);
    top.append(name, badge);

    const stats = document.createElement("div");
    stats.className = "wh-stats";
    stats.append(
      stat("Size", warehouse.size),
      stat("Auto-suspend", warehouse.auto_suspend ? `${warehouse.auto_suspend}s` : "—"),
      stat("Auto-resume", warehouse.auto_resume ? "yes" : "no"),
    );

    const actions = document.createElement("div");
    actions.className = "wh-act";

    const toggle = document.createElement("button");
    toggle.className = "ghost";
    toggle.textContent = started ? "Suspend" : "Resume";
    toggle.addEventListener("click", () => {
      void act(() => (started ? suspendWarehouse(warehouse.name) : resumeWarehouse(warehouse.name)));
    });

    const drop = document.createElement("button");
    drop.className = "ghost danger";
    drop.textContent = "Drop";
    drop.addEventListener("click", () => void act(() => dropWarehouse(warehouse.name)));

    actions.append(toggle, drop);
    article.append(top, stats, actions);
    return article;
  }

  /** Runs an action and reloads, so the view always shows what the API reports. */
  async function act(action: () => Promise<void>): Promise<void> {
    try {
      await action();
    } catch (cause) {
      root.append(renderNotice("error", "Action failed", messageOf(cause)));
      return;
    }
    await refresh();
  }

  void refresh();
  return { refresh };
}

/** Title-cases a state for display without inventing a name for it. */
function label(state: string): string {
  return state.charAt(0) + state.slice(1).toLowerCase();
}

function stat(label: string, value: string): HTMLElement {
  const wrapper = document.createElement("div");

  const caption = document.createElement("span");
  caption.className = "lab";
  caption.textContent = label;

  const text = document.createElement("span");
  text.className = "val";
  text.textContent = value;

  wrapper.append(caption, text);
  return wrapper;
}

function messageOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : "Request failed";
}
