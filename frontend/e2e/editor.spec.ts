import { expect, test } from "@playwright/test";

test("notebook and file editors work with external requests blocked", async ({ page, request }) => {
  const saved = await request.put("/api/settings", { data: { defaultProvider: "", defaultModel: "" } });
  expect(saved.ok()).toBe(true);
  const externalRequests: string[] = [];
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/*", (route) => {
    const url = new URL(route.request().url());
    if (url.protocol.startsWith("http") && !["127.0.0.1", "localhost"].includes(url.hostname)) {
      externalRequests.push(url.hostname);
      return route.abort();
    }
    return route.continue();
  });

  await page.goto("/");
  await page.getByRole("button", { name: "Open local tools", exact: true }).click();
  await page.getByLabel("Project path").fill("..");
  await page.getByRole("button", { name: "Open Folder" }).click();
  await page.getByRole("button", { name: "Notebook", exact: true }).click();
  await expect(page.locator(".monaco-editor")).toBeVisible({ timeout: 15000 });
  await page.locator(".monaco-editor .view-lines").click();
  await page.keyboard.type('print("Local editor ready")', { delay: 30 });
  await expect(page.locator(".view-lines")).toContainText(/Local\s+editor\s+ready/);
  await expect(page.getByRole("button", { name: "New and run" })).toBeEnabled();

  await page.getByRole("button", { name: "Files", exact: true }).click();
  await page.getByRole("button", { name: "README.md", exact: true }).click();
  await expect(page.locator(".monaco-editor")).toBeVisible({ timeout: 15000 });
  await expect(page.locator(".view-lines")).toContainText("Superscience");
  expect(externalRequests).toEqual([]);
  expect(errors).toEqual([]);
});
