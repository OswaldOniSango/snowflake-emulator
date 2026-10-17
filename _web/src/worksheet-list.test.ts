import { expect, it, vi } from "vitest";
import { renderWorksheetList } from "./worksheet-list";
import { defaultWorkspace } from "./workspace";
it("opens real worksheets and creates new ones with safe names", () => {
  const parent = document.createElement("div"); const workspace = defaultWorkspace(); workspace.worksheets[0]!.name = "<img src=x>";
  const open = vi.fn(); const add = vi.fn(); renderWorksheetList(parent, workspace, open, add);
  expect(parent.querySelector("img")).toBeNull();
  parent.querySelector<HTMLButtonElement>(".worksheet-file")!.click(); expect(open).toHaveBeenCalledWith(workspace.activeId);
  parent.querySelector<HTMLButtonElement>(".workspace-add")!.click(); expect(add).toHaveBeenCalledOnce();
});
