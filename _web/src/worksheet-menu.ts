import type { Worksheet } from "./workspace";
export interface WorksheetActions { rename(): void; download(): void; delete(): void; }
/** One floating menu avoids clipping inside the horizontally scrolling tabs. */
export function createWorksheetMenu() {
  let panel: HTMLElement | null = null;
  let anchor: HTMLButtonElement | null = null;
  function close(focus = false): void {
    panel?.remove(); panel = null;
    anchor?.setAttribute("aria-expanded", "false");
    if (focus && anchor?.isConnected) anchor.focus();
    anchor = null;
  }
  document.addEventListener("pointerdown", (event) => {
    if (event.target instanceof Node && !panel?.contains(event.target) && !anchor?.contains(event.target)) close();
  });
  window.addEventListener("resize", () => close());
  window.addEventListener("scroll", () => close(), true);
  function trigger(worksheet: Worksheet, actions: WorksheetActions): HTMLButtonElement {
    const button = document.createElement("button");
    button.className = "worksheet-menu-toggle"; button.type = "button";
    button.textContent = "⋯"; button.title = "Worksheet actions";
    button.setAttribute("aria-label", `Worksheet actions: ${worksheet.name}`);
    button.setAttribute("aria-haspopup", "menu"); button.setAttribute("aria-expanded", "false");
    function open(last = false): void {
      close(); anchor = button;
      button.setAttribute("aria-expanded", "true");
      panel = document.createElement("div"); panel.className = "worksheet-tab-menu";
      panel.setAttribute("role", "menu"); panel.setAttribute("aria-label", `Actions for ${worksheet.name}`);
      const items: HTMLButtonElement[] = [];
      for (const [label, icon, action] of [["Rename", "✎", actions.rename], ["Download", "↓", actions.download], ["Delete", "⌫", actions.delete]] as const) {
        const item = document.createElement("button"); item.type = "button"; item.setAttribute("role", "menuitem");
        if (label === "Delete") item.className = "danger";
        const glyph = document.createElement("span"); glyph.setAttribute("aria-hidden", "true"); glyph.textContent = icon;
        item.append(glyph, document.createTextNode(label));
        item.addEventListener("click", () => { close(label === "Download"); action(); });
        panel.append(item); items.push(item);
      }
      panel.addEventListener("keydown", (event) => {
        if (event.key === "Escape") { event.preventDefault(); close(true); }
        if (event.key === "Tab") { close(true); return; }
        if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        const index = items.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : (index + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
        items[next]?.focus();
      });
      document.body.append(panel);
      const rect = button.getBoundingClientRect();
      const size = panel.getBoundingClientRect();
      panel.style.left = `${Math.max(8, Math.min(rect.left, window.innerWidth - size.width - 8))}px`;
      panel.style.top = `${Math.max(8, Math.min(rect.bottom + 4, window.innerHeight - size.height - 8))}px`;
      items[last ? items.length - 1 : 0]?.focus();
    }
    button.addEventListener("click", (event) => { event.stopPropagation(); if (anchor === button) close(true); else open(); });
    button.addEventListener("keydown", (event) => {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); open(event.key === "ArrowUp"); }
    });
    return button;
  }
  return { trigger, close };
}

export function worksheetFilename(name: string): string {
  const safe = [...name].map((char) => char.charCodeAt(0) < 32 ? "_" : char).join("").replace(/[\\/:*?"<>|]/g, "_").trim().replace(/\.+$/, "") || "worksheet";
  return /\.sql$/i.test(safe) ? safe : `${safe}.sql`;
}
