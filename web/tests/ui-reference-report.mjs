import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const pct = (a, b) => (100 * a / b).toFixed(2) + "%";

export async function compareImages(browser, out, old, next) {
  const page = await browser.newPage();
  try {
    const sources = await Promise.all([old, next].map(async row => "data:image/png;base64," + (await readFile(join(out, row.file))).toString("base64")));
    const { pair, diff, ...pixels } = await page.evaluate(async pngs => {
      const images = await Promise.all(pngs.map(async src => { const img = new Image(); img.src = src; await img.decode(); return img; }));
      const [width, height] = [images[0].naturalWidth, images[0].naturalHeight];
      if (images[1].naturalWidth !== width || images[1].naturalHeight !== height) throw Error("两张原图尺寸不同");
      const canvas = (w, h) => { const el = document.createElement("canvas"); el.width = w; el.height = h; return el; };
      const combined = canvas(width * 2, height + 34);
      const draw = combined.getContext("2d");
      draw.fillStyle = "#20232a"; draw.fillRect(0, 0, combined.width, 34);
      draw.fillStyle = "white"; draw.font = "18px sans-serif";
      ["已安装旧提交", "当前提交"].forEach((label, i) => { draw.fillText(label, i * width + 12, 23); draw.drawImage(images[i], i * width, 34); });
      const left = draw.getImageData(0, 34, width, height).data;
      const right = draw.getImageData(width, 34, width, height).data;
      const map = canvas(width, height);
      const ctx = map.getContext("2d"), heat = ctx.createImageData(width, height);
      let changed = 0, above12 = 0;
      for (let i = 0; i < left.length; i += 4) {
        const delta = Math.max(Math.abs(left[i] - right[i]), Math.abs(left[i + 1] - right[i + 1]), Math.abs(left[i + 2] - right[i + 2]), Math.abs(left[i + 3] - right[i + 3]));
        if (delta) changed++;
        if (delta > 12) above12++;
        heat.data[i] = delta ? Math.min(255, 45 + delta * 2) : 0;
        heat.data[i + 1] = delta > 12 ? 80 : 0;
        heat.data[i + 3] = 255;
      }
      ctx.putImageData(heat, 0, 0);
      return { pair: combined.toDataURL("image/png").split(",")[1], diff: map.toDataURL("image/png").split(",")[1], width, height, changed, above12, total: width * height };
    }, sources);
    const base = `${old.view}-${old.scene}`;
    await Promise.all([[`${base}-pair.png`, pair], [`${base}-difference.png`, diff]].map(([name, data]) => writeFile(join(out, name), Buffer.from(data, "base64"))));
    const a = old.metrics, b = next.metrics;
    const fields = ["ids", "titles", "captions", "cardDates", "groups", "toolbar", "filters", "heading", "pager", "authors", "nextTitles", "detailMeta", "tags", "imageSize", "dark", "zoom", "font", "layer"];
    const fieldsChanged = fields.filter(k => !same(a[k], b[k]));
    const geometry = Object.keys(a.anchors).filter(k => {
      const l = a.anchors[k], r = b.anchors[k];
      return !l || !r ? l !== r : l.some((v, i) => Math.abs(v - r[i]) > 1);
    });
    return { view: old.view, scene: old.scene, ...pixels, geometry, fieldsChanged, pairFile: `${base}-pair.png`, diffFile: `${base}-difference.png` };
  } finally {
    await page.close();
  }
}

