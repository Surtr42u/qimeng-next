import { expect, test } from "@playwright/test";
import { mockLibrary } from "./fixture";

test("the original vocabulary workspace edits the custom layer and preserves a stale full-set draft", async ({ page }) => {
  await mockLibrary(page);
  const groups = [{canonical:"合成组",variants:["fixture"],characters:[{canonical:"合成角色",aliases:["alias"]}]}];
  let revision=1;
  let stopWords=["合成停用"];
  const attempted: unknown[]=[];
  await page.route("**/api/v1/sources/custom-groups",route=>{
    if(route.request().method()==="PUT"){
      const body=route.request().postDataJSON();attempted.push(body);expect(route.request().headers()["idempotency-key"]).toBeTruthy();
      if(body.expectedRevision!==revision)return route.fulfill({status:409,json:{code:"revision_conflict",message:"合成并发修改",currentRevision:revision}});
      revision++;stopWords=body.stopWords;
    }
    return route.fulfill({json:{revision,groups,stopWords,rebuildState:"completed",processed:3,matched:1,ambiguous:0}});
  });
  await page.goto("/manage");await page.getByRole("link",{name:/词表维护/}).click();
  await expect(page.locator("#page-maintenance-files-vocabulary-edit h3")).toHaveText(["停用词","自定义出处组"]);
  await page.getByRole("button",{name:"添加停用词"}).click();
  const dialog=page.getByRole("dialog");await dialog.getByRole("textbox").fill("新增备注");await dialog.getByRole("button",{name:"确定"}).click();
  revision=2;stopWords=["合成停用","其他设备"];
  await page.getByRole("button",{name:"保存修改"}).click();
  await expect(page.getByText("新增备注",{exact:true})).toBeVisible();
  await page.getByRole("button",{name:"读取服务器当前词表"}).click();await page.getByText("核对当前服务器修订 2").click();
  await expect(page.locator("pre")).toContainText("其他设备");
  await page.getByRole("button",{name:"已核对，保留草稿用于整组替换"}).click();await page.getByRole("button",{name:"保存修改"}).click();
  await expect(page.getByRole("status").filter({hasText:"已保存"})).toBeVisible();
  expect(attempted).toEqual([1,2].map(expectedRevision=>({groups,stopWords:["合成停用","新增备注"],expectedRevision})));
});
