type Entry = {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
  file: (
    success: (file: File) => void,
    failure: (error: DOMException) => void,
  ) => void;
  createReader: () => {
    readEntries: (
      success: (entries: Entry[]) => void,
      failure: (error: DOMException) => void,
    ) => void;
  };
};
export async function droppedFiles(
  data: DataTransfer,
  maximum: number,
): Promise<File[]> {
  const result: File[] = [];
  const add = (file: File, path?: string) => {
    if (result.length >= maximum)
      throw new Error(`单批最多选择 ${maximum} 个文件，请分批上传。`);
    if (path)
      Object.defineProperty(file, "webkitRelativePath", { value: path });
    result.push(file);
  };
  const walk = async (entry: Entry, parent: string) => {
    const path = parent ? `${parent}/${entry.name}` : entry.name;
    if (entry.isFile) {
      const file = await new Promise<File>((resolve, reject) =>
        entry.file(resolve, reject),
      );
      add(file, path);
    } else if (entry.isDirectory) {
      const reader = entry.createReader();
      while (true) {
        const entries = await new Promise<Entry[]>((resolve, reject) =>
          reader.readEntries(resolve, reject),
        );
        if (!entries.length) break;
        for (const child of entries) await walk(child, path);
      }
    }
  };
  // DataTransfer access is only guaranteed during the drop event. Capture both
  // representations before walking a directory or awaiting any file reads.
  const inputs = Array.from(data.items).filter(item => item.kind === "file").map(item => ({
    entry: item.webkitGetAsEntry?.() as Entry | null,
    file: item.getAsFile(),
  }));
  if (!inputs.length) {
    for (const file of Array.from(data.files)) add(file);
  } else {
    for (const input of inputs) {
      if (input.entry) await walk(input.entry, "");
      else if (input.file) add(input.file);
      else throw new Error("拖入的条目无法读取，本次选择尚未上传。请重新选择文件。");
    }
  }
  return result;
}
