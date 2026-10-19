// Presentation helpers. Everything here is pure so it can be used in templates
// without touching reactive state.

export function formatBytes(bytes: number | undefined, empty = "—"): string {
  if (bytes === undefined || bytes === null) return empty;
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const digits = value >= 100 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(digits)} ${units[unit]}`;
}

export function formatSpeed(bytesPerSecond: number): string {
  if (!bytesPerSecond || bytesPerSecond <= 0) return "";
  return `${formatBytes(bytesPerSecond)}/s`;
}

export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "";
  const totalSeconds = Math.ceil(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds} 秒`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return `${minutes} 分 ${seconds} 秒`;
  const hours = Math.floor(minutes / 60);
  return `${hours} 小时 ${minutes % 60} 分`;
}

const MONTHS = ["1月", "2月", "3月", "4月", "5月", "6月", "7月", "8月", "9月", "10月", "11月", "12月"];

export function formatDate(iso: string | undefined, mode: "iso" | "localized"): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  if (mode === "iso") {
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
      date.getHours(),
    )}:${pad(date.getMinutes())}`;
  }
  const now = new Date();
  const sameYear = date.getFullYear() === now.getFullYear();
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  if (!sameYear) {
    return `${date.getFullYear()}年${MONTHS[date.getMonth()]}${date.getDate()}日 ${time}`;
  }
  return `${date.getMonth() + 1}月${date.getDate()}日 ${time}`;
}

export function basename(path: string | undefined): string {
  if (!path) return "";
  const clean = path.replace(/\/+$/, "");
  if (clean === "" || clean === "/") return "/";
  const index = clean.lastIndexOf("/");
  return index === -1 ? clean : clean.slice(index + 1);
}

export function dirname(path: string): string {
  const clean = path.replace(/\/+$/, "");
  if (clean === "" || clean === "/") return "/";
  const index = clean.lastIndexOf("/");
  if (index <= 0) return "/";
  return clean.slice(0, index);
}

export function joinPath(base: string, name: string): string {
  if (!base || base === "/") return `/${name}`;
  return `${base.replace(/\/+$/, "")}/${name}`;
}

export function isInside(parent: string, child: string): boolean {
  const p = parent.replace(/\/+$/, "");
  return child === p || child.startsWith(`${p}/`);
}

export function extensionOf(name: string): string {
  const index = name.lastIndexOf(".");
  if (index <= 0) return "";
  return name.slice(index + 1).toLowerCase();
}

export function isImage(name: string): boolean {
  return ["png", "jpg", "jpeg", "gif", "webp", "heic", "bmp", "svg", "tiff"].includes(
    extensionOf(name),
  );
}

export function isArchive(name: string): boolean {
  return ["zip", "tar", "gz", "bz2", "xz", "7z", "rar", "dmg", "iso"].includes(extensionOf(name));
}

export function isCode(name: string): boolean {
  return [
    "ts", "tsx", "js", "jsx", "vue", "go", "rs", "py", "rb", "java", "c", "h", "cpp",
    "hpp", "cs", "swift", "kt", "php", "sh", "zsh", "sql", "json", "yaml", "yml",
    "toml", "xml", "html", "css", "scss", "md",
  ].includes(extensionOf(name));
}

export function isDocument(name: string): boolean {
  return ["pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "pages", "numbers", "key", "txt", "rtf"].includes(
    extensionOf(name),
  );
}

export function isMedia(name: string): boolean {
  return ["mp4", "mov", "m4v", "avi", "mkv", "mp3", "wav", "flac", "aac", "m4a"].includes(
    extensionOf(name),
  );
}

export function plural(n: number, singular: string, pluralForm?: string): string {
  return `${n} ${n === 1 ? singular : pluralForm ?? singular}`;
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/** Windows-style "safe" rename of a clashing name, used only for local display. */
export function withSuffix(name: string, suffix: number): string {
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return `${name} (${suffix})`;
  return `${name.slice(0, dot)} (${suffix})${name.slice(dot)}`;
}
