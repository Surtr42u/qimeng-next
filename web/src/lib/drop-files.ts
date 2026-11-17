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
  const entries = Array.from(data.items).map(
    (item) => item.webkitGetAsEntry?.() as Entry | null,
  );
  if (entries.some(Boolean)) {
    for (const entry of entries) if (entry) await walk(entry, "");
  } else for (const file of Array.from(data.files)) add(file);
  return result;
}
