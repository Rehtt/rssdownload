import type { DownloadStatus, RecognitionStatus } from "./types";

export const downloadLabels: Record<DownloadStatus, string> = {
  not_requested: "未请求",
  queued: "排队中",
  submitting: "提交中",
  waiting: "等待中",
  active: "下载中",
  paused: "已暂停",
  complete: "已完成",
  error: "失败",
  unknown: "状态未知",
};

export const recognitionLabels: Record<RecognitionStatus, string> = {
  recognized: "已识别",
  unsupported: "不支持",
};

export function formatDate(value: string | null | undefined) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = value;
  let index = 0;
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024;
    index += 1;
  }
  const digits = amount >= 100 || index === 0 ? 0 : amount >= 10 ? 1 : 2;
  return `${amount.toFixed(digits)} ${units[index]}`;
}

export function progressPercent(completed: number, total: number) {
  if (!total) return 0;
  return Math.min(100, Math.max(0, Math.round((completed / total) * 100)));
}

