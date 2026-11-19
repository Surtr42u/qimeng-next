import { createHash } from "node:crypto";

export const fixedTime = "2026-11-18T04:00:00Z";
const authorId = "20000000-0000-4000-8000-000000000001";
const libraryId = "30000000-0000-4000-8000-000000000001";
const tagId = "40000000-0000-4000-8000-000000000001";
const authorName = "合成作者", source = "合成作品", character = "合成角色", tag = "晨光";

// One set of encoded bytes for both sites, including originals, thumbnails and preloads.
export async function makeInventory(browser) {
  const page = await browser.newPage();
  try {
    const encoded = await page.evaluate(() => Array.from({ length: 10 }, (_, i) => {
      const canvas = document.createElement("canvas");
      canvas.width = 640;
      canvas.height = 360;
      const ctx = canvas.getContext("2d");
      const hue = i * 34 + 12;
      const sky = ctx.createLinearGradient(0, 0, 640, 360);
      sky.addColorStop(0, `hsl(${hue} 64% 75%)`);
      sky.addColorStop(1, `hsl(${hue + 40} 48% 40%)`);
      ctx.fillStyle = sky;
      ctx.fillRect(0, 0, 640, 360);
      ctx.fillStyle = "#fff4d2";
      ctx.beginPath();
      ctx.arc(64 + i * 48, 64 + (i % 3) * 20, 32, 0, 2 * Math.PI);
      ctx.fill();
      for (let layer = 0; layer < 2; layer++) {
        ctx.fillStyle = `hsl(${hue + layer * 30} 32% ${45 - layer * 15}%)`;
        ctx.beginPath();
        ctx.moveTo(0, 360);
        for (let x = 0; x <= 640; x += 8) ctx.lineTo(x, 190 + layer * 65 + 40 * Math.sin(x / 94 + i + layer));
        ctx.lineTo(640, 360);
        ctx.fill();
      }
      return canvas.toDataURL("image/png").split(",")[1];
    }));
    return encoded.map((image, i) => {
      const index = String(i + 1).padStart(2, "0");
      const bytes = Buffer.from(image, "base64");
      const date = i < 5 ? "2026-11-18" : "2026-11-17";
      return { id: `10000000-0000-4000-8000-0000000000${index}`, title: `合成景物 ${index}.png`, date,
        timestamp: `${date}T02:${String(50 - i).padStart(2, "0")}:00Z`, views: 20 - i,
        hash: createHash("sha256").update(bytes).digest("hex"), bytes };
    });
  } finally {
    await page.close();
  }
}

