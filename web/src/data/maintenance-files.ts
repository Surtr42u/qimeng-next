import { sha256 } from "@noble/hashes/sha2.js";
import { fileDigest } from "./hash";
import { newId } from "../lib/id";

export type Directory = FileSystemDirectoryHandle & {
  queryPermission(options: { mode: "readwrite" }): Promise<PermissionState>;
  requestPermission(options: { mode: "readwrite" }): Promise<PermissionState>;
};
type PickerWindow = Window & { showDirectoryPicker?: (options: { id: string; mode: "readwrite" }) => Promise<Directory> };
export const folderSupported = () => !!(window as PickerWindow).showDirectoryPicker && !!navigator.locks;
export function pickFolder() {
  const picker = (window as PickerWindow).showDirectoryPicker;
  if (!picker || !navigator.locks) throw new Error("当前浏览器不支持维护目录写入。");
  return picker.call(window, { id: "qimeng-maintenance", mode: "readwrite" });
}
export const folderLock = <T,>(job: () => Promise<T>, signal?: AbortSignal) =>
  navigator.locks.request("qimeng-maintenance-directory", { mode: "exclusive", signal }, job);
const missing = (error: unknown) => error instanceof DOMException && error.name === "NotFoundError";
async function fileAt(root: FileSystemDirectoryHandle, path: string[]): Promise<File | undefined> {
  try { return await (await (await parentAt(root, path, false)).getFileHandle(path.at(-1)!)).getFile(); }
  catch (error) { if (missing(error)) return undefined; throw error; }
}
const validName = (name: string) => !!name && name !== "." && name !== ".." && !/[\\/:*?"<>|\u0000-\u001f]/.test(name);
async function parentAt(root: FileSystemDirectoryHandle, path: string[], create: boolean) {
  if (!path.length || !path.every(validName)) throw new Error("目录或文件名包含不可用的字符。");
  let parent = root;
  for (const name of path.slice(0, -1)) parent = await parent.getDirectoryHandle(name, { create });
  return parent;
}
async function writeStream(root: FileSystemDirectoryHandle, path: string[], stream: ReadableStream<Uint8Array>, expected: string, signal: AbortSignal) {
  signal.throwIfAborted();
  const parent = await parentAt(root, path, true);
  const output = await (await parent.getFileHandle(path.at(-1)!, { create: true })).createWritable();
  const hash = sha256.create();
  const verify = new TransformStream<Uint8Array, Uint8Array>({
    transform(bytes, writer) { hash.update(bytes); writer.enqueue(bytes); },
    flush() { if (toHex(hash.digest()) !== expected) throw new Error("内容与预期 SHA-256 不符，本次写入已中止。"); },
  });
  // pipeTo aborts the writable on a failed digest/read, before its atomic close.
  await stream.pipeThrough(verify).pipeTo(output, { signal });
}
async function writeText(root: FileSystemDirectoryHandle, path: string[], text: string, signal: AbortSignal) {
  await writeStream(root, path, new Blob([text]).stream(), textHash(text), signal);
}
const toHex = (bytes: Uint8Array) => Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("");
const textHash = (source: string) => toHex(sha256(new TextEncoder().encode(source)));
const digest = (file: File, signal: AbortSignal) => fileDigest(file, signal, () => {});
async function smallText(file: File, limit = 16384) {
  if (file.size > limit) throw new Error("维护目录的清单或摘要超出大小限制。");
  return file.text();
}

const slots = ["当前", "上次"] as const;
type Slot = typeof slots[number];
const layout = {
  archive: ["备份", "影库备份.qnext"],
  master: ["作者资料", "作者总表.txt"],
  report: ["作者资料", "作者资料.md"],
  index: ["作者资料", "作者片段索引.json"],
} as const;
type Part = keyof typeof layout;
const parts = Object.keys(layout) as Part[];
const stageRoot = ".绮梦下一代暂存";
const journalName = ".绮梦下一代轮换.json";
const mobileJournal = ".绮梦手机轮换.json";
async function checkOtherWriter(root: Directory) {
  if ((await fileAt(root, [mobileJournal]))?.size) throw new Error("请先在手机完成未结束的归档，再更新或读取这两代资料。");
}
const recordName = ".绮梦下一代清单.json";
const checksum = /^[a-f0-9]{64}$/;
export type Generation = { instanceId: string; businessRevision: string; backupId: string; hashes: Record<Part, string> };
type Journal = { stage: string; phase: "previous" | "current"; current?: Generation; previous?: Generation; next: Generation };
function generation(value: unknown): Generation {
  const row = value as Generation;
  if (!row || typeof row.instanceId !== "string" || !/^\d+$/.test(row.businessRevision) || typeof row.backupId !== "string" || !/^[A-Za-z0-9_-]{1,200}$/.test(row.backupId) ||
      !row.hashes || !parts.every(part => checksum.test(row.hashes[part]))) throw new Error("维护目录中的代次清单无效，已停止写回。");
  return row;
}
export async function readGeneration(root: Directory, slot: Slot): Promise<Generation | undefined> {
  const file = await fileAt(root, [slot, recordName]);
  return file?.size ? generation(JSON.parse(await smallText(file))) : undefined;
}
const sameGeneration = (a: Generation | undefined, b: Generation | undefined) =>
  (!a || !b) ? a === b : a.instanceId === b.instanceId && a.businessRevision === b.businessRevision && a.backupId === b.backupId && parts.every(part => a.hashes[part] === b.hashes[part]);
async function readJournal(root: Directory) {
  const file = await fileAt(root, [journalName]);
  if (!file?.size) return;
  const row: Journal = JSON.parse(await smallText(file));
  if (!row || !/^[A-Za-z0-9_-]{8,128}$/.test(row.stage) || !["previous", "current"].includes(row.phase)) throw new Error("维护目录中的轮换记录无效。");
  generation(row.next); if (row.current) generation(row.current); if (row.previous) generation(row.previous);
  if ([row.current, row.previous].some(g => g && g.instanceId !== row.next.instanceId)) throw new Error("两代清单属于不同影库。");
  return row;
}
const hashPath = (path: string[]) => [...path.slice(0, -1), path.at(-1)! + ".sha256"];
async function checkParts(root: Directory, slot: Slot, old: Generation | undefined, next: Generation | undefined, signal: AbortSignal, interrupted: boolean) {
  const record = await readGeneration(root, slot);
  if (!sameGeneration(record, old) && !(interrupted && sameGeneration(record, next))) throw new Error(slot + "代清单已改变，无法继续更新。");
  const present: Partial<Record<Part, string>> = {};
  for (const part of parts) {
    const path = [slot, ...layout[part]];
    const file = await fileAt(root, path);
    const side = await fileAt(root, hashPath(path));
    const allowed = [old?.hashes[part], interrupted ? next?.hashes[part] : undefined].filter(Boolean);
    if (file) {
      if (!(interrupted && !old && file.size === 0)) {
        present[part] = await digest(file, signal);
        if (!allowed.includes(present[part])) throw new Error(path.join(" / ") + " 已被外部修改或来自另一份归档，已保留原文件。");
      }
    } else if (old) throw new Error(path.join(" / ") + " 缺失，已暂停更新。");
    if (side) {
      const stored = (await smallText(side, 1024)).trim().toLowerCase();
      if (!allowed.includes(stored) && !(interrupted && !old && !stored)) throw new Error(path.join(" / ") + " 的独立摘要不符。");
    } else if (old) throw new Error(path.join(" / ") + " 缺少独立摘要。");
  }
  return present;
}
type ContentStream = (part: Part) => Promise<ReadableStream<Uint8Array>>;
async function publish(root: Directory, slot: Slot, old: Generation | undefined, next: Generation, stream: ContentStream, signal: AbortSignal) {
  const present = await checkParts(root, slot, old, next, signal, true);
  for (const part of parts) {
    const path = [slot, ...layout[part]];
    if (present[part] !== next.hashes[part]) await writeStream(root, path, await stream(part), next.hashes[part], signal);
    await writeText(root, hashPath(path), next.hashes[part] + "\n", signal);
  }
  await writeText(root, [slot, recordName], JSON.stringify(next), signal);
}
type ArchiveStream = (id: string, signal: AbortSignal) => Promise<ReadableStream<Uint8Array> | null>;
export async function resumeRotation(root: Directory, instanceId: string, download: ArchiveStream, signal: AbortSignal) {
  await checkOtherWriter(root);
  const job = await readJournal(root);
  if (!job) return;
  if (job.next.instanceId !== instanceId) throw new Error("维护目录中有另一个影库未完成的轮换。");
  if (job.phase === "previous" && job.current) {
    await publish(root, "上次", job.previous, job.current, async part => {
      const source = await fileAt(root, ["当前", ...layout[part]]);
      if (!source) throw new Error("当前代文件缺失，未轮换。");
      return source.stream();
    }, signal);
    job.phase = "current"; await writeText(root, [journalName], JSON.stringify(job), signal);
  }
  await publish(root, "当前", job.current, job.next, async part => {
    if (part === "archive") {
      const stream = await download(job.next.backupId, signal);
      if (!stream) throw new Error("服务器未提供归档正文。");
      return stream;
    }
    const file = await fileAt(root, [stageRoot, job.stage, layout[part][1]]);
    if (!file) throw new Error("待提交的作者资料缺失。");
    return file.stream();
  }, signal);
  await releaseMaterials(root, { stage: job.stage, hashes: job.next.hashes }, signal);
  await root.removeEntry(journalName);
  return job.next;
}
export type Materials = { stage: string; hashes: Record<"master" | "report" | "index", string> };
export async function releaseMaterials(root: Directory, materials: Materials, signal: AbortSignal) {
  const parent = await root.getDirectoryHandle(stageRoot);
  let stage: FileSystemDirectoryHandle;
  try { stage = await parent.getDirectoryHandle(materials.stage); }
  catch (error) { if (missing(error)) return; throw error; }
  for (const part of ["master", "report", "index"] as const) {
    const name = layout[part][1];
    const file = await fileAt(stage, [name]);
    if (!file) continue;
    if (await digest(file, signal) !== materials.hashes[part]) throw new Error("暂存资料出现外部修改，已保留原文件。");
    await stage.removeEntry(name);
  }
  const iterable = stage as FileSystemDirectoryHandle & { values(): AsyncIterable<FileSystemHandle> };
  for await (const _entry of iterable.values()) return;
  await parent.removeEntry(materials.stage);
}
export async function stageMaterials(root: Directory, master: ReadableStream<Uint8Array>, report: ReadableStream<Uint8Array>, signal: AbortSignal): Promise<Materials> {
  const stage = newId();
  const hashes = { master: "", report: "", index: "" };
  const files = { master, report, index: new Blob([JSON.stringify(["作者总表.txt"])]).stream() };
  for (const part of ["master", "report", "index"] as const) {
    const path = [stageRoot, stage, layout[part][1]];
    if (await fileAt(root, path)) throw new Error("暂存文件已存在，未覆盖。");
    const hash = sha256.create();
    const parent = await parentAt(root, path, true);
    const output = await (await parent.getFileHandle(path.at(-1)!, { create: true })).createWritable();
    await files[part].pipeThrough(new TransformStream<Uint8Array, Uint8Array>({
      transform(chunk, writer) { hash.update(chunk); writer.enqueue(chunk); },
    })).pipeTo(output, { signal });
    hashes[part] = toHex(hash.digest());
  }
  return { stage, hashes };
}
export async function rotate(root: Directory, identity: Omit<Generation, "hashes">, archiveHash: string, materials: Materials, download: ArchiveStream, signal: AbortSignal) {
  await checkOtherWriter(root);
  const current = await readGeneration(root, "当前");
  const previous = await readGeneration(root, "上次");
  if ([current, previous].some(value => value && value.instanceId !== identity.instanceId)) throw new Error("目录已绑定另一影库，未覆盖其资料。");
  if (!current && previous) throw new Error("当前代缺失，上一代仍然保留。");
  await checkParts(root, "当前", current, undefined, signal, false);
  if (current) await checkParts(root, "上次", previous, undefined, signal, false);
  const next = generation({ ...identity, hashes: { archive: archiveHash, ...materials.hashes } });
  const job: Journal = { stage: materials.stage, phase: current ? "previous" : "current", current, previous, next };
  await writeText(root, [journalName], JSON.stringify(job), signal);
  return await resumeRotation(root, identity.instanceId, download, signal);
}
export async function verifyGeneration(root: Directory, slot: Slot, expected: Generation, signal: AbortSignal) {
  await checkParts(root, slot, expected, undefined, signal, false);
}
export async function recoveryFile(root: Directory, slot: Slot, instanceId: string, signal: AbortSignal) {
  await checkOtherWriter(root);
  if (await readJournal(root)) throw new Error("请先在浏览器完成未结束的归档，再选择恢复来源。");
  const current = await readGeneration(root, slot);
  if (current) {
    if (current.instanceId !== instanceId) throw new Error("该代资料属于另一个影库。");
    const file = await fileAt(root, [slot, ...layout.archive]);
    const side = await fileAt(root, hashPath([slot, ...layout.archive]));
    if (!file || !side || (await smallText(side, 1024)).trim() !== current.hashes.archive || await digest(file, signal) !== current.hashes.archive) throw new Error("归档或其独立摘要不符。");
    return { kind: "next" as const, file, hash: current.hashes.archive };
  }
  const path = [slot, "备份", "影库备份.json"];
  const file = await fileAt(root, path);
  const side = await fileAt(root, hashPath(path));
  if (!file || !side) throw new Error("这代备份或独立摘要不存在。");
  const hash = (await smallText(side, 1024)).trim().toLowerCase();
  if (!checksum.test(hash) || await digest(file, signal) !== hash) throw new Error("旧 v2 正文与独立摘要不符。");
  return { kind: "v2" as const, file, hash };
}
export async function folderAuthorLists(root: Directory) {
  await checkOtherWriter(root);
  if (await readJournal(root)) throw new Error("请先完成未结束的归档，再读取当前作者资料。");
  const folder = await (await root.getDirectoryHandle("当前")).getDirectoryHandle("作者资料");
  const index = await fileAt(folder, ["作者片段索引.json"]);
  if (!index) {
    const files: File[] = [];
    for await (const item of (folder as FileSystemDirectoryHandle & { values(): AsyncIterable<FileSystemHandle> }).values()) {
      if (item.kind === "file" && /\.txt$/i.test(item.name)) files.push(await (item as FileSystemFileHandle).getFile());
    }
    return files;
  }
  const names: unknown = JSON.parse(await smallText(index, 1024 * 1024));
  if (!Array.isArray(names) || names.some(name => typeof name !== "string" || !validName(name) || !/\.txt$/i.test(name))) throw new Error("作者片段索引包含无效名称。");
  const files: File[] = [];
  for (const name of names) files.push(await (await folder.getFileHandle(name)).getFile());
  return files;
}
export async function archiveOriginal(root: Directory, libraryName: string, relativeDirectory: string, file: File, expected: string, signal: AbortSignal) {
  const path = ["上传归档", libraryName, ...relativeDirectory.split("/").filter(Boolean), file.name];
  const old = await fileAt(root, path);
  if (old) {
    if (old.size === file.size && await digest(old, signal) === expected) return "已有相同原件副本";
    throw new Error("归档处的同名文件内容不同，已保留该文件。请先移开冲突文件再重试。");
  }
  await writeStream(root, path, file.stream(), expected, signal);
  return "原件副本已归档";
}
