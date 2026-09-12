import { describe, expect, it } from "vitest";
import { discoverAvailableRoles, isSupportedPrivilege, objectName, roleNamesFromRows, safeName } from "./identity-admin";
import type { Statement } from "./api";

describe("identity administration validation", () => {
  it("reads SHOW ROLES names from the second column", () => {
    expect(roleNamesFromRows([["x", "ACCOUNTADMIN"], ["x", "ANALYST"]], "PUBLIC")).toEqual([
      "PUBLIC",
      "ACCOUNTADMIN",
      "ANALYST",
    ]);
  });

  it("offers direct and inherited roles but not unrelated catalog roles", async () => {
    const execute = async (sql: string): Promise<Statement> => {
      const rows = sql.includes("TO USER")
        ? [["ACCOUNTADMIN", "USER", "ADMIN"], ["PUBLIC", "USER", "ADMIN"]]
        : sql.includes("ACCOUNTADMIN")
          ? [["SYSADMIN", "ROLE", "ACCOUNTADMIN"], ["USAGE", "WAREHOUSE", "COMPUTE_WH"]]
          : [];
      return { rows } as Statement;
    };

    await expect(discoverAvailableRoles(
      { database: "TEST_DB", schema: "PUBLIC" },
      "ADMIN",
      "ACCOUNTADMIN",
      execute,
    )).resolves.toEqual(["ACCOUNTADMIN", "PUBLIC", "SYSADMIN"]);
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
