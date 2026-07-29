import { Tag } from "antd";
import { downloadLabels, recognitionLabels } from "../format";
import type { DownloadStatus, RecognitionStatus } from "../types";

const downloadColors: Record<DownloadStatus, string> = {
  not_requested: "default",
  queued: "cyan",
  submitting: "processing",
  waiting: "geekblue",
  active: "blue",
  paused: "gold",
  complete: "green",
  error: "red",
  unknown: "orange",
};

export function DownloadStatusTag({ status }: { status: DownloadStatus }) {
  return <Tag color={downloadColors[status]}>{downloadLabels[status]}</Tag>;
}

export function RecognitionStatusTag({
  status,
}: {
  status: RecognitionStatus;
}) {
  return (
    <Tag color={status === "recognized" ? "green" : "default"}>
      {recognitionLabels[status]}
    </Tag>
  );
}

