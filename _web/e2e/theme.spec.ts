import { expect, test } from "@playwright/test";

test("explicit dark theme overrides a light system preference", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.addInitScript(() => localStorage.setItem("mallard.theme", "light"));
  await page.route("**/session/v1/login-request", (route) => route.fulfill({
    json: {
      success: true,
      data: {
        token: "theme-test-token",
        masterToken: "theme-test-master-token",
        validityInSeconds: 3600,
        sessionInfo: {
          databaseName: "TEST_DB",
          schemaName: "PUBLIC",
          warehouseName: "TEST_WH",
          roleName: "ACCOUNTADMIN",
        },
      },
    },
  }));
  await page.goto("/");

  await page.getByLabel("Username", { exact: true }).fill("ADMIN");
  await page.getByLabel("Password", { exact: true }).fill("admin");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("button", { name: "User menu", exact: true }).click();
  await expect(page.getByRole("button", { name: "Sign out", exact: true })).toBeVisible();

  const themeButton = page.getByRole("button", { name: "Light theme. Click to change." });
  const background = async (): Promise<string> => page.locator("body").evaluate(
    (body) => getComputedStyle(body).backgroundColor,
  );

  await expect.poll(background).toBe("rgb(246, 248, 250)");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.locator("html")).toHaveCSS("color-scheme", "light");
  await themeButton.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect.poll(background).toBe("rgb(25, 31, 39)");
  await expect(page.locator("html")).toHaveCSS("color-scheme", "dark");

  await page.getByRole("button", { name: "Dark theme. Click to change." }).click();
  await expect(page.locator("html")).not.toHaveAttribute("data-theme");
  await expect.poll(background).toBe("rgb(246, 248, 250)");
});
