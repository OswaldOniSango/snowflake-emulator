import { describe, expect, it } from "vitest";
import { isSupportedPrivilege, objectName, roleNamesFromRows, safeName } from "./identity-admin";

describe("identity administration validation", () => {
  it("reads SHOW ROLES names from the second column", () => {
    expect(roleNamesFromRows([["x", "ACCOUNTADMIN"], ["x", "ANALYST"]], "PUBLIC")).toEqual([
      "PUBLIC",
      "ACCOUNTADMIN",
      "ANALYST",
    ]);
  });

  it("accepts only supported privilege and object shapes", () => {
    expect(isSupportedPrivilege("select")).toBe(true);
    expect(isSupportedPrivilege("OWNERSHIP")).toBe(false);
    expect(objectName("TABLE sales.orders")).toBe(true);
    expect(objectName("TABLE sales; DROP TABLE users")).toBe(false);
  });

  it("rejects unsafe identifiers", () => {
    expect(safeName("ANALYST_1")).toBe(true);
    expect(safeName("analyst;DROP")).toBe(false);
  });
});
