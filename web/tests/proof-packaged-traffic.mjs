import { chromium } from "@playwright/test";
import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";

const network = process.env.PROOF_NETWORK;
assert(process.env.GITHUB_ACTIONS === "true" && ["loopback","restricted"].includes(network));
const ports = JSON.parse(process.env.PROOF_PORTS), cookies = JSON.parse(process.env.PROOF_COOKIES);
const output = [];
async function persist() {
  await writeFile("../build/browser-static-" + network + ".json", JSON.stringify(output, null, 2)+"\n");
}
async function trial(variant, round) {
  const port = ports[variant], base = "http://127.0.0.1:" + port;
  assert([18493,18494].includes(port));
  const browser = await chromium.launch();
  try {
    const context = await browser.newContext({ viewport:{ width:1280,height:800 } });
    await context.addCookies([{ name:"qimeng_session",value:cookies[variant],url:base,httpOnly:true,sameSite:"Strict" }]);
    const page = await context.newPage();
    page.setDefaultTimeout(12000);
    const cachedEntry = async () => page.evaluate(async () => {
      const cache = await caches.open("qimeng-shell-v1"), keys = (await cache.keys()).map(k => new URL(k.url).pathname);
      return {paths:keys,core:keys.filter(p => /^\/assets\/index-[\w-]{8}\.(js|css)$/.test(p))};
    });
    for (const cache of ["cold","warm"]) {
      if (cache === "warm") {
        // First navigation installs the worker. Prime its first controlled visit before measuring a warm reopen.
        await page.evaluate(() => navigator.serviceWorker.ready.then(() => true));
        await page.reload({waitUntil:"load"});
        await page.waitForFunction(() => !!navigator.serviceWorker.controller);
        await page.locator("#page-home .media-card").first().waitFor();
        await page.waitForLoadState("networkidle");
        await page.waitForFunction(async () => {
          const keys = await (await caches.open("qimeng-shell-v1")).keys();
          return keys.filter(k => /\/assets\/index-[\w-]{8}\.(js|css)$/.test(new URL(k.url).pathname)).length === 2;
        });
      }
      const resourceResponses = [];
      const listener = response => {
        const path = new URL(response.url()).pathname;
        if (/^\/assets\/index-[\w-]{8}\.(js|css)$/.test(path))
          resourceResponses.push({path, encoding:response.headers()["content-encoding"] || "identity",
                                  fromServiceWorker:response.fromServiceWorker(),status:response.status()});
      };
      page.on("response", listener);
      const startEpochMs = Date.now();
      const nav = await page.goto(base + "/", {waitUntil:"commit"});
      assert(nav?.ok());
      await page.locator("#page-home .media-card").first().click({trial:true});
      const actionEpochMs = Date.now();
      await page.waitForLoadState("load");
      await page.evaluate(() => Promise.all([document.fonts.ready, navigator.serviceWorker.ready]).then(()=>true));
      await page.waitForLoadState("networkidle");
      const endEpochMs = Date.now();
      page.off("response",listener);
      const resourceTiming = await page.evaluate(() => performance.getEntriesByType("resource")
        .filter(r => /\/assets\/index-[\w-]{8}\.(js|css)$/.test(new URL(r.name).pathname))
        .map(r=>({path:new URL(r.name).pathname, encodedBodySize:r.encodedBodySize,decodedBodySize:r.decodedBodySize,
                  transferSize:r.transferSize, workerStart:r.workerStart})));
      assert.equal(resourceResponses.length,2);
      const expected = variant === "candidate" ? "gzip" : "identity";
      for (const res of resourceResponses) {
        assert.equal(res.encoding,expected);
        if (cache === "warm") assert(res.fromServiceWorker);
      }
      output.push({network,variant,round,cache,chromium:browser.version(),startEpochMs,actionEpochMs,endEpochMs,
        actionableMs:actionEpochMs-startEpochMs,action:"first synthetic media card passes Playwright trial click",
        navigationFromSW:nav.fromServiceWorker(),resourceResponses,resourceTiming});
      await persist();
    }
    // Prove the click also performs real UI navigation outside the timed window.
    await page.locator("#page-home .media-card").first().click();
    await page.waitForURL(/\/asset\/[^/]+$/);
    await page.goBack({waitUntil:"commit"});
    await page.locator("#page-home .media-card").first().waitFor();
    const keys = await cachedEntry();
    assert.equal(keys.core.length,2);
    assert(!keys.paths.some(p=>p.startsWith("/api/")));
    if (round===4) {
      const rule=["OUTPUT","-o","lo","-p","tcp","--dport",String(port),"-j","REJECT","--reject-with","tcp-reset"];
      execFileSync("sudo",["iptables","-w","-I",...rule]);
      try {
        await context.setOffline(true);
        const back = await page.goto(base+"/?offline-proof=1",{waitUntil:"load"});
        assert(back.ok() && back.fromServiceWorker());
        await page.locator("#root .connection-screen").waitFor();
        assert.equal(await page.locator(".media-card").count(),0);
        assert(await page.evaluate(async () => { try {await fetch("/api/v1/sessions/current");return false;} catch {return true;} }));
        output.at(-1).offline = {previouslyVisitedShellFromSW:true,appExecuted:true,protectedAPINetworkUnavailable:true,
          cachedAPIEntries:0, previouslyOpenedCardNavigatedBack:true};
        await persist();
      } finally {
        execFileSync("sudo",["iptables","-w","-D",...rule]);
      }
    }
  } finally { await browser.close(); }
}
for (let round=1;round<=4;round++) {
  const order=round%2 ? ["baseline","candidate"] : ["candidate","baseline"];
  for (const variant of order) await trial(variant,round);
}
