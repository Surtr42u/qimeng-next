import { expect, test } from "@playwright/test";

test("disconnecting after an instance 503 leaves a working retry at startup", async ({ page, context }) => {
  const endpoint = "**/api/v1/instance";
  let probes = 0;
  page.on("request", request => {
    if (new URL(request.url()).pathname === "/api/v1/instance") probes++;
  });
  await page.route(endpoint, route => route.fulfill({
    status: 503,
    json: { code: "temporarily_unavailable", message: "合成影库暂不可用" },
  }), { times: 1 });

  const disconnect = page.waitForResponse(response =>
    new URL(response.url()).pathname === "/api/v1/instance" && response.status() === 503,
  ).then(() => context.setOffline(true));
  await Promise.all([page.goto("/"), disconnect]);
  await expect(page.getByRole("alert")).toContainText("浏览器报告离线，连接检查已暂停。");
  await expect(page.getByRole("alert")).toContainText("合成影库暂不可用");
  expect(probes).toBe(1);
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect.poll(() => probes).toBe(2);
  await expect(page.getByRole("alert")).toContainText("无法连接影库");
});
