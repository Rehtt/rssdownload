import {
  CloudDownloadOutlined,
  LinkOutlined,
  PauseOutlined,
  PlayCircleOutlined,
  RedoOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Descriptions,
  Drawer,
  Input,
  Progress,
  Select,
  Space,
  Table,
  Tooltip,
  Typography,
} from "antd";
import type { ColumnsType, TablePaginationConfig } from "antd/es/table";
import { useMemo, useState } from "react";
import { api, queryString } from "../api";
import { PageHeader } from "../components/PageHeader";
import {
  DownloadStatusTag,
  RecognitionStatusTag,
} from "../components/StatusTag";
import {
  downloadLabels,
  formatBytes,
  formatDate,
  progressPercent,
  recognitionLabels,
} from "../format";
import type {
  DownloadStatus,
  FeedItem,
  Page,
  RecognitionStatus,
  Subscription,
} from "../types";

interface Filters {
  search: string;
  subscriptionId?: number;
  recognition?: RecognitionStatus;
  downloadStatus?: DownloadStatus;
}

export function ItemsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [filters, setFilters] = useState<Filters>({ search: "" });
  const [searchDraft, setSearchDraft] = useState("");
  const [selected, setSelected] = useState<FeedItem | null>(null);

  const query = queryString({
    page,
    pageSize,
    search: filters.search,
    subscriptionId: filters.subscriptionId,
    recognition: filters.recognition,
    downloadStatus: filters.downloadStatus,
  });
  const items = useQuery({
    queryKey: ["items", page, pageSize, filters],
    queryFn: () => api<Page<FeedItem>>(`/items${query}`),
    refetchInterval: 5_000,
  });
  const subscriptions = useQuery({
    queryKey: ["subscriptions", false],
    queryFn: () => api<Subscription[]>("/subscriptions"),
  });

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["items"] }),
      queryClient.invalidateQueries({ queryKey: ["dashboard"] }),
    ]);
  };

  const action = useMutation({
    mutationFn: ({
      item,
      operation,
    }: {
      item: FeedItem;
      operation: "download" | "retry" | "pause" | "resume";
    }) => {
      if (operation === "download") {
        return api(`/items/${item.id}/download`, { method: "POST" });
      }
      return api(`/downloads/${item.downloadId}/${operation}`, {
        method: "POST",
      });
    },
    onSuccess: async (_, variables) => {
      const labels = {
        download: "条目已加入下载队列",
        retry: "任务已重新排队",
        pause: "任务已暂停",
        resume: "任务已继续",
      };
      message.success(labels[variables.operation]);
      await refresh();
    },
    onError: (error) => message.error(error.message),
  });

  const columns: ColumnsType<FeedItem> = useMemo(
    () => [
      {
        title: "发布条目",
        key: "title",
        render: (_, record) => (
          <button
            type="button"
            className="item-title-button"
            onClick={() => setSelected(record)}
          >
            <Typography.Text strong>{record.title}</Typography.Text>
            <Typography.Text type="secondary">
              {record.subscriptionName || "未命名订阅"} ·{" "}
              {formatDate(record.publishedAt ?? record.discoveredAt)}
            </Typography.Text>
          </button>
        ),
      },
      {
        title: "大小",
        dataIndex: "enclosureLength",
        width: 110,
        responsive: ["lg"],
        render: formatBytes,
      },
      {
        title: "识别",
        dataIndex: "recognitionStatus",
        width: 100,
        responsive: ["md"],
        render: (status: RecognitionStatus) => (
          <RecognitionStatusTag status={status} />
        ),
      },
      {
        title: "下载状态",
        key: "download",
        width: 200,
        render: (_, record) => {
          if (!record.download) {
            return <DownloadStatusTag status="not_requested" />;
          }
          const percentage = progressPercent(
            record.download.completedLength,
            record.download.totalLength,
          );
          return (
            <div className="download-cell">
              <DownloadStatusTag status={record.download.status} />
              {record.download.totalLength > 0 &&
                record.download.status !== "complete" && (
                  <Progress
                    percent={percentage}
                    size="small"
                    showInfo={false}
                    status={record.download.status === "error" ? "exception" : "active"}
                  />
                )}
            </div>
          );
        },
      },
      {
        title: "",
        key: "action",
        width: 72,
        fixed: "right",
        render: (_, record) => (
          <ItemAction
            item={record}
            loading={action.isPending}
            onAction={(operation) => action.mutate({ item: record, operation })}
          />
        ),
      },
    ],
    [action],
  );

  const pagination: TablePaginationConfig = {
    current: page,
    pageSize,
    total: items.data?.total ?? 0,
    showSizeChanger: true,
    showTotal: (total) => `共 ${total} 条`,
    onChange: (nextPage, nextPageSize) => {
      setPage(nextPageSize === pageSize ? nextPage : 1);
      setPageSize(nextPageSize);
    },
  };

  return (
    <>
      <PageHeader
        title="条目与下载"
        description="查看 RSS 识别结果、aria2 实时进度及错误信息。"
      />
      {items.error && (
        <Alert
          type="error"
          showIcon
          message="条目加载失败"
          description={items.error.message}
        />
      )}
      <div className="filter-bar">
        <Input
          allowClear
          prefix={<SearchOutlined />}
          placeholder="搜索发布标题"
          value={searchDraft}
          onChange={(event) => setSearchDraft(event.target.value)}
          onPressEnter={() => {
            setFilters((value) => ({ ...value, search: searchDraft.trim() }));
            setPage(1);
          }}
        />
        <Select
          allowClear
          placeholder="全部订阅"
          value={filters.subscriptionId}
          options={(subscriptions.data ?? []).map((subscription) => ({
            value: subscription.id,
            label: subscription.name || `订阅 #${subscription.id}`,
          }))}
          onChange={(subscriptionId) => {
            setFilters((value) => ({ ...value, subscriptionId }));
            setPage(1);
          }}
        />
        <Select
          allowClear
          placeholder="全部识别结果"
          value={filters.recognition}
          options={Object.entries(recognitionLabels).map(([value, label]) => ({
            value,
            label,
          }))}
          onChange={(recognition) => {
            setFilters((value) => ({ ...value, recognition }));
            setPage(1);
          }}
        />
        <Select
          allowClear
          placeholder="全部下载状态"
          value={filters.downloadStatus}
          options={Object.entries(downloadLabels).map(([value, label]) => ({
            value,
            label,
          }))}
          onChange={(downloadStatus) => {
            setFilters((value) => ({ ...value, downloadStatus }));
            setPage(1);
          }}
        />
        <Button
          type="primary"
          icon={<SearchOutlined />}
          onClick={() => {
            setFilters((value) => ({ ...value, search: searchDraft.trim() }));
            setPage(1);
          }}
        >
          搜索
        </Button>
      </div>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={items.data?.items ?? []}
        loading={items.isLoading}
        pagination={pagination}
        scroll={{ x: 760 }}
      />
      <Drawer
        open={Boolean(selected)}
        title="条目详情"
        width={560}
        onClose={() => setSelected(null)}
      >
        {selected && <ItemDetails item={selected} />}
      </Drawer>
    </>
  );
}

