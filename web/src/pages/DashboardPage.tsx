import {
  CheckCircleOutlined,
  CloudDownloadOutlined,
  ExclamationCircleOutlined,
  FieldTimeOutlined,
  UnorderedListOutlined,
} from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { Alert, Card, Col, Empty, List, Row, Skeleton, Statistic, Typography } from "antd";
import { api } from "../api";
import { PageHeader } from "../components/PageHeader";
import { DownloadStatusTag } from "../components/StatusTag";
import { formatDate } from "../format";
import type { Dashboard, FeedItem, Page } from "../types";

export function DashboardPage() {
  const dashboard = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<Dashboard>("/dashboard"),
    refetchInterval: 10_000,
  });
  const recent = useQuery({
    queryKey: ["items", "recent"],
    queryFn: () => api<Page<FeedItem>>("/items?page=1&pageSize=6"),
    refetchInterval: 10_000,
  });

  return (
    <>
      <PageHeader
        title="运行概览"
        description="订阅抓取、条目识别和 aria2 下载任务的实时摘要。"
      />
      {dashboard.error && (
        <Alert
          type="error"
          showIcon
          message="概览加载失败"
          description={dashboard.error.message}
        />
      )}
      <Row gutter={[16, 16]} className="metric-grid">
        <Metric
          title="有效订阅"
          value={dashboard.data?.subscriptions}
          icon={<UnorderedListOutlined />}
          tone="teal"
          loading={dashboard.isLoading}
        />
        <Metric
          title="已识别条目"
          value={dashboard.data?.recognizedItems}
          icon={<CheckCircleOutlined />}
          tone="blue"
          loading={dashboard.isLoading}
        />
        <Metric
          title="活动下载"
          value={dashboard.data?.activeDownloads}
          icon={<CloudDownloadOutlined />}
          tone="violet"
          loading={dashboard.isLoading}
        />
        <Metric
          title="异常任务"
          value={dashboard.data?.errors}
          icon={<ExclamationCircleOutlined />}
          tone="orange"
          loading={dashboard.isLoading}
        />
      </Row>
      <Row gutter={[16, 16]} className="dashboard-lower">
        <Col xs={24} xl={16}>
          <Card
            title="最近发现"
            extra={
              <Typography.Text type="secondary">
                共完成 {dashboard.data?.completed ?? 0} 项
              </Typography.Text>
            }
          >
            {recent.isLoading ? (
              <Skeleton active />
            ) : recent.data?.items.length ? (
              <List
                dataSource={recent.data.items}
                renderItem={(item) => (
                  <List.Item
                    extra={
                      item.download ? (
                        <DownloadStatusTag status={item.download.status} />
                      ) : (
                        <DownloadStatusTag status="not_requested" />
                      )
                    }
                  >
                    <List.Item.Meta
                      title={
                        item.detailUrl ? (
                          <a href={item.detailUrl} target="_blank" rel="noreferrer">
                            {item.title}
                          </a>
                        ) : (
                          item.title
                        )
                      }
                      description={`${item.subscriptionName || "未命名订阅"} · ${formatDate(
                        item.publishedAt ?? item.discoveredAt,
                      )}`}
                    />
                  </List.Item>
                )}
              />
            ) : (
              <Empty description="尚未发现 RSS 条目" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card title="调度状态" className="schedule-card">
            <div className="schedule-icon">
              <FieldTimeOutlined />
            </div>
            <Typography.Title level={4}>后台轮询已启用</Typography.Title>
            <Typography.Paragraph type="secondary">
              最近一次订阅检查
            </Typography.Paragraph>
            <Typography.Text strong>
              {formatDate(dashboard.data?.lastSyncAt)}
            </Typography.Text>
            <div className="schedule-pulse">
              <span />
              系统正在持续监听新条目
            </div>
          </Card>
        </Col>
      </Row>
    </>
  );
}

function Metric({
  title,
  value,
  icon,
  tone,
  loading,
}: {
  title: string;
  value?: number;
  icon: React.ReactNode;
  tone: string;
  loading: boolean;
}) {
  return (
    <Col xs={24} sm={12} xl={6}>
      <Card className={`metric-card metric-${tone}`}>
        <div className="metric-icon">{icon}</div>
        <Statistic title={title} value={loading ? "—" : (value ?? 0)} />
      </Card>
    </Col>
  );
}

