import type { Workspace } from "./workspace";
export function renderWorksheetList(parent: HTMLElement, workspace: Workspace, onOpen: (id: string) => void, onNew: () => void): void {
  parent.replaceChildren();
  const title = document.createElement("div"); title.className = "workspace-list-title"; title.textContent = "My Workspace";
  const add = document.createElement("button"); add.className = "workspace-add"; add.textContent = "+  Add new"; add.addEventListener("click", onNew);
  const list = document.createElement("div"); list.className = "worksheet-list";
  for (const worksheet of workspace.worksheets) {
    const button = document.createElement("button"); button.className = "worksheet-file";
    button.setAttribute("aria-current", String(worksheet.id === workspace.activeId));
    const icon = document.createElement("span"); icon.className = "sql-file-icon"; icon.textContent = "SQL"; icon.setAttribute("aria-hidden", "true");
    const name = document.createElement("span"); name.textContent = worksheet.name; button.title = worksheet.name;
    button.append(icon, name); button.addEventListener("click", () => onOpen(worksheet.id)); list.append(button);
  }
  parent.append(title, add, list);
}