const response = (body, status = 200, headers = {}) => ({ body, status, headers });
export function adapter(side, inventory) {
  const legacy = side === "installed";
  const total = inventory.length;
  const dateCounts = [...new Set(inventory.map(a => a.date))].map(date => ({ date, fileCount: inventory.filter(a => a.date === date).length }));
  const tagRecord = { id: tagId, name: tag };
  const person = { id: authorId, displayName: authorName, type: "regular", fileCount: total, followed: false };
  const rows = inventory.map(a => legacy ? {
    id: a.id, libraryId, fileName: a.title, modifiedAt: a.timestamp, addedAt: a.timestamp,
    mediaType: "image", sizeBytes: a.bytes.length, source, characters: [character], authorId, authorNames: [authorName],
    tagIds: [tagId], cos: false, isFavorite: false, likedToday: false, likeCount: 0, viewCount: a.views, playCount: 0,
    thumbUrl: `/media/thumb/${a.id}`, origUrl: `/media/orig/${a.id}`,
  } : {
    id: a.id, libraryId, fileName: a.title, originalFileName: a.title, relativePath: a.title, mimeType: "image/png",
    mediaType: "image", size: a.bytes.length, hash: a.hash, revision: 1, tagsRevision: 1, authorsRevision: 1,
    contentVersion: 1, authorId, authorIds: [authorId], authorName, authorStatus: "confirmed", source, characters: [character],
    tags: [tag], favorite: false, liked: false, likeCount: 0, viewCount: a.views, playCount: 0, note: "",
    createdAt: a.timestamp, fileDate: a.timestamp, sourceMtime: a.timestamp, updatedAt: a.timestamp,
    thumbnailUrl: `/api/v1/assets/${a.id}/thumbnail`, contentUrl: `/api/v1/assets/${a.id}/content`,
    width: 640, height: 360, probeState: "ready", trashed: false, positionMs: 0, positionContentVersion: 1,
  });
  const choice = (kind, id, name, count) => ({ kind, id, key: id, name, count });
  const facets = {
    partitions: [choice("partition", "all", "全部", total), choice("partition", "regular", "常规", total), choice("partition", "cos", "COS", 0)],
    works: [choice("source", source, source, total)], roles: [choice("character", character, character, total)],
    types: [choice("type", "all", "全部", total), choice("type", "image", "图片", total), choice("type", "animated_image", "动图", 0), choice("type", "video", "视频", 0)],
  };
  const legacyFacets = Object.fromEntries(Object.entries(facets).map(([key, options]) => [
    ({ works: "authors", roles: "characters" })[key] ?? key,
    options.map(opt => ({ key: opt.key, name: opt.name, kind: opt.kind, fileCount: opt.count })),
  ]));
  return (path, params, method) => {
    if (method === "POST") {
      if (legacy && path === "/events/view") return response({ outcome: "ok" }, 202);
      if (!legacy && /^\/assets\/[\w-]+\/events$/.test(path)) return response({ accepted: true }, 202);
      return null;
    }
    if (method !== "GET") return null;
    if (path === "/recommendations") return legacy ? response(rows, 200, { "X-Qimeng-Next-Offset": "-1" }) : response({ items: rows, snapshotId: "fixture-one" });
    if (path === "/assets") return response({ items: rows, totalMatched: total, dateCounts, ...(legacy ? { nextCursor: null } : { unknownDateCount: 0 }) });
    if (path === "/assets/facets") {
      if (legacy) return response(legacyFacets);
      const items = facets[params.get("row")];
      return items && response({ items, totalOptions: items.length, totalMatched: total });
    }
    if (path === "/tags") return response(legacy ? [tagRecord] : { items: [tag] });
    if (path === "/libraries") {
      const library = { id: libraryId, name: "合成图库", kind: legacy ? "normal" : "regular", fileCount: total, scanState: "idle", capabilities: { authorAttach: true }, revision: 1 };
      return response(legacy ? [library] : { items: [library], totalMatched: 1 });
    }
    if (legacy && path === "/authors") return response([person]);
    if (legacy && path === "/library/revision") return response({ revision: 1 });
    if (legacy && path === "/config") return response({ scan: { workers: 2, thumbEdge: 800 }, upload: { autoAccept: true, maxBytesMb: 2147483647 } });
    if (legacy && path === `/authors/${authorId}/profile/avatar`) return response({ code: "NOT_FOUND", message: "合成作者没有头像" }, 404);
    if (!legacy && path === "/instance") return response({ id: "fixture-instance", name: "合成图库", apiVersion: "0.1.0", capabilities: { recommendations: true, rankings: true, stats: true, backups: true, migration: false, aiConfigured: false } });
    if (!legacy && path === "/sessions/current") return response({ id: "fixture-session", label: "合成会话", scopes: ["admin"], expiresAt: "2027-01-01T00:00:00Z" });
    if (!legacy && path === "/settings") return response({ revision: 1, scan: { workers: 2, thumbEdge: 800 }, upload: { autoAccept: true }, effectiveScanWorkers: 2, maxChunkBytes: 8388608, maxActiveUploads: 4, maxBatchItems: 100, sourceCleanupEnabled: false });
    if (!legacy && path === `/authors/${authorId}`) return response({ id: authorId, name: authorName, kind: "regular", revision: 1, aliases: [], acquisitionSources: [], fileCount: total, following: false, placeholder: false, fieldStatus: {} });
    if (!legacy && path === `/authors/${authorId}/profile-summary`) return response({ authorId, xRevision: 0, biographyOriginal: "", biographyZh: "" });
    if (!legacy && path === "/preferences/recommendation") {
      const weights = { tagRelevance: 0.15, tagCollection: 0.15, engagement: 0.1, recency: 0.1,
        likeScore: 0.1, discovery: 0.1, freshness: 0.1, browseDepth: 0.1, maxRandom: 0.1 };
      return response({ revision: 1, weights, presets: { balanced: weights, familiar: weights, explore: weights, fresh: weights } });
    }
    if (!legacy && path === "/ai/delegations") return response({ items: [] });
    if (!legacy && path.startsWith(`/authors/${authorId}/`)) {
      const blocks = ["x", "basic", "accounts"].map(block => ({ authorId, block, revision: 1,
        values: block === "basic" ? { summary: "合成资料" } : block === "x" ? { biographyOriginal: "合成作者原文" } : {},
        evidence: [], source: "human", updatedAt: fixedTime }));
      const archive = { id: "sample-archive", authorId, authorName, reason: "merge", createdAt: fixedTime,
        blockCount: blocks.length, candidateCount: 0, conflicts: [] };
      if (path.endsWith("/profile")) return response({ authorId, editable: true, blocks });
      if (path.endsWith("/updates")) return response({ authorId, revision: 1, trackUpdates: false, feedUrl: "", items: [] });
      if (path.endsWith("/candidates")) return response({ items: [] });
      if (path.endsWith("/archives")) return response({ items: [archive] });
      if (path.endsWith("/archives/sample-archive")) return response({ ...archive, blocks });
    }
    const match = path.match(/^\/assets\/([\w-]+)(?:\/(timeline-tags))?$/);
    const item = match && rows.find(a => a.id === match[1]);
    if (item) {
      if (legacy && match[2] === "timeline-tags") return response([]);
      if (!match[2]) return response(legacy ? { ...item, width: 640, height: 360, relPath: item.fileName, directory: "", tags: [tagRecord], authors: [person], videoCodec: null } : item);
    }
    return null;
  };
}