export async function writeReport(out, data) {
  const rows = [
    "# 已安装 Web 与当前 Web 的云端同场景对照", "",
    `旧提交 \`${data.installed}\`，当前提交 \`${data.candidate}\`。各自按提交内的 OpenAPI 和锁文件生成、构建。运行入口使用同一 Chromium ${data.browser}、系统中文字体 ${data.font}、语言 ${data.locale}、时区 ${data.timezone}、固定时刻 ${data.time}、暗色、DPR 1、浏览器默认缩放。桌面为 1440×900，窄屏为 390×844 的桌面浏览器视口。产品的 CSS 缩放另行实测。`, "",
    "两站读取同一批十张 640×360 PNG 原图与缩略图、顺序、大小、作者、标签及浏览数。北京时间今天、昨天各五件。两套测试 HTTP 接口各自提供产品原协议。输入的字节数、SHA-256、日期和身份见 measurements.json。以只读合成目录和测试会话进入首页，点击侧栏“相册”，点第五张卡片进详情，再用浏览器返回。", "",
    "每个视口、每套应用使用独立空浏览器上下文，随后共用其正常 HTTP 缓存。两侧均停用 Service Worker。截图保留真实页面、遮挡、字体和完整视口，没有遮罩或样式注入。并排图按原尺寸排列，标题写在截图上方。差异图逐像素比较两张原始 PNG。像素偏差不设通关阈值。", "",
    "|视口 / 页面|任何差异像素|通道变化大于 12|超过 1 CSS px 的布局差异|内容或呈现字段差异|",
    "|---|---:|---:|---|---|",
  ];
  const label = { home: "首页", album: "相册", detail: "详情", desktop: "桌面", narrow: "窄屏" };
  const name = p => label[p.view] + " / " + label[p.scene];
  for (const p of data.pairs) rows.push(`|${name(p)}|${pct(p.changed, p.total)}|${pct(p.above12, p.total)}|${p.geometry.join("、") || "无"}|${p.fieldsChanged.join("、") || "无"}|`);
  rows.push("", "## 输入、内容和布局实测", "");
  const val = x => JSON.stringify(x).replaceAll("|", "／");
  for (const p of data.pairs) {
    const [a, b] = data.cases.filter(c => c.view === p.view && c.scene === p.scene);
    const am = a.metrics, bm = b.metrics;
    rows.push(`### ${name(p)}`, "",
      `原图 ${a.file}、${b.file}。并排图 ${p.pairFile}、逐像素差异图 ${p.diffFile}。路由 ${am.path} / ${bm.path}，CSS 缩放 ${am.zoom} / ${bm.zoom}，底衬列表均读取 ${am.ids.length} / ${bm.ids.length} 件。`, "");
    if (p.scene === "detail") {
      rows.push(`旧页面的遮挡与底衬为 ${val(am.layer)}，当前为 ${val(bm.layer)}。旧原图 ${val(am.imageSize)}，当前 ${val(bm.imageSize)}，旧页码 ${val(am.pager)}，当前 ${val(bm.pager)}。`, "",
        `返回相册的实际滚动偏移（CSS px）旧 ${val(a.returnToAlbum ?? null)}，当前 ${val(b.returnToAlbum ?? null)}。`, "");
    }
    if (p.geometry.length) {
      rows.push("|元素|旧 [x,y,w,h]|当前 [x,y,w,h]|", "|---|---|---|");
      for (const k of p.geometry) rows.push(`|${k}|${val(am.anchors[k])}|${val(bm.anchors[k])}|`);
      rows.push("");
    }
    for (const k of p.fieldsChanged) {
      if (k !== "layer") rows.push(`- ${k}：旧 ${val(am[k])}，当前 ${val(bm[k])}。`);
    }
    rows.push("");
  }
  rows.push("## 实际 HTTP 上下行", "",
    "从浏览器视角统计字节。上行是隔离 HTTP 服务实际从 TCP 套接字读取的字节，下行是该服务向套接字写入的字节，含 HTTP/1.1 请求头、响应头和分块开销。未使用代理、TLS 或内容压缩。另列 API、媒体、静态资源等响应正文字节。首轮是冷启动，进入相册与详情复用缓存。此处数值仅代表该固定场景。", "",
    "|视口 / 页面|版本|实际上行 B|实际下行 B|响应正文 B（按类别）|请求数|", "|---|---|---:|---:|---|---:|");
  const totals = new Map();
  for (const c of data.cases) {
    for (const [phase, n] of [[name(c), c.net], ...(c.returnNet ? [[label[c.view] + " / 返回相册", c.returnNet]] : [])]) {
      const groups = Object.entries(n.categories).map(([key, sum]) => `${key} ${sum.outBytes}`).join("、") || "0";
      const which = c.side === "installed" ? "旧" : "当前";
      rows.push(`|${phase}|${which}|${n.wireIn}|${n.wireOut}|${groups}|${n.requests.length}|`);
      const key = label[c.view] + " / " + which, sum = totals.get(key) ?? [0, 0];
      totals.set(key, [sum[0] + n.wireIn, sum[1] + n.wireOut]);
    }
  }
  rows.push("");
  for (const [key, [sent, received]] of totals) rows.push(`- ${key} 全流程上行 ${sent} B，下行 ${received} B。`);
  rows.push("", "## 有效性与边界", "",
    data.errors.length ? "以下场景或输入校验失败，保留已完成截图，不能据此认定全套验证通过。" : "十件媒体的身份、顺序、真实原图解码、详情页码、列表底衬和返回路径均经产品入口验证。",
    "", ...data.errors.map(e => "- " + e.replaceAll("\n", " ").slice(0, 700)),
    "", "本批仅验证有数据的首页、相册与图片详情。Go 后端运行、视频播放、原生桌面和 Android 由各自的云验证承担。原始几何、DOM 文字、应用响应与不带查询串的验证请求按场景保存在 measurements.json。");
  await writeFile(join(out, "report.md"), rows.join("\n") + "\n");
}
