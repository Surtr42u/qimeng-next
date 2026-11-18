import { expect, test } from "@playwright/test";
import { mockLibrary } from "./fixture";

test("saving the complete author set clears the navigation guard before returning to the detail", async ({ page }) => {
  const state = await mockLibrary(page);
  const asset = state.assets[0]!;
  let patches = 0;
  await page.route("**/api/v1/assets/synthetic-0", async route => {
    if (route.request().method() !== "PATCH") return route.fulfill({ json: asset });
    patches++; const body = route.request().postDataJSON();
    expect(body.authorIds).toEqual(["synthetic-author"]); expect(body.newAuthorNames).toEqual(["合成追加"]);
    expect(route.request().headers()["idempotency-key"]).toBeTruthy();
    asset.authorIds = ["synthetic-author", "second-author"]; asset.authorsRevision++; asset.revision++;
    return route.fulfill({ json: asset });
  });
  await page.route("**/api/v1/authors/second-author", route => route.fulfill({ json: { id: "second-author", name: "合成追加", kind: "regular", revision: 1, acquisitionSources: [] } }));
  await page.goto("/asset/synthetic-0/edit");
  await page.getByLabel("查找作者", { exact: true }).fill("合成追加");
  await page.getByRole("button", { name: "新建：合成追加 保存时创建" }).click();
  await page.locator("#page-asset-edit").getByRole("button", { name: "保存", exact: true }).click();
  await expect(page).toHaveURL(/\/asset\/synthetic-0$/);
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  expect(patches).toBe(1);
});
