import { expect, test } from "@playwright/test";
import { mockLibrary } from "./fixture";

test('a source revision conflict rebases only the edited words before a second explicit save', async ({ page }) => {
  const state = await mockLibrary(page); let revision=1; let sources=['original']; const writes: unknown[]=[];
  await page.route('**/api/v1/authors/synthetic-author', route=>{
    if(route.request().method()==='PATCH'){
      const body=route.request().postDataJSON();writes.push(body);
      if(body.expectedRevision!==revision)return route.fulfill({status:409,json:{code:'revision_conflict',message:'来源已变更'}});
      revision++;sources=body.acquisitionSources;
    }
    return route.fulfill({json:{id:'synthetic-author',kind:'regular',name:'合成作者',placeholder:false,revision,acquisitionSources:sources}});
  });
  await page.goto('/asset/'+state.assets[0]!.id+'/edit');
  const editor=page.locator('#page-asset-edit');
  await editor.getByRole('button',{name:'original',exact:true}).click();
  const input=editor.getByPlaceholder('新站点或 URL，回车加入');await input.fill('local');await input.press('Enter');
  revision=2;sources=['original','remote'];
  await editor.getByRole('button',{name:'保存',exact:true}).click();
  await editor.getByRole('button',{name:'读取当前版本并保留我的改动'}).click();
  await expect(editor.getByRole('button',{name:'remote',exact:true})).toHaveAttribute('aria-pressed','true');
  expect(writes).toHaveLength(1);
  await editor.getByRole('button',{name:'保存',exact:true}).click();
  await expect(page).toHaveURL(/\/asset\/synthetic-0$/);
  expect(writes).toEqual([{expectedRevision:1,acquisitionSources:['local']},{expectedRevision:2,acquisitionSources:['remote','local']}]);
});

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
