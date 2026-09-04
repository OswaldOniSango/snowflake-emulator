/**
 * Whether the results dock is collapsed, remembered across reloads the same
 * way the theme is (see theme.ts) — a worksheet-heavy session wants the
 * editor to keep the extra room until asked to give it back.
 */

const STORAGE_KEY = "mallard.dock-collapsed";

export function loadDockCollapsed(storage: Storage | null = safeStorage()): boolean {
  try {
    return storage?.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

export function saveDockCollapsed(collapsed: boolean, storage: Storage | null = safeStorage()): void {
  try {
    if (collapsed) {
      storage?.setItem(STORAGE_KEY, "1");
    } else {
      storage?.removeItem(STORAGE_KEY);
    }
  } catch {
    // The choice simply will not be remembered.
  }
}

function label(collapsed: boolean): string {
  return collapsed ? "Show results panel" : "Hide results panel";
}

/**
 * A button that collapses or restores `dock` (hiding it lets the editor grow
 * into the freed space, since it is the only flexible sibling left) and
 * remembers the choice.
 */
export function createDockCollapseToggle(dock: HTMLElement): HTMLButtonElement {
  let collapsed = loadDockCollapsed();
  dock.hidden = collapsed;

  const button = document.createElement("button");
  button.className = "icon-btn";
  button.type = "button";

  const paint = (): void => {
    button.textContent = collapsed ? "⌃" : "⌄";
    button.setAttribute("aria-label", label(collapsed));
    button.title = label(collapsed);
  };

  button.addEventListener("click", () => {
    collapsed = !collapsed;
    dock.hidden = collapsed;
    saveDockCollapsed(collapsed);
    paint();
  });

  paint();
  return button;
}

function safeStorage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}