function ItemAction({
  item,
  loading,
  onAction,
}: {
  item: FeedItem;
  loading: boolean;
  onAction: (operation: "download" | "retry" | "pause" | "resume") => void;
}) {
  if (item.recognitionStatus !== "recognized") return null;
  if (!item.download) {
    return (
      <Tooltip title="手动下载">
        <Button
          type="text"
          loading={loading}
          icon={<CloudDownloadOutlined />}
          onClick={() => onAction("download")}
        />
      </Tooltip>
    );
  }
  if (item.download.status === "error" || item.download.status === "unknown") {
    return (
      <Tooltip title="重新提交">
        <Button
          type="text"
          loading={loading}
          icon={<RedoOutlined />}
          onClick={() => onAction("retry")}
        />
      </Tooltip>
    );
  }
  if (item.download.status === "paused") {
    return (
      <Tooltip title="继续">
        <Button
          type="text"
          loading={loading}
          icon={<PlayCircleOutlined />}
          onClick={() => onAction("resume")}
        />
      </Tooltip>
    );
  }
  if (item.download.status === "active" || item.download.status === "waiting") {
    return (
      <Tooltip title="暂停">
        <Button
          type="text"
          loading={loading}
          icon={<PauseOutlined />}
          onClick={() => onAction("pause")}
        />
      </Tooltip>
    );
  }
  return null;
}

function ItemDetails({ item }: { item: FeedItem }) {
  return (
    <div className="item-details">
      <Typography.Title level={4}>{item.title}</Typography.Title>
      {item.description && (
        <Typography.Paragraph type="secondary">
          {item.description}
        </Typography.Paragraph>
      )}
      <Descriptions column={1} size="small" bordered>
        <Descriptions.Item label="订阅">{item.subscriptionName}</Descriptions.Item>
        <Descriptions.Item label="发布时间">
          {formatDate(item.publishedAt)}
        </Descriptions.Item>
        <Descriptions.Item label="发现时间">
          {formatDate(item.discoveredAt)}
        </Descriptions.Item>
        <Descriptions.Item label="文件大小">
          {formatBytes(item.enclosureLength)}
        </Descriptions.Item>
        <Descriptions.Item label="识别结果">
          <RecognitionStatusTag status={item.recognitionStatus} />
          {item.recognitionError && (
            <Typography.Text type="danger">{item.recognitionError}</Typography.Text>
          )}
        </Descriptions.Item>
        {item.download && (
          <>
            <Descriptions.Item label="下载状态">
              <DownloadStatusTag status={item.download.status} />
            </Descriptions.Item>
            <Descriptions.Item label="aria2 GID">
              <Typography.Text copyable code>
                {item.download.aria2Gid}
              </Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label="进度">
              {formatBytes(item.download.completedLength)} /{" "}
              {formatBytes(item.download.totalLength)}
            </Descriptions.Item>
            {item.download.errorMessage && (
              <Descriptions.Item label="错误">
                <Typography.Text type="danger">
                  {item.download.errorMessage}
                </Typography.Text>
              </Descriptions.Item>
            )}
          </>
        )}
      </Descriptions>
      <Space className="detail-links">
        {item.detailUrl && (
          <Button
            icon={<LinkOutlined />}
            href={item.detailUrl}
            target="_blank"
            rel="noreferrer"
          >
            Mikan 详情页
          </Button>
        )}
        {item.enclosureUrl && (
          <Typography.Text type="secondary" ellipsis={{ tooltip: item.enclosureUrl }}>
            {item.enclosureUrl}
          </Typography.Text>
        )}
      </Space>
    </div>
  );
}

