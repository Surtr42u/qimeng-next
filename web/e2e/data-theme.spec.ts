import { test, expect, type Locator } from "@playwright/test";
import { mockLibrary } from "./fixture";

async function followsToken(locator: Locator, property: string, token: string) {
  await expect
    .poll(() =>
      locator.evaluate((element, { property, token }) => {
        const probe = document.createElement("span");
        probe.style.color = "var(" + token + ")";
        document.body.appendChild(probe);
        try {
          return (
            getComputedStyle(element).getPropertyValue(property) ===
            getComputedStyle(probe).color
          );
        } finally {
          probe.remove();
        }
      }, { property, token }),
    )
    .toBe(true);
}

for (const theme of ["dark", "light"] as const) {
  test("data and browsing review retain the " + theme + " Web palette", async ({ page }, testInfo) => {
    await page.addInitScript((choice) => localStorage.setItem("qimeng.theme", choice), theme);
    await mockLibrary(page);
    await page.goto("/data?range=30d");
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    const period = page.getByRole("group", { name: "统计时段" });
    const selected = period.getByRole("button", { name: "近 30 天", exact: true });
    const inactive = period.getByRole("button", { name: "近 7 天", exact: true });
    await expect(selected).toHaveAttribute("aria-pressed", "true");
    await expect(inactive).toHaveAttribute("aria-pressed", "false");
    await followsToken(page.locator("body"), "background-color", "--qm-bg");
    await followsToken(selected, "background-color", "--qm-primary");
    await followsToken(selected, "color", "--qm-on-accent");
    await followsToken(inactive, "color", "--qm-text-muted");

    const trend = page.getByRole("region", { name: "浏览与播放趋势" });
    const dates = trend.locator(".recharts-cartesian-axis-tick-value");
    await expect(dates.first()).toBeVisible();
    await followsToken(dates.first(), "fill", "--text-sub");
    await page.screenshot({ path: testInfo.outputPath("data-" + theme + ".png"), animations: "disabled" });

    await page.getByRole("link", { name: "查看回顾", exact: true }).click();
    await expect(page).toHaveURL(/\/data\/review\?range=30d$/);
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    await expect(page.getByRole("group", { name: "24小时浏览时长" })).toBeVisible();
    await followsToken(page.locator("body"), "background-color", "--qm-bg");
    await page.screenshot({ path: testInfo.outputPath("data-review-" + theme + ".png"), animations: "disabled" });
  });
}
